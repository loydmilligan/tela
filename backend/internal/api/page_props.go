package api

import (
	"context"
	"database/sql"
	"errors"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/zcag/tela/backend/internal/auth"
	"github.com/zcag/tela/backend/internal/pagemd"
)

// Bound-field write-back. A ` ```field ` block in a page body is a pointer to a
// key in pages.props; flipping the widget in the read view PATCHes that one key
// through here (see docs/page-properties.md, "PATCH /api/pages/{id}/props").
// Modeled on the poll vote (polls.go): a dedicated, editor-gated mutation. Two
// deliberate differences from the poll path:
//
//   - It writes pages.props (JSONB) via a server-side shallow-merge
//     (props || $1::jsonb) rather than the body, so one field flip can't clobber
//     a concurrent prop edit — unlike Replace-semantics PATCH /api/pages/{id}.
//   - It does NOT reset the Yjs room. Props ride the comments lane (REST-only,
//     decoupled from the collaborative doc — page-properties.md); the body is
//     unchanged, and the read view refreshes via query invalidation. The poll's
//     resetPage exists only because a vote rewrites the body; there is no overlay
//     to drop here, and dropping it would needlessly evict live editors.
//
// Churn-free like the poll vote: no revision snapshot, no notification, no
// reindex — a field flip isn't authored content.

type setPagePropRequest struct {
	Key string `json:"key"`
	// Value is stored verbatim as JSON (toggle → bool, select/text → string), so
	// props @> containment filters stay predictable.
	Value any `json:"value"`
}

// SetPageProp handles PATCH /api/pages/{id}/props — shallow-merge a single
// props key. Session-gated (registered inside the authed block, not under an
// IsPublicPath prefix); the core re-checks edit access in-tx.
func (s *Server) SetPageProp(w http.ResponseWriter, r *http.Request) {
	pageID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req setPagePropRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not parse request body")
		return
	}
	k, _ := auth.APIKeyFromContext(r.Context())
	if _, ae := s.setPagePropCore(r.Context(), u, k, pageID, req.Key, req.Value); ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// setPagePropCore shallow-merges one key into pages.props and returns the FULL
// merged bag. Both front doors call it: the HTTP handler discards the bag (the
// read view refetches the page anyway), while the MCP set_prop tool returns it
// so an agent can see, in the same round trip, that its sibling keys survived —
// the whole point of the single-key merge over update_page's replace.
func (s *Server) setPagePropCore(ctx context.Context, u *auth.User, k *auth.APIKey, pageID int64, key string, value any) (map[string]any, *apiErr) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, &apiErr{http.StatusBadRequest, "bad_request", "missing prop key"}
	}
	// Reserved keys are column-owned / derived and never live in the bag. A
	// targeted verb should reject rather than silently drop them, so a caller
	// learns their write went nowhere. FilterReserved deletes any reserved key;
	// an emptied map means the one key we were handed was reserved.
	if len(pagemd.FilterReserved(map[string]any{key: value})) == 0 {
		return nil, &apiErr{http.StatusBadRequest, "bad_request", "reserved prop key"}
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, &apiErr{http.StatusInternalServerError, "internal", "begin tx failed"}
	}
	defer tx.Rollback()

	existing, err := selectPageByIDTx(ctx, tx, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &apiErr{http.StatusForbidden, "forbidden", "not a member"}
	}
	if err != nil {
		return nil, &apiErr{http.StatusInternalServerError, "internal", "lookup page failed"}
	}
	// A field write is a page edit — editors-only, same gate as the poll vote and
	// the Replace PATCH (docs/access-model.md invariant 4: one resolution path).
	if ae := s.requireEditTx(ctx, tx, u, k, existing.SpaceID); ae != nil {
		return nil, ae
	}

	// Shallow-merge one key server-side (atomic per-statement; no read-modify-
	// write clobber of a concurrent prop write). Bind props as a JSON string with
	// a ::jsonb cast — pgx encodes []byte as bytea (CLAUDE.md pgx gotcha).
	// RETURNING hands back the post-merge bag from the same statement — no second
	// read, so no window for a concurrent write to make the returned bag a lie.
	merge := propsJSON(map[string]any{key: value})
	var mergedRaw []byte
	err = tx.QueryRowContext(ctx,
		`UPDATE pages SET props = props || $1::jsonb, updated_at = tela_now() WHERE id = $2 RETURNING props`,
		merge, pageID).Scan(&mergedRaw)
	// No row came back — the page vanished between the lookup above and here.
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &apiErr{http.StatusNotFound, "not_found", "page not found"}
	}
	if err != nil {
		return nil, &apiErr{http.StatusInternalServerError, "internal", "prop write failed"}
	}
	merged := map[string]any{}
	if len(mergedRaw) > 0 {
		if err := json.Unmarshal(mergedRaw, &merged); err != nil {
			return nil, &apiErr{http.StatusInternalServerError, "internal", "decode props failed"}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, &apiErr{http.StatusInternalServerError, "internal", "commit failed"}
	}
	return merged, nil
}
