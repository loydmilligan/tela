package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/zcag/tela/backend/internal/atlas/core"
	"github.com/zcag/tela/backend/internal/auth"
)

// ── request / response shapes ───────────────────────────────────────────────

type atlasSecretCreateReq struct {
	Name  string            `json:"name"`
	Kind  string            `json:"kind"`           // "git" | "jira"
	Value string            `json:"value"`          // the token (write-only)
	Meta  map[string]string `json:"meta,omitempty"` // git: {username}; jira: {email, base_url}
}

// atlasSecretDTO is the read shape: the token value is NEVER returned (write-only),
// only the non-secret name/kind/meta so the UI can show what's attached.
type atlasSecretDTO struct {
	ID        int64             `json:"id"`
	SpaceID   int64             `json:"space_id"`
	Name      string            `json:"name"`
	Kind      string            `json:"kind"`
	Meta      map[string]string `json:"meta,omitempty"`
	CreatedAt string            `json:"created_at"`
}

var atlasSecretKinds = map[string]bool{string(core.SourceGit): true, string(core.SourceJira): true}

// ── space-scoped: secret CRUD (all management-gated) ─────────────────────────

// CreateAtlasSecret stores a source credential in a space. The value (token) is
// write-only — it lands here on create and is blanked on every read.
// POST /api/spaces/{id}/atlas/secrets — management-gated.
func (s *Server) CreateAtlasSecret(w http.ResponseWriter, r *http.Request) {
	spaceID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	k, _ := auth.APIKeyFromContext(r.Context())
	if ae := s.atlasSpaceManageErr(r.Context(), u, k, spaceID); ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	var req atlasSecretCreateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_secret", "name is required")
		return
	}
	if !atlasSecretKinds[req.Kind] {
		writeError(w, http.StatusBadRequest, "invalid_kind", "kind must be 'git' or 'jira'")
		return
	}
	if req.Value == "" {
		writeError(w, http.StatusBadRequest, "invalid_secret", "value (token) is required")
		return
	}
	// jira authenticates as Basic base64(email:token) — the email is non-secret and
	// must travel in meta so the connector can build the header.
	if req.Kind == string(core.SourceJira) && (req.Meta == nil || req.Meta["email"] == "") {
		writeError(w, http.StatusBadRequest, "invalid_secret", "jira secret requires meta.email")
		return
	}
	metaJSON := ""
	if len(req.Meta) > 0 {
		b, _ := json.Marshal(req.Meta)
		metaJSON = string(b)
	}
	var id int64
	var createdAt string
	err := s.DB.QueryRowContext(r.Context(), `
		INSERT INTO atlas_secrets (space_id, name, kind, value, meta_json)
		VALUES ($1,$2,$3,$4,$5) RETURNING id, created_at`,
		spaceID, req.Name, req.Kind, req.Value, metaJSON).Scan(&id, &createdAt)
	if err != nil {
		if isUniqueConstraintErr(err) {
			writeError(w, http.StatusConflict, "duplicate", "a secret with this name already exists in the space")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "create secret failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"secret": atlasSecretDTO{
		ID: id, SpaceID: spaceID, Name: req.Name, Kind: req.Kind, Meta: req.Meta, CreatedAt: createdAt,
	}})
}

// ListAtlasSecrets lists a space's secrets with the value BLANKED (never returned).
// GET /api/spaces/{id}/atlas/secrets — management-gated.
func (s *Server) ListAtlasSecrets(w http.ResponseWriter, r *http.Request) {
	spaceID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	k, _ := auth.APIKeyFromContext(r.Context())
	if ae := s.atlasSpaceManageErr(r.Context(), u, k, spaceID); ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT id, space_id, name, kind, meta_json, created_at
		   FROM atlas_secrets WHERE space_id=$1 ORDER BY id`, spaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list secrets failed")
		return
	}
	defer rows.Close()
	out := []atlasSecretDTO{}
	for rows.Next() {
		var d atlasSecretDTO
		var metaJSON string
		if err := rows.Scan(&d.ID, &d.SpaceID, &d.Name, &d.Kind, &metaJSON, &d.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan secret failed")
			return
		}
		d.Meta = parseSecretMeta(metaJSON)
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, map[string]any{"secrets": out})
}

// DeleteAtlasSecret removes a secret (its FK on atlas_sources nulls — referencing
// sources fall back to public/no-auth). DELETE /api/atlas/secrets/{id} — management.
func (s *Server) DeleteAtlasSecret(w http.ResponseWriter, r *http.Request) {
	secretID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	spaceID, err := s.atlasSecretSpace(r.Context(), secretID)
	if err != nil {
		s.atlasResolveErr(w, err)
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	k, _ := auth.APIKeyFromContext(r.Context())
	if ae := s.atlasSpaceManageErr(r.Context(), u, k, spaceID); ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), `DELETE FROM atlas_secrets WHERE id=$1`, secretID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "delete secret failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── resolution (executor side) ──────────────────────────────────────────────

// loadAtlasSecret reads a secret's token + parsed meta by id. Mirrors atlas's
// internal/secret.ResolveSource: the value is the access token, meta carries the
// non-secret adornments (jira email/base_url, git username).
func loadAtlasSecret(ctx context.Context, db *sql.DB, id int64) (value string, meta map[string]string, err error) {
	var metaJSON string
	err = db.QueryRowContext(ctx, `SELECT value, meta_json FROM atlas_secrets WHERE id=$1`, id).Scan(&value, &metaJSON)
	if err != nil {
		return "", nil, err
	}
	return value, parseSecretMeta(metaJSON), nil
}

// applyAtlasSecret stuffs a resolved secret onto a run's transient core.Source so
// the connector authenticates without the Connector interface ever carrying a
// credential. jira reads SecretValue (token) + SecretMeta["email"] + Location
// (base) directly. git authenticates via the clone URL, not SecretValue, so the
// token is injected into Location here (a no-op for an empty value or a non-http
// location, e.g. a local path).
func applyAtlasSecret(src *core.Source, value string, meta map[string]string) {
	src.SecretValue = value
	src.SecretMeta = meta
	if src.Type == core.SourceGit && value != "" {
		username := ""
		if meta != nil {
			username = meta["username"]
		}
		src.Location = injectGitAuth(src.Location, username, value)
	}
}

// injectGitAuth embeds credentials in an https(s) git URL's userinfo so `git
// clone`/`ls-remote` authenticate non-interactively. A token-only secret becomes
// the userinfo (works for a GitHub PAT); a username (GitHub "x-access-token",
// GitLab "oauth2", or a real login) becomes user:token. Non-http schemes (ssh,
// local paths) are returned unchanged — auth there isn't URL-borne.
func injectGitAuth(location, username, token string) string {
	u, err := url.Parse(location)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return location
	}
	if username != "" {
		u.User = url.UserPassword(username, token)
	} else {
		u.User = url.User(token)
	}
	return u.String()
}

func parseSecretMeta(metaJSON string) map[string]string {
	if metaJSON == "" {
		return nil
	}
	var m map[string]string
	if json.Unmarshal([]byte(metaJSON), &m) != nil {
		return nil
	}
	return m
}

func (s *Server) atlasSecretSpace(ctx context.Context, secretID int64) (int64, error) {
	var spaceID int64
	err := s.DB.QueryRowContext(ctx, `SELECT space_id FROM atlas_secrets WHERE id=$1`, secretID).Scan(&spaceID)
	return spaceID, err
}
