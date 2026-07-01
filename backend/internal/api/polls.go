package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/zcag/tela/backend/internal/auth"
	"github.com/zcag/tela/backend/internal/pollmd"
)

// Poll voting. A poll's definition AND its votes live in pages.body as a
// `:::poll{id}` directive (see internal/pollmd) — there is no votes table.
// Casting a vote is a structured, row-locked edit of that block, followed by the
// same collab-overlay reset an agent/out-of-band write uses so live editors
// re-seed. Deliberately churn-free: unlike a normal edit it snapshots NO
// revision, notifies NO one, and queues NO reindex — a vote isn't authored
// content, so it shouldn't spam history, the activity feed, or RAG.

type voteRequest struct {
	// Choice is the option label to vote for; "" retracts the caller's vote.
	Choice string `json:"choice"`
}

// VotePoll handles POST /api/pages/{id}/polls/{pollId}/vote.
func (s *Server) VotePoll(w http.ResponseWriter, r *http.Request) {
	pageID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	pollID := r.PathValue("pollId")
	if strings.TrimSpace(pollID) == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "missing poll id")
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req voteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not parse request body")
		return
	}
	k, _ := auth.APIKeyFromContext(r.Context())
	if ae := s.votePollCore(r.Context(), u, k, pageID, pollID, req.Choice); ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) votePollCore(ctx context.Context, u *auth.User, k *auth.APIKey, pageID int64, pollID, choice string) *apiErr {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return &apiErr{http.StatusInternalServerError, "internal", "begin tx failed"}
	}
	defer tx.Rollback()

	existing, err := selectPageByIDTx(ctx, tx, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return &apiErr{http.StatusForbidden, "forbidden", "not a member"}
	}
	if err != nil {
		return &apiErr{http.StatusInternalServerError, "internal", "lookup page failed"}
	}
	// Voting is a body write, so it needs edit access — polls are editors-only by
	// design (the vote is authored into the doc).
	if ae := s.requireEditTx(ctx, tx, u, k, existing.SpaceID); ae != nil {
		return ae
	}

	newBody, changed, err := pollmd.ApplyVote(existing.Body, pollID, choice, u.Username)
	switch {
	case errors.Is(err, pollmd.ErrPollNotFound):
		return &apiErr{http.StatusNotFound, "not_found", "poll not found on this page"}
	case errors.Is(err, pollmd.ErrOptionNotFound):
		return &apiErr{http.StatusBadRequest, "bad_request", "no such poll option"}
	case err != nil:
		return &apiErr{http.StatusBadRequest, "bad_request", err.Error()}
	}
	if !changed {
		return nil // idempotent — vote already recorded as requested
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE pages SET body = $1, updated_at = tela_now() WHERE id = $2`,
		newBody, pageID); err != nil {
		return &apiErr{http.StatusInternalServerError, "internal", "vote write failed"}
	}
	if err := tx.Commit(); err != nil {
		return &apiErr{http.StatusInternalServerError, "internal", "commit failed"}
	}
	// Out-of-band body change → drop the Yjs overlay so any open editor re-seeds
	// from the body that now carries the vote (DB-wins, same as an agent write).
	if err := s.rooms.resetPage(ctx, s.DB, pageID); err != nil {
		// Non-fatal: the vote is committed; a live editor just won't see it until
		// its next reload.
		return nil
	}
	return nil
}

// --- handle resolution -------------------------------------------------------

type resolveUsersRequest struct {
	Handles []string `json:"handles"`
}

type resolvedUser struct {
	ID     int64  `json:"id"`
	Handle string `json:"handle"`
	Name   string `json:"name"`
}

// ResolveUsers handles POST /api/users/resolve — batch username → display
// identity for any authenticated user. Reusable identity primitive (poll voters
// now, @-mentions later): the reader parses `@username` from a poll body and
// needs names + avatars. Returns only public-ish identity (id, handle, name),
// never email or membership, so it leaks nothing beyond the shared handle.
func (s *Server) ResolveUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	var req resolveUsersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not parse request body")
		return
	}
	handles := normalizeHandles(req.Handles)
	if len(handles) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"users": []resolvedUser{}})
		return
	}
	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT id, username, COALESCE(NULLIF(display_name, ''), username)
		   FROM users
		  WHERE username = ANY($1) AND is_active = 1`, handles)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "resolve failed")
		return
	}
	defer rows.Close()
	out := []resolvedUser{}
	for rows.Next() {
		var ru resolvedUser
		if err := rows.Scan(&ru.ID, &ru.Handle, &ru.Name); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "resolve scan failed")
			return
		}
		out = append(out, ru)
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

// normalizeHandles strips a leading '@', trims, drops blanks/dupes, and caps the
// batch so one request can't fan out unbounded.
func normalizeHandles(in []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, h := range in {
		h = strings.TrimPrefix(strings.TrimSpace(h), "@")
		if h == "" {
			continue
		}
		if _, dup := seen[h]; dup {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
		if len(out) >= 200 {
			break
		}
	}
	return out
}
