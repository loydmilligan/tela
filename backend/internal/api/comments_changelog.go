package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/zcag/tela/backend/internal/auth"
)

// Per-page auto-changelog. Every interactive save writes a SYSTEM change-comment
// on the page, so a page self-documents its history: the changelog footer is then
// just a comment query (`type: change`, scoped to the page) — no new rendering
// primitive, and manual change-comments (decisions, deprecations) interleave with
// the automatic ones in one stream.
//
// Two things make this safe, and both are deliberate:
//
//   - SILENT. It goes through createCommentCore with System:true, which skips the
//     notification fan-out entirely. Without that, an auto-comment per save would
//     reach every follower over in-app + email + ntfy PHONE PUSH on every edit.
//     Note the brief's suggested mitigation — "mirror the notification
//     page_updated throttle" (CollapseUnread) — could not work here: it collapses
//     unread IN-APP rows keyed on read-state (which comments have no analogue
//     for), and emitNotifications applies it only to insertInApp — dispatchEmails
//     and dispatchNtfy are called independently and would still fire per save.
//     Suppression at the source is the only thing that actually holds.
//
//   - DEBOUNCED by a real time window. A flurry of saves collapses into ONE entry
//     by UPDATEing the author's own recent entry in place instead of appending.
//
// The summary is cheap and structured — no model call on the write path. A rich
// LLM-summarized diff is a separate, on-demand/batched concern.

// changelogDebounceMinutes is how long an author's change-comment stays open for
// amendment. Within the window a further save updates that entry (bumping its
// datetime) instead of appending a new one, so "typed for ten minutes" reads as
// one change, not forty. Beyond it, the next save starts a fresh entry.
const changelogDebounceMinutes = 10

// changelogCommentType is the props `type` value the footer query filters on.
const changelogCommentType = "change"

// autoChangeComment records one page edit in the page's changelog. Best-effort:
// it must never fail the save that triggered it (mirrors the notification
// helpers), so every error is logged and swallowed. Call AFTER the update tx
// commits, on the interactive edit path only — a vault sync must not narrate
// itself into every page's history.
func (s *Server) autoChangeComment(ctx context.Context, u *auth.User, k *auth.APIKey, pageID int64, agentWrite bool) {
	actor := "manual edit"
	if agentWrite {
		actor = "agent edit"
	}
	// Cheap + structured, per the design: who and what kind, no diff model call.
	summary := fmt.Sprintf("Edited by %s (%s)", u.Username, actor)

	// Debounce: if this author already has an open (recent) system change-comment
	// on this page, amend it rather than appending a second one. Scoped to the
	// author so two people editing concurrently keep separate entries.
	var existingID int64
	err := s.DB.QueryRowContext(ctx, `
		SELECT id FROM comments
		 WHERE page_id = $1 AND author_id = $2 AND deleted_at IS NULL
		   AND props->>'type' = $3
		   AND props->>'auto' = 'true'
		   -- Datetimes are TEXT 'YYYY-MM-DD HH24:MI:SS' UTC (the SQLite-era
		   -- convention), so compare in that shape. make_interval keeps the
		   -- window a bound int arg — pgx can't encode an int into a text concat.
		   AND created_at > to_char((now() at time zone 'utc') - make_interval(mins => $4),
		                            'YYYY-MM-DD HH24:MI:SS')
		 ORDER BY created_at DESC, id DESC
		 LIMIT 1`,
		pageID, u.ID, changelogCommentType, changelogDebounceMinutes).Scan(&existingID)
	switch {
	case err == nil:
		// Amend in place: refresh the recorded time so the entry tracks the LAST
		// save in the flurry, and bump the edit count so the record stays honest
		// about being a collapse of several saves.
		if _, uErr := s.DB.ExecContext(ctx, `
			UPDATE comments
			   SET updated_at = tela_now(),
			       props = props
			         || jsonb_build_object('datetime', tela_now())
			         || jsonb_build_object('edits',
			              coalesce((props->>'edits')::int, 1) + 1)
			 WHERE id = $1`, existingID); uErr != nil {
			slog.Error("changelog: amend entry", "comment_id", existingID, "err", uErr)
		}
		return
	case !errors.Is(err, sql.ErrNoRows):
		slog.Error("changelog: lookup open entry", "page_id", pageID, "err", err)
		return
	}

	// No open entry — start a new one.
	body := summary
	_, ae := s.createCommentCore(ctx, u, k, pageID, commentCreateRequest{
		Body: body,
		Props: map[string]any{
			"type": changelogCommentType,
			// change_summary, NOT summary: pages.props.summary is the page's
			// ABSTRACT (what the page is about, written by the auto-summarizer);
			// this is what CHANGED in one edit. Different lanes, different
			// meanings — sharing the key would invite conflating them (and would
			// be a trap if the summarizer ever generalized beyond pages).
			"change_summary": summary,
			// auto distinguishes a server-written entry from a hand-written
			// change-comment, and is what the debounce lookup keys on — amending
			// someone's hand-written decision comment would be wrong.
			"auto": true,
			// No `datetime` on a fresh entry: created_at IS the event time. It only
			// appears once the entry is amended, which is exactly the convention —
			// datetime overrides created_at when the two diverge.
			"edits": 1,
		},
	}, commentCreateOpts{System: true})
	if ae != nil {
		slog.Error("changelog: write entry", "page_id", pageID, "code", ae.Code, "err", ae.Message)
		return
	}
}
