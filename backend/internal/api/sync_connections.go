package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/zcag/tela/backend/internal/auth"
)

// sync_connections.go — user self-service "Connect your vault" (sync §16). A
// member mints a WebDAV sync token for THEIR OWN access (an api_keys row owned
// by them, write or read-only, optionally pinned to one space they belong to)
// and gets a ready-to-paste rclone setup. Distinct from the /api/api_keys CRUD,
// which is instance-admin-only for issuing agent keys: this surface is
// space-membership-gated personal access, the one the app's Sync settings panel
// drives. Revoke reuses the owner-gated DELETE /api/api_keys/{id}.

type syncConnectionCreateRequest struct {
	Name     string `json:"name"`
	SpaceID  *int64 `json:"space_id"`  // nil → whole workspace
	ReadOnly bool   `json:"read_only"` // false → write (two-way); true → sync-down only
}

// rcloneSetup is the copy-paste configuration the panel shows once. The raw PAT
// is embedded in config_create_command (same once-only exposure as api_keys).
type rcloneSetup struct {
	WebdavURL           string `json:"webdav_url"`
	RemoteName          string `json:"remote_name"`
	RemotePath          string `json:"remote_path"`
	ConfigCreateCommand string `json:"config_create_command"`
	SyncCommand         string `json:"sync_command"`
	ReadOnly            bool   `json:"read_only"`
	Excludes            string `json:"excludes"`
}

// CreateSyncConnection — POST /api/sync/connections. Any authenticated user mints
// a sync token for their own access; a space pin is gated on membership.
func (s *Server) CreateSyncConnection(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req syncConnectionCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not parse request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > apiKeyMaxNameLen {
		writeError(w, http.StatusBadRequest, "bad_request", "name must be 1-100 characters")
		return
	}
	scope := auth.ScopeWrite
	if req.ReadOnly {
		scope = auth.ScopeRead
	}

	ctx := r.Context()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "begin tx failed")
		return
	}
	defer tx.Rollback()

	// A space pin must name a space the caller belongs to — the token can't be
	// scoped to access the user doesn't have.
	var (
		spaceArg  any = nil
		spaceSlug string
	)
	if req.SpaceID != nil {
		if *req.SpaceID <= 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "space_id must be a positive integer")
			return
		}
		if _, err := spaceRoleTx(ctx, tx, u.ID, *req.SpaceID); errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusForbidden, "forbidden", "not a member of that space")
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "lookup membership failed")
			return
		}
		if err := tx.QueryRowContext(ctx, `SELECT slug FROM spaces WHERE id = $1`, *req.SpaceID).Scan(&spaceSlug); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "lookup space failed")
			return
		}
		spaceArg = *req.SpaceID
	}

	raw, prefix, hmacHex, err := auth.NewAPIKey(auth.LoadAPIKeySecret())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "generate token failed")
		return
	}
	var id int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO api_keys (user_id, name, key_prefix, key_hmac, scope, space_id)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		u.ID, name, prefix, hmacHex, scope, spaceArg).Scan(&id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "insert token failed")
		return
	}
	dto, err := selectAPIKeyByIDTx(ctx, tx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch created token failed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "commit failed")
		return
	}
	dto.Key = raw
	writeJSON(w, http.StatusCreated, map[string]any{
		"connection": dto,
		"rclone":     buildRcloneSetup(firstNonEmpty(u.Email, u.Username), spaceSlug, raw, req.ReadOnly),
	})
}

// ListSyncConnections — GET /api/sync/connections. The caller's own sync tokens
// (revoked rows included so the history is visible); never re-exposes the key.
func (s *Server) ListSyncConnections(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	rows, err := s.DB.QueryContext(r.Context(),
		apiKeySelectColumns+` WHERE user_id = $1 ORDER BY id DESC`, u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list connections failed")
		return
	}
	defer rows.Close()
	out := []apiKeyDTO{}
	for rows.Next() {
		dto, err := scanAPIKey(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan connection row failed")
			return
		}
		out = append(out, dto)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate connections failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": out})
}

// buildRcloneSetup assembles the copy-paste rclone configuration for a freshly
// minted token. The WebDAV URL and the --ignore-size flag (tela transforms files
// on write, so rclone must not size-check — see docs/webdav-sync.md) are
// server-authoritative, not hardcoded in the client.
func buildRcloneSetup(user, spaceSlug, rawKey string, readOnly bool) rcloneSetup {
	url := strings.TrimRight(publicBaseURL(), "/") + "/dav/"
	const remote = "tela"
	remotePath := remote + ":"
	localDir := "./tela"
	if spaceSlug != "" {
		remotePath = remote + ":" + spaceSlug
		localDir = "./" + spaceSlug
	}
	if user == "" {
		user = "you@example.com"
	}
	// Read-only → one-way pull (the token can't write, so bisync would 403 going
	// up). Two-way → bisync with --ignore-size (tela renders frontmatter + merges
	// on write, so rclone must not size-check; see docs/webdav-sync.md).
	syncCmd := fmt.Sprintf("rclone bisync %s %s --resync --ignore-size", localDir, remotePath)
	if readOnly {
		syncCmd = fmt.Sprintf("rclone sync %s %s --create-empty-src-dirs", remotePath, localDir)
	}
	return rcloneSetup{
		WebdavURL:           url,
		RemoteName:          remote,
		RemotePath:          remotePath,
		ConfigCreateCommand: fmt.Sprintf("rclone config create %s webdav url=%s vendor=other user=%s pass=%s", remote, url, user, rawKey),
		SyncCommand:         syncCmd,
		ReadOnly:            readOnly,
		Excludes:            ".DS_Store\n._*\n*.swp\n*.tmp\n~$*\nThumbs.db\n.git/**\n.obsidian/**",
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
