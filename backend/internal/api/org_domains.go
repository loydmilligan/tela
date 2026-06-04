package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

// orgDomainDTO is the wire shape for an auto-join domain mapping.
type orgDomainDTO struct {
	Domain    string `json:"domain"`
	OrgID     int64  `json:"org_id"`
	OrgName   string `json:"org_name"`
	OrgRole   string `json:"org_role"`
	CreatedAt string `json:"created_at"`
}

type orgDomainAddRequest struct {
	Domain  string `json:"domain"`
	OrgID   int64  `json:"org_id"`
	OrgRole string `json:"org_role"`
}

// ListOrgDomains returns every auto-join domain mapping. Instance-admin only.
func (s *Server) ListOrgDomains(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireInstanceAdmin(w, r); !ok {
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT d.domain, d.org_id, o.name, d.org_role, d.created_at
		  FROM org_email_domains d
		  JOIN orgs o ON o.id = d.org_id
		 ORDER BY d.domain ASC`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list domains failed")
		return
	}
	defer rows.Close()

	domains := []orgDomainDTO{}
	for rows.Next() {
		var d orgDomainDTO
		if err := rows.Scan(&d.Domain, &d.OrgID, &d.OrgName, &d.OrgRole, &d.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan domain row failed")
			return
		}
		domains = append(domains, d)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate domains failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"domains": domains})
}

// CreateOrgDomain maps an email domain to an org for auto-join. Instance-admin
// only. 409 if the domain is already mapped (a domain belongs to one org).
func (s *Server) CreateOrgDomain(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireInstanceAdmin(w, r); !ok {
		return
	}
	var req orgDomainAddRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	domain := normalizeDomain(req.Domain)
	if domain == "" || !strings.Contains(domain, ".") {
		writeError(w, http.StatusBadRequest, "bad_request", "a valid bare domain is required (e.g. acme.com)")
		return
	}
	if req.OrgID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "org_id is required")
		return
	}
	role := req.OrgRole
	if role == "" {
		role = orgRoleMember
	}
	if !isValidOrgRole(role) {
		writeError(w, http.StatusBadRequest, "bad_request", "org_role must be one of admin, member")
		return
	}

	ctx := r.Context()
	if exists, err := orgExists(ctx, s.DB, req.OrgID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup org failed")
		return
	} else if !exists {
		writeError(w, http.StatusNotFound, "not_found", "org not found")
		return
	}

	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO org_email_domains (domain, org_id, org_role) VALUES (?, ?, ?)`,
		domain, req.OrgID, role); err != nil {
		if isUniqueConstraintErr(err) {
			writeError(w, http.StatusConflict, "conflict", "that domain is already mapped to an org")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "create domain failed")
		return
	}
	var dto orgDomainDTO
	err := s.DB.QueryRowContext(ctx, `
		SELECT d.domain, d.org_id, o.name, d.org_role, d.created_at
		  FROM org_email_domains d JOIN orgs o ON o.id = d.org_id
		 WHERE d.domain = ?`, domain).
		Scan(&dto.Domain, &dto.OrgID, &dto.OrgName, &dto.OrgRole, &dto.CreatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch created domain failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"domain": dto})
}

// DeleteOrgDomain removes a domain mapping. Instance-admin only. Existing
// memberships created by past auto-joins are left intact.
func (s *Server) DeleteOrgDomain(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireInstanceAdmin(w, r); !ok {
		return
	}
	domain := normalizeDomain(r.PathValue("domain"))
	if domain == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "domain is required")
		return
	}
	res, err := s.DB.ExecContext(r.Context(),
		`DELETE FROM org_email_domains WHERE domain = ?`, domain)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "delete domain failed")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "domain not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// normalizeDomain lowercases/trims and strips a leading "@" so both "acme.com"
// and "@acme.com" inputs land the same.
func normalizeDomain(s string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "@")
}

// emailDomain returns the lowercased host part of an address, or "" if there's
// no single '@'.
func emailDomain(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return ""
	}
	return strings.ToLower(email[at+1:])
}

// applyAutoJoin enrolls userID into any org whose configured email domain
// matches the (already verified) address. Best-effort: a hiccup here must not
// block sign-in, so errors are logged and swallowed. INSERT OR IGNORE keeps it
// idempotent and respects existing (possibly admin-elevated) memberships.
func applyAutoJoin(ctx context.Context, db *sql.DB, userID int64, email string) {
	domain := emailDomain(email)
	if domain == "" {
		return
	}
	if _, err := db.ExecContext(ctx, `
		INSERT OR IGNORE INTO org_members (org_id, user_id, org_role)
		SELECT org_id, ?, org_role FROM org_email_domains WHERE domain = ?`,
		userID, domain); err != nil {
		log.Printf("auto-join for user %d (%s): %v", userID, domain, err)
	}
}
