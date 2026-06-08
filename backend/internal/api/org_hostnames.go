package api

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"
)

// org_hostnames.go is the management half of org custom domains: the org-admin
// CRUD for registering a hostname, the DNS-TXT ownership challenge, and the
// instance-admin force-activate path. The request-time resolution + origin
// logic lives in custom_domains.go.
//
// "Hostname" here is a web front door (tela.ngss.io). Do not confuse it with
// org_email_domains ("domain"), which is email-based auto-join — a different
// feature with its own handlers in org_domains.go.

const verifyTXTPrefix = "_tela-verify."

// orgHostnameDTO is the wire shape. It carries the DNS records the admin must
// set: a TXT challenge proving ownership, and the CNAME target the hostname
// should point at. status is 'pending' until verified, then 'active'.
type orgHostnameDTO struct {
	Hostname    string  `json:"hostname"`
	Status      string  `json:"status"`
	TXTName     string  `json:"txt_name"`     // where to put the challenge
	TXTValue    string  `json:"txt_value"`    // the challenge value
	CNAMETarget string  `json:"cname_target"` // where the hostname should point
	VerifiedAt  *string `json:"verified_at"`
	CreatedAt   string  `json:"created_at"`
}

type orgHostnameAddRequest struct {
	Hostname string `json:"hostname"`
}

// customDomainTarget is the stable hostname customers CNAME their subdomain to,
// so the box's IP can change without breaking customer DNS. Configurable via
// TELA_CUSTOM_DOMAIN_TARGET; falls back to the canonical host. Surfaced in the
// DTO as guidance only — the app never resolves against it.
func customDomainTarget() string {
	if t := strings.TrimSpace(os.Getenv("TELA_CUSTOM_DOMAIN_TARGET")); t != "" {
		return t
	}
	return hostnameOnly(strings.TrimPrefix(strings.TrimPrefix(canonicalBaseURL(), "https://"), "http://"))
}

func hostnameToDTO(hostname, status, verifyToken string, verifiedAt *string, createdAt string) orgHostnameDTO {
	return orgHostnameDTO{
		Hostname:    hostname,
		Status:      status,
		TXTName:     verifyTXTPrefix + hostname,
		TXTValue:    verifyToken,
		CNAMETarget: customDomainTarget(),
		VerifiedAt:  verifiedAt,
		CreatedAt:   createdAt,
	}
}

// ListOrgHostnames — GET /api/orgs/{id}/hostnames. Org-admin (instance-admin
// passes virtually).
func (s *Server) ListOrgHostnames(w http.ResponseWriter, r *http.Request) {
	orgID, ok := parseOrgID(w, r)
	if !ok {
		return
	}
	if !s.requireOrgAdmin(w, r, orgID) {
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT hostname, status, verify_token, verified_at, created_at
		  FROM org_hostnames WHERE org_id = $1 ORDER BY created_at ASC`, orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list hostnames failed")
		return
	}
	defer rows.Close()
	out := []orgHostnameDTO{}
	for rows.Next() {
		var hostname, status, token, createdAt string
		var verifiedAt *string
		if err := rows.Scan(&hostname, &status, &token, &verifiedAt, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan hostname failed")
			return
		}
		out = append(out, hostnameToDTO(hostname, status, token, verifiedAt, createdAt))
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate hostnames failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hostnames": out})
}

// AddOrgHostname — POST /api/orgs/{id}/hostnames. Org-admin. Registers a
// pending hostname with a fresh DNS-TXT challenge. 409 if the hostname is
// already claimed (by any org — a hostname maps to one org).
func (s *Server) AddOrgHostname(w http.ResponseWriter, r *http.Request) {
	orgID, ok := parseOrgID(w, r)
	if !ok {
		return
	}
	if !s.requireOrgAdmin(w, r, orgID) {
		return
	}
	var req orgHostnameAddRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	host, err := normalizeHostname(req.Hostname)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	token, err := randomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "token generation failed")
		return
	}
	var createdAt string
	err = s.DB.QueryRowContext(r.Context(), `
		INSERT INTO org_hostnames (hostname, org_id, verify_token)
		VALUES ($1, $2, $3) RETURNING created_at`, host, orgID, token).Scan(&createdAt)
	if err != nil {
		if isUniqueConstraintErr(err) {
			writeError(w, http.StatusConflict, "conflict", "that hostname is already registered")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "register hostname failed")
		return
	}
	s.audit(r.Context(), r, "org_hostname.add", "org", orgID, host)
	writeJSON(w, http.StatusCreated, map[string]any{
		"hostname": hostnameToDTO(host, "pending", token, nil, createdAt),
	})
}

// VerifyOrgHostname — POST /api/orgs/{id}/hostnames/{hostname}/verify. Org-admin.
// Confirms ownership by resolving the DNS-TXT challenge, then flips the row to
// 'active' (which lets Caddy on-demand TLS issue a cert). An instance-admin
// bypasses the DNS check entirely — the operator-trusted alternate path the
// product asked for (e.g. a domain wired up out of band).
func (s *Server) VerifyOrgHostname(w http.ResponseWriter, r *http.Request) {
	orgID, ok := parseOrgID(w, r)
	if !ok {
		return
	}
	if !s.requireOrgAdmin(w, r, orgID) {
		return
	}
	host := hostnameOnly(r.PathValue("hostname"))
	var token, status string
	err := s.DB.QueryRowContext(r.Context(),
		`SELECT verify_token, status FROM org_hostnames WHERE hostname = $1 AND org_id = $2`,
		host, orgID).Scan(&token, &status)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "hostname not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup hostname failed")
		return
	}

	forced := callerIsInstanceAdmin(r)
	if status != "active" && !forced {
		if err := verifyHostnameTXT(r.Context(), host, token); err != nil {
			writeError(w, http.StatusBadRequest, "verification_failed",
				"DNS TXT record not found or did not match — add "+verifyTXTPrefix+host+" then retry (propagation can take a few minutes)")
			return
		}
	}

	var verifiedAt, createdAt string
	err = s.DB.QueryRowContext(r.Context(), `
		UPDATE org_hostnames SET status = 'active', verified_at = tela_now(), updated_at = tela_now()
		 WHERE hostname = $1 AND org_id = $2
		 RETURNING verified_at, created_at`, host, orgID).Scan(&verifiedAt, &createdAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "activate hostname failed")
		return
	}
	detail := host
	if forced && status != "active" {
		detail = host + " (forced)"
	}
	s.audit(r.Context(), r, "org_hostname.verify", "org", orgID, detail)
	writeJSON(w, http.StatusOK, map[string]any{
		"hostname": hostnameToDTO(host, "active", token, &verifiedAt, createdAt),
	})
}

// DeleteOrgHostname — DELETE /api/orgs/{id}/hostnames/{hostname}. Org-admin.
// Removes the mapping (and, once Caddy stops being asked, the cert lapses).
// Sessions bound to this org on this host become unusable, which is correct.
func (s *Server) DeleteOrgHostname(w http.ResponseWriter, r *http.Request) {
	orgID, ok := parseOrgID(w, r)
	if !ok {
		return
	}
	if !s.requireOrgAdmin(w, r, orgID) {
		return
	}
	host := hostnameOnly(r.PathValue("hostname"))
	res, err := s.DB.ExecContext(r.Context(),
		`DELETE FROM org_hostnames WHERE hostname = $1 AND org_id = $2`, host, orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "delete hostname failed")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "hostname not found")
		return
	}
	s.audit(r.Context(), r, "org_hostname.delete", "org", orgID, host)
	w.WriteHeader(http.StatusNoContent)
}

type hostnameHealthDTO struct {
	DNSOK   bool     `json:"dns_ok"`   // resolves in DNS
	Addrs   []string `json:"addrs"`    // resolved public addresses
	HTTPSOK bool     `json:"https_ok"` // TLS handshake succeeds with a cert valid for the host
	Note    string   `json:"note,omitempty"`
}

// OrgHostnameHealth — GET /api/orgs/{id}/hostnames/{hostname}/health. Org-admin.
// A live reachability probe so admins can self-diagnose: does the hostname
// resolve, and does it serve HTTPS with a valid cert (i.e. did the on-demand
// cert issue and is DNS pointing at us)?
func (s *Server) OrgHostnameHealth(w http.ResponseWriter, r *http.Request) {
	orgID, ok := parseOrgID(w, r)
	if !ok {
		return
	}
	if !s.requireOrgAdmin(w, r, orgID) {
		return
	}
	host := hostnameOnly(r.PathValue("hostname"))
	var x int
	if err := s.DB.QueryRowContext(r.Context(),
		`SELECT 1 FROM org_hostnames WHERE hostname = $1 AND org_id = $2`, host, orgID).Scan(&x); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "hostname not found")
		return
	}
	writeJSON(w, http.StatusOK, checkHostnameHealth(r.Context(), host))
}

// checkHostnameHealth resolves host and, if it points at a public address,
// attempts a TLS handshake. SSRF guard: a verified hostname's DNS could still
// point at a private/loopback IP, so we never dial one — that would turn the
// probe into an internal port scanner.
func checkHostnameHealth(ctx context.Context, host string) hostnameHealthDTO {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	addrs, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil || len(addrs) == 0 {
		return hostnameHealthDTO{Note: "hostname does not resolve in DNS yet"}
	}
	out := hostnameHealthDTO{DNSOK: true}
	for _, a := range addrs {
		if ip := net.ParseIP(a); ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() {
			continue // skip private/loopback/link-local from the reported + dialled set
		}
		out.Addrs = append(out.Addrs, a)
	}
	if len(out.Addrs) == 0 {
		out.Note = "resolves to a non-public address — point DNS at this instance"
		return out
	}

	dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 4 * time.Second}, Config: &tls.Config{ServerName: host}}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, "443"))
	if err != nil {
		out.Note = "no valid HTTPS yet — the certificate issues on the first visit once DNS is correct"
		return out
	}
	_ = conn.Close()
	out.HTTPSOK = true
	return out
}

// normalizeHostname lowercases/trims a hostname and rejects anything that
// can't be an org front door: malformed hosts, the canonical host, and
// registrable apexes (custom domains are subdomain-only — an org points a
// subdomain it controls at us, never its root marketing domain, which also
// keeps us on CNAME-able names since an apex can't CNAME).
func normalizeHostname(raw string) (string, error) {
	host := strings.TrimSuffix(hostnameOnly(raw), ".")
	if host == "" {
		return "", errors.New("a hostname is required")
	}
	if len(host) > 253 || !validHostLabels(host) {
		return "", errors.New("not a valid hostname")
	}
	if host == customDomainTarget() || host == hostnameOnly(strings.TrimPrefix(strings.TrimPrefix(canonicalBaseURL(), "https://"), "http://")) {
		return "", errors.New("that hostname is reserved")
	}
	// Subdomain-only: reject a bare registrable domain (eTLD+1) and any public
	// suffix. publicsuffix is already a transitive dep (golang.org/x/net).
	if etld1, err := publicsuffix.EffectiveTLDPlusOne(host); err != nil || host == etld1 {
		return "", errors.New("use a subdomain you control (e.g. tela.example.com), not a root domain")
	}
	return host, nil
}

// validHostLabels checks DNS label structure: 1–63 chars each, alphanumeric or
// hyphen, no leading/trailing hyphen, at least two labels.
func validHostLabels(host string) bool {
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return false
	}
	for _, l := range labels {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for i := 0; i < len(l); i++ {
			c := l[i]
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// verifyHostnameTXT resolves _tela-verify.<host> and succeeds if any TXT record
// equals token. A plain TXT lookup — no SSRF surface (we never fetch the host).
func verifyHostnameTXT(ctx context.Context, host, token string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	recs, err := net.DefaultResolver.LookupTXT(ctx, verifyTXTPrefix+host)
	if err != nil {
		return err
	}
	for _, rec := range recs {
		if strings.TrimSpace(rec) == token {
			return nil
		}
	}
	return errors.New("challenge record not found")
}

func randomToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
