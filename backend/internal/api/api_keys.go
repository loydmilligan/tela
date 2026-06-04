package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/zcag/tela/backend/internal/auth"
)

// M16.A.1 API keys. Manage bearer tokens used by headless agents (precursor
// to the Tela MCP server). Three scopes (read/write/admin) and optional
// per-space restriction. CRUD lives under /api/api_keys + /api/api_keys/{id};
// the actual bearer auth path is in internal/auth/api_key.go + middleware.go.

const (
	apiKeyMaxNameLen = 100
)

// apiKeyDTO is the management envelope returned by list/create/get. NEVER
// carries key_hmac. The full raw key (`key` field below) is populated ONLY on
// the POST response — every subsequent read returns nil for it.
type apiKeyDTO struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	KeyPrefix  string  `json:"key_prefix"`
	Scope      string  `json:"scope"`
	SpaceID    *int64  `json:"space_id"`
	LastUsedAt *string `json:"last_used_at"`
	ExpiresAt  *string `json:"expires_at"`
	CreatedAt  string  `json:"created_at"`
	RevokedAt  *string `json:"revoked_at"`

	// Key is the raw `tela_pat_...` value. Only populated on the CREATE
	// response; list/get responses leave it empty (omitempty would also work
	// but explicit "" is clearer at the boundary).
	Key string `json:"key,omitempty"`
}

type apiKeyCreateRequest struct {
	Name      string  `json:"name"`
	Scope     string  `json:"scope"`
	SpaceID   *int64  `json:"space_id"`
	ExpiresAt *string `json:"expires_at"`
}

// requireAPIKeyAdmin gates the /api/api_keys CRUD surface. The caller must
// either be on a cookie session that belongs to an instance-admin, OR be on
// a bearer token whose scope is admin. Bearer tokens with read/write scope
// 403 here even if the underlying user is an instance-admin — the scope is a
// ceiling, not a floor.
func requireAPIKeyAdmin(w http.ResponseWriter, r *http.Request) (*auth.User, bool) {
	u, ok := requireUser(w, r)
	if !ok {
		return nil, false
	}
	if k, isBearer := auth.APIKeyFromContext(r.Context()); isBearer {
		if k.Scope != auth.ScopeAdmin {
			writeError(w, http.StatusForbidden, "api_key_scope", "admin scope required")
			return nil, false
		}
	}
	if !u.IsInstanceAdmin {
		// Non-admin users can still manage their OWN keys via /api/users/me/
		// keys later; the global /api/api_keys surface is admin-only for v0.
		writeError(w, http.StatusForbidden, "forbidden", "instance admin required")
		return nil, false
	}
	return u, true
}

// CreateAPIKey — POST /api/api_keys. Generates a fresh tela_pat_ token,
// stores its HMAC + prefix, and returns the raw key ONCE. Subsequent reads
// never re-expose the secret.
func (s *Server) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	u, ok := requireAPIKeyAdmin(w, r)
	if !ok {
		return
	}
	var req apiKeyCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not parse request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > apiKeyMaxNameLen {
		writeError(w, http.StatusBadRequest, "bad_request", "name must be 1-100 characters")
		return
	}
	scope := strings.TrimSpace(req.Scope)
	if scope != auth.ScopeRead && scope != auth.ScopeWrite && scope != auth.ScopeAdmin {
		writeError(w, http.StatusBadRequest, "bad_request", "scope must be one of read, write, admin")
		return
	}

	var expiresArg any = nil
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		normalised, err := parseFutureExpires(*req.ExpiresAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "expires_at must be a future YYYY-MM-DD HH:MM:SS")
			return
		}
		expiresArg = normalised
	}

	ctx := r.Context()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "begin tx failed")
		return
	}
	defer tx.Rollback()

	// Validate optional space_id exists. Don't gate on membership here — an
	// instance admin issuing a key for a space they're not in is a legitimate
	// scenario (handing the key off to an agent that will operate on behalf
	// of an existing space owner).
	var spaceArg any = nil
	if req.SpaceID != nil {
		if *req.SpaceID <= 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "space_id must be a positive integer")
			return
		}
		var x int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM spaces WHERE id = $1`, *req.SpaceID).Scan(&x)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusBadRequest, "space_not_found", "space not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "lookup space failed")
			return
		}
		spaceArg = *req.SpaceID
	}

	raw, prefix, hmacHex, err := auth.NewAPIKey(auth.LoadAPIKeySecret())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "generate api key failed")
		return
	}

	var id int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO api_keys (user_id, name, key_prefix, key_hmac, scope, space_id, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		u.ID, name, prefix, hmacHex, scope, spaceArg, expiresArg).Scan(&id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "insert api key failed")
		return
	}
	dto, err := selectAPIKeyByIDTx(ctx, tx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch created api key failed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "commit failed")
		return
	}
	dto.Key = raw
	writeJSON(w, http.StatusCreated, map[string]any{"api_key": dto})
}

// ListAPIKeys — GET /api/api_keys. Instance-admin only: returns every key
// (revoked rows included so the audit trail is visible). Never includes the
// raw token or HMAC.
func (s *Server) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAPIKeyAdmin(w, r); !ok {
		return
	}
	ctx := r.Context()
	rows, err := s.DB.QueryContext(ctx, apiKeySelectColumns+` ORDER BY id DESC`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list api keys failed")
		return
	}
	defer rows.Close()

	out := []apiKeyDTO{}
	for rows.Next() {
		dto, err := scanAPIKey(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan api key row failed")
			return
		}
		out = append(out, dto)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate api keys failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"api_keys": out})
}

// DeleteAPIKey — DELETE /api/api_keys/{id}. Soft-revoke. Instance-admin can
// revoke any key; the key's owner can also revoke their own. Idempotent: a
// second DELETE on an already-revoked key returns 204 without re-stamping
// revoked_at.
func (s *Server) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	keyID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	// Bearer-mode callers must hold admin scope; the user-self-revoke path
	// is reachable from cookie sessions only (a non-admin user cannot have
	// reached this handler via bearer — non-admin bearer means scope is
	// read/write, neither of which can manage api_keys).
	if k, isBearer := auth.APIKeyFromContext(r.Context()); isBearer {
		if k.Scope != auth.ScopeAdmin {
			writeError(w, http.StatusForbidden, "api_key_scope", "admin scope required")
			return
		}
	}

	ctx := r.Context()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "begin tx failed")
		return
	}
	defer tx.Rollback()

	var (
		ownerID   int64
		revokedAt sql.NullString
	)
	err = tx.QueryRowContext(ctx,
		`SELECT user_id, revoked_at FROM api_keys WHERE id = $1`, keyID).
		Scan(&ownerID, &revokedAt)
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
	if revokedAt.Valid {
		// Idempotent — already revoked, second DELETE is a no-op 204.
		if err := tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "commit failed")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE api_keys SET revoked_at = tela_now() WHERE id = $1`,
		keyID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "revoke api key failed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "commit failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

const apiKeySelectColumns = `
	SELECT id, name, key_prefix, scope, space_id,
	       last_used_at, expires_at, created_at, revoked_at
	  FROM api_keys`

func scanAPIKey(r rowScanner) (apiKeyDTO, error) {
	var (
		dto       apiKeyDTO
		spaceID   sql.NullInt64
		lastUsed  sql.NullString
		expiresAt sql.NullString
		revokedAt sql.NullString
	)
	if err := r.Scan(&dto.ID, &dto.Name, &dto.KeyPrefix, &dto.Scope, &spaceID,
		&lastUsed, &expiresAt, &dto.CreatedAt, &revokedAt); err != nil {
		return apiKeyDTO{}, err
	}
	if spaceID.Valid {
		v := spaceID.Int64
		dto.SpaceID = &v
	}
	dto.LastUsedAt = nullableString(lastUsed)
	dto.ExpiresAt = nullableString(expiresAt)
	dto.RevokedAt = nullableString(revokedAt)
	return dto, nil
}

func selectAPIKeyByIDTx(ctx context.Context, tx *sql.Tx, id int64) (apiKeyDTO, error) {
	row := tx.QueryRowContext(ctx, apiKeySelectColumns+` WHERE id = $1`, id)
	return scanAPIKey(row)
}
