package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zcag/tela/backend/internal/auth"
)

// admin_domain_login.go lets an instance admin log into an org's custom domain
// (white-label front door) as themselves with one click. An org may lock its
// door to its own SSO (e.g. Microsoft Entra) — an instance admin whose identity
// isn't in that IdP otherwise has no way in. The mint endpoint (canonical host,
// instance-admin session) issues a short-lived signed token; the admin's browser
// redeems it ON the custom host, where it mints a host-bound session and drops
// the normal session cookie. From then on it's an ordinary persistent session —
// no per-visit round-trip.
//
// Security posture mirrors the print token (pdf_export.go): stateless HMAC over
// the share secret, no table, no migration. It is deliberately short-lived (60s)
// because, unlike the read-only print token, it mints a full session — kept tiny
// to bound replay if the redeem URL leaks (e.g. into browser history). It grants
// no privilege escalation: the session is the admin's own identity, and content
// stays identity-based (they see only the spaces they already can).

const adminDomainLoginTTL = 60 * time.Second

func (s *Server) adminDomainLoginKey() []byte {
	h := hmac.New(sha256.New, s.shareSecret)
	h.Write([]byte("tela-admin-domain-login-v1"))
	return h.Sum(nil)
}

// mintAdminDomainLoginToken signs "uid.oid.exp.host". host is last so its dots
// don't confuse the field split.
func (s *Server) mintAdminDomainLoginToken(userID, orgID int64, host string) string {
	payload := fmt.Sprintf("%d.%d.%d.%s", userID, orgID, time.Now().Add(adminDomainLoginTTL).Unix(), host)
	mac := hmac.New(sha256.New, s.adminDomainLoginKey())
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Server) verifyAdminDomainLoginToken(tok string) (userID, orgID int64, host string, ok bool) {
	parts := strings.SplitN(tok, ".", 2)
	if len(parts) != 2 {
		return 0, 0, "", false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return 0, 0, "", false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, 0, "", false
	}
	mac := hmac.New(sha256.New, s.adminDomainLoginKey())
	mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return 0, 0, "", false
	}
	f := strings.SplitN(string(payload), ".", 4)
	if len(f) != 4 {
		return 0, 0, "", false
	}
	uid, e1 := strconv.ParseInt(f[0], 10, 64)
	oid, e2 := strconv.ParseInt(f[1], 10, 64)
	exp, e3 := strconv.ParseInt(f[2], 10, 64)
	if e1 != nil || e2 != nil || e3 != nil || time.Now().Unix() > exp {
		return 0, 0, "", false
	}
	return uid, oid, f[3], true
}

// AdminDomainLoginMint — POST /api/orgs/{id}/hostnames/{hostname}/admin-login.
// Instance-admin only. Returns the absolute redeem URL on the org's domain.
func (s *Server) AdminDomainLoginMint(w http.ResponseWriter, r *http.Request) {
	u, ok := requireInstanceAdmin(w, r)
	if !ok {
		return
	}
	orgID, ok := parseOrgID(w, r)
	if !ok {
		return
	}
	host := hostnameOnly(r.PathValue("hostname"))
	// The host must be an ACTIVE hostname owned by this org.
	if gotOrg, ok := s.orgByHost(r.Context(), host); !ok || gotOrg != orgID {
		writeError(w, http.StatusNotFound, "not_found", "no active hostname for this org")
		return
	}
	tok := s.mintAdminDomainLoginToken(u.ID, orgID, host)
	s.audit(r.Context(), r, "admin.domain_login.mint", "org", orgID, host)
	writeJSON(w, http.StatusOK, map[string]any{
		"url": "https://" + host + "/api/auth/admin-login/redeem?t=" + url.QueryEscape(tok),
	})
}

// AdminDomainLoginRedeem — GET /api/auth/admin-login/redeem?t=…. PUBLIC path
// (under /api/auth/): self-authenticates via the token. Runs on the custom host,
// so hostOrgMiddleware has stamped the OrgContext; CreateSession binds the new
// session to it. On any failure it bounces to the login screen rather than
// leaking a reason.
func (s *Server) AdminDomainLoginRedeem(w http.ResponseWriter, r *http.Request) {
	fail := func() { http.Redirect(w, r, "/login?sso_error=admin_login", http.StatusFound) }

	uid, oid, host, ok := s.verifyAdminDomainLoginToken(r.URL.Query().Get("t"))
	if !ok {
		fail()
		return
	}
	// Token must be redeemed on the exact host it was minted for, and that host
	// must resolve (via the middleware) to the same org — so the session binds to
	// the right front door and a token can't be replayed onto another domain.
	if host != hostnameOnly(r.Host) {
		fail()
		return
	}
	oc, hasOrg := auth.OrgContextFromContext(r.Context())
	if !hasOrg || oc.OrgID != oid {
		fail()
		return
	}
	// Re-check at redeem time: still an active instance admin?
	var isAdmin, isActive int
	if err := s.DB.QueryRowContext(r.Context(),
		`SELECT is_instance_admin, is_active FROM users WHERE id = $1`, uid).
		Scan(&isAdmin, &isActive); err != nil || isAdmin != 1 || isActive != 1 {
		fail()
		return
	}
	sid, err := auth.CreateSession(r.Context(), s.DB, uid, r.UserAgent())
	if err != nil {
		fail()
		return
	}
	auth.SetSessionCookie(w, sid)
	writeAudit(r.Context(), s.DB, &uid, "admin.domain_login.redeem", "org", oid, host)
	http.Redirect(w, r, "/", http.StatusFound)
}
