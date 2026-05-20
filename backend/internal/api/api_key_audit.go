package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/zcag/tela/backend/internal/auth"
)

// M16.A.2 API-key audit log read surface. Writes happen asynchronously in
// auth.Middleware on the bearer-auth path; this file only exposes the read
// path used by the Settings → API Keys tab (M16.A.3) so a user can see what
// their token has been doing.

// apiKeyAuditMaxLimit is the hard cap on ?limit. 500 picked the same way as
// the share-link tree depth: large enough for a single page of "recent
// activity" UI without ever returning a giant payload that would force the
// client to paginate inside one screen.
const apiKeyAuditMaxLimit = 500

// apiKeyAuditDefaultLimit matches the task spec — 100 rows per request.
const apiKeyAuditDefaultLimit = 100

type apiKeyAuditEntry struct {
	ID         int64  `json:"id"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	StatusCode int    `json:"status_code"`
	Ts         string `json:"ts"`
}

// ListAPIKeyAudit — GET /api/api_keys/{id}/audit?limit=100&before=<ts>.
// Returns rows most-recent-first. The caller must be either the key's owner
// OR an instance-admin; everyone else gets 404 (404 over 403 keeps the key's
// existence hidden from a probe).
//
// `before` is a `YYYY-MM-DD HH:MM:SS` timestamp — matches Tela's wire format
// and lets the client paginate by passing the last-seen `ts` value back in.
// Strict less-than, so a row exactly at `before` is excluded (paginate
// boundary cleanly: pass the smallest ts you've already shown).
//
// Bearer-mode callers (the MCP server, future CLIs) need admin scope to read
// the audit log. Owner-self read via a bearer key is intentionally blocked:
// audit is a humans-only surface. A read-scope or write-scope bearer key
// reading its own audit log would let a stolen token enumerate the trail
// you'd use to detect it.
func (s *Server) ListAPIKeyAudit(w http.ResponseWriter, r *http.Request) {
	keyID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	if k, isBearer := auth.APIKeyFromContext(r.Context()); isBearer {
		if k.Scope != auth.ScopeAdmin {
			writeError(w, http.StatusForbidden, "api_key_scope", "admin scope required")
			return
		}
	}

	limit := apiKeyAuditDefaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "limit must be a positive integer")
			return
		}
		if n > apiKeyAuditMaxLimit {
			n = apiKeyAuditMaxLimit
		}
		limit = n
	}
	var beforeArg any
	if v := r.URL.Query().Get("before"); v != "" {
		if _, err := time.Parse("2006-01-02 15:04:05", v); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "before must be a 'YYYY-MM-DD HH:MM:SS' timestamp")
			return
		}
		beforeArg = v
	}

	// Authorise against the key's owner before reading any audit rows. 404 on
	// missing OR not-owned-and-not-admin, identical envelopes — a probe can't
	// distinguish "no such key" from "not yours".
	var ownerID int64
	err := s.DB.QueryRowContext(r.Context(),
		`SELECT user_id FROM api_keys WHERE id = ?`, keyID).Scan(&ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "api key not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup api key failed")
		return
	}
	if ownerID != u.ID && !u.IsInstanceAdmin {
		writeError(w, http.StatusNotFound, "not_found", "api key not found")
		return
	}

	q := `SELECT id, method, path, status_code, ts
	        FROM api_key_audit
	       WHERE api_key_id = ?`
	args := []any{keyID}
	if beforeArg != nil {
		q += ` AND ts < ?`
		args = append(args, beforeArg)
	}
	// Tie-break on id DESC so two rows with identical ts (sub-second writes)
	// still come back in a stable order — required for `before`-based
	// pagination to converge.
	q += ` ORDER BY ts DESC, id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.DB.QueryContext(r.Context(), q, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "query api_key_audit failed")
		return
	}
	defer rows.Close()
	entries := []apiKeyAuditEntry{}
	for rows.Next() {
		var e apiKeyAuditEntry
		if err := rows.Scan(&e.ID, &e.Method, &e.Path, &e.StatusCode, &e.Ts); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan audit row failed")
			return
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate audit rows failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"audit": entries})
}
