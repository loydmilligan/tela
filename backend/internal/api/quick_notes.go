package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
)

// quickNotesTitle is the canonical title of the per-user scratchpad page the
// `/n` shortcut opens. Match on it to find-or-create.
const quickNotesTitle = "Quick Notes"

// QuickNotes returns the caller's "Quick Notes" page, creating it (and their
// personal space, if missing) on first use. It backs the `/n` shortcut: a
// frictionless scratchpad that always lives at the top level of the user's
// private personal space. Idempotent — repeated calls return the same page.
//
// The find-or-create runs in one tx. A double-open race (two tabs hitting `/n`
// at once) could in theory mint two pages; that's harmless for a personal
// scratchpad — `/n` then opens the lowest-id one and the stray is deletable —
// so we don't pay for a dedicated unique constraint.
func (s *Server) QuickNotes(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	// The personal space is the home for solo writing (docs/visibility-model.md);
	// ensure it exists before we hang a page off it.
	spaceID, err := EnsurePersonalSpace(ctx, s.DB, u.ID, u.Username)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "ensure personal space failed")
		return
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "begin tx failed")
		return
	}
	defer tx.Rollback()

	var id int64
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM pages
		 WHERE space_id = $1 AND parent_id IS NULL AND title = $2
		 ORDER BY id LIMIT 1`, spaceID, quickNotesTitle).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		newID, cerr := createQuickNotesPageTx(ctx, tx, spaceID)
		if cerr != nil {
			writeError(w, http.StatusInternalServerError, "internal", "create quick notes failed")
			return
		}
		id = newID
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal", "lookup quick notes failed")
		return
	}

	page, err := selectPageByIDTx(ctx, tx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch quick notes failed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "commit failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"page": page})
}

// createQuickNotesPageTx inserts the scratchpad as a top-level page appended
// after any existing roots, mirroring CreatePage's position + link-sync so the
// page is indistinguishable from a normally-created one.
func createQuickNotesPageTx(ctx context.Context, tx *sql.Tx, spaceID int64) (int64, error) {
	var maxPos sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		`SELECT MAX(position) FROM pages WHERE space_id = $1 AND parent_id IS NULL`, spaceID).
		Scan(&maxPos); err != nil {
		return 0, err
	}
	var position int64
	if maxPos.Valid {
		position = maxPos.Int64 + 1
	}
	var id int64
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO pages(space_id, parent_id, title, body, position) VALUES ($1, NULL, $2, '', $3) RETURNING id`,
		spaceID, quickNotesTitle, position).Scan(&id); err != nil {
		return 0, err
	}
	if err := syncPageLinks(ctx, tx, id, ""); err != nil {
		return 0, err
	}
	return id, nil
}
