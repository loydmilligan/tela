package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/zcag/tela/backend/internal/auth"
)

// Notifications: "something happened that a specific user should know about."
// Generic over subject + type so new event kinds are additive; best-effort and
// access-gated at emit. See docs/notifications.md.

// Event types. Text codes (not a DB enum) so a new kind is additive — add a
// constant, an emit site, and a frontend render case.
const (
	notifMention      = "mention"
	notifPageUpdated  = "page_updated"
	notifSpaceAdded   = "space_added"
	notifCommentReply = "comment_reply"
)

// Delivery channels. Only inapp is delivered today; email prefs are stored for
// when the email channel ships.
const (
	channelInApp = "inapp"
	channelEmail = "email"
)

// userMentionRE matches the canonical on-wire person mention the editor inserts:
// `tela://user/{id}`. Mirrors wikiLinkRE for pages.
var userMentionRE = regexp.MustCompile(`tela://user/([0-9]+)`)

// parseUserMentions returns the distinct positive user ids mentioned in body.
func parseUserMentions(body string) []int64 {
	matches := userMentionRE.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(matches))
	ids := make([]int64, 0, len(matches))
	for _, m := range matches {
		n, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil || n <= 0 {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		ids = append(ids, n)
	}
	return ids
}

// notificationInput is one notification to write. Emission policy:
//   - DedupKey != ""    → one-ever per (user, key): ON CONFLICT DO NOTHING.
//     For one-shot events (a mention on a page).
//   - CollapseUnread    → at most one UNREAD per (user, type, subject) at a
//     time; once read, the next event makes a fresh row. For recurring events
//     (a followed page changed) so rapid edits don't pile up.
//   - neither           → always insert.
type notificationInput struct {
	UserID        int64
	Type          string
	ActorID       *int64
	SubjectKind   string
	SubjectID     int64
	SpaceID       *int64
	Data          map[string]any
	DedupKey      string
	CollapseUnread bool
}

// inAppEnabled reports whether the user wants in-app notifications of this event
// type. Opt-out: absence of a row (or a lookup error) means enabled — better to
// over-notify than silently drop.
func (s *Server) inAppEnabled(ctx context.Context, userID int64, eventType string) bool {
	var enabled int
	err := s.DB.QueryRowContext(ctx,
		`SELECT enabled FROM notification_prefs WHERE user_id = $1 AND event_type = $2 AND channel = $3`,
		userID, eventType, channelInApp).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return true
	}
	if err != nil {
		slog.Error("notification pref lookup", "event_type", eventType, "user_id", userID, "err", err)
		return true
	}
	return enabled == 1
}

// emitNotifications writes notifications best-effort, after per-user preference
// gating. Any error is logged, never surfaced — a notification must never fail
// the action that triggered it. Call AFTER the triggering tx commits.
func (s *Server) emitNotifications(ctx context.Context, ins ...notificationInput) {
	for _, in := range ins {
		if !s.inAppEnabled(ctx, in.UserID, in.Type) {
			continue
		}
		data := in.Data
		if data == nil {
			data = map[string]any{}
		}
		payload, err := json.Marshal(data)
		if err != nil {
			slog.Error("notification marshal", "type", in.Type, "err", err)
			continue
		}
		switch {
		case in.CollapseUnread:
			// Skip if the recipient already has an unread one for this subject.
			_, err = s.DB.ExecContext(ctx, `
				INSERT INTO notifications
				  (user_id, type, actor_id, subject_kind, subject_id, space_id, data)
				SELECT $1, $2, $3, $4, $5, $6, $7::jsonb
				 WHERE NOT EXISTS (
				   SELECT 1 FROM notifications
				    WHERE user_id = $1 AND type = $2 AND subject_kind = $4 AND subject_id = $5
				      AND read_at IS NULL
				 )`,
				in.UserID, in.Type, nullableInt64(in.ActorID), in.SubjectKind, in.SubjectID,
				nullableInt64(in.SpaceID), string(payload))
		case in.DedupKey != "":
			_, err = s.DB.ExecContext(ctx, `
				INSERT INTO notifications
				  (user_id, type, actor_id, subject_kind, subject_id, space_id, data, dedup_key)
				VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8)
				ON CONFLICT (user_id, dedup_key) WHERE dedup_key IS NOT NULL DO NOTHING`,
				in.UserID, in.Type, nullableInt64(in.ActorID), in.SubjectKind, in.SubjectID,
				nullableInt64(in.SpaceID), string(payload), in.DedupKey)
		default:
			_, err = s.DB.ExecContext(ctx, `
				INSERT INTO notifications
				  (user_id, type, actor_id, subject_kind, subject_id, space_id, data)
				VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)`,
				in.UserID, in.Type, nullableInt64(in.ActorID), in.SubjectKind, in.SubjectID,
				nullableInt64(in.SpaceID), string(payload))
		}
		if err != nil {
			slog.Error("emit notification", "type", in.Type, "user_id", in.UserID, "err", err)
		}
	}
}

// notifyPageMentions emits a `mention` notification to each user @-mentioned in
// a page's body — except the author, and only to users with access to the space
// (so the data payload never leaks a title to a non-member). Idempotent per
// (page, user), so re-saving the page doesn't re-notify.
func (s *Server) notifyPageMentions(ctx context.Context, actor *auth.User, pageID, spaceID int64, title, body string) {
	ids := parseUserMentions(body)
	if len(ids) == 0 {
		return
	}
	// Resolve mentioned ids → those who aren't the author and can see the space.
	// Dynamic placeholders (this codebase avoids array params).
	ph := make([]string, len(ids))
	args := make([]any, 0, len(ids)+2)
	args = append(args, spaceID, actor.ID)
	for i, id := range ids {
		args = append(args, id)
		ph[i] = "$" + strconv.Itoa(i+3)
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT DISTINCT user_id FROM space_access
		 WHERE space_id = $1 AND user_id <> $2 AND user_id IN (`+strings.Join(ph, ",")+`)`, args...)
	if err != nil {
		slog.Error("notify mentions: resolve recipients", "page_id", pageID, "err", err)
		return
	}
	defer rows.Close()
	var recipients []int64
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			slog.Error("notify mentions: scan recipient", "err", err)
			return
		}
		recipients = append(recipients, uid)
	}
	if len(recipients) == 0 {
		return
	}
	actorID := actor.ID
	out := make([]notificationInput, 0, len(recipients))
	for _, uid := range recipients {
		out = append(out, notificationInput{
			UserID:      uid,
			Type:        notifMention,
			ActorID:     &actorID,
			SubjectKind: "page",
			SubjectID:   pageID,
			SpaceID:     &spaceID,
			Data:        map[string]any{"page_title": title, "actor_username": actor.Username},
			DedupKey:    "mention:page:" + strconv.FormatInt(pageID, 10) + ":" + strconv.FormatInt(uid, 10),
		})
	}
	s.emitNotifications(ctx, out...)
}

// notifyPageUpdate emits a `page_updated` notification to everyone following the
// page (directly) or its space — except the editor, and only to users who still
// have access. CollapseUnread keeps it to one unread "this page changed" per
// follower until they look, so a flurry of edits doesn't pile up.
func (s *Server) notifyPageUpdate(ctx context.Context, editor *auth.User, pageID, spaceID int64, title string) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT DISTINCT sub.user_id
		  FROM subscriptions sub
		  JOIN space_access sa ON sa.user_id = sub.user_id AND sa.space_id = $2
		 WHERE sub.user_id <> $3
		   AND ( (sub.subject_kind = 'page'  AND sub.subject_id = $1)
		      OR (sub.subject_kind = 'space' AND sub.subject_id = $2) )`,
		pageID, spaceID, editor.ID)
	if err != nil {
		slog.Error("notify page update: resolve followers", "page_id", pageID, "err", err)
		return
	}
	defer rows.Close()
	var recipients []int64
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			slog.Error("notify page update: scan follower", "err", err)
			return
		}
		recipients = append(recipients, uid)
	}
	if len(recipients) == 0 {
		return
	}
	editorID := editor.ID
	out := make([]notificationInput, 0, len(recipients))
	for _, uid := range recipients {
		out = append(out, notificationInput{
			UserID:         uid,
			Type:           notifPageUpdated,
			ActorID:        &editorID,
			SubjectKind:    "page",
			SubjectID:      pageID,
			SpaceID:        &spaceID,
			Data:           map[string]any{"page_title": title, "actor_username": editor.Username},
			CollapseUnread: true,
		})
	}
	s.emitNotifications(ctx, out...)
}

// notifySpaceAdded tells a user they were added to a space. Idempotent per
// (space, user). The recipient just gained access, so no extra gating needed.
func (s *Server) notifySpaceAdded(ctx context.Context, actor *auth.User, addedUserID, spaceID int64) {
	if actor == nil || addedUserID == actor.ID {
		return
	}
	var name string
	if err := s.DB.QueryRowContext(ctx, `SELECT name FROM spaces WHERE id = $1`, spaceID).Scan(&name); err != nil {
		slog.Error("notify space_added: lookup space", "space_id", spaceID, "err", err)
		return
	}
	actorID := actor.ID
	s.emitNotifications(ctx, notificationInput{
		UserID:      addedUserID,
		Type:        notifSpaceAdded,
		ActorID:     &actorID,
		SubjectKind: "space",
		SubjectID:   spaceID,
		SpaceID:     &spaceID,
		Data:        map[string]any{"space_name": name, "actor_username": actor.Username},
		DedupKey:    "space_added:space:" + strconv.FormatInt(spaceID, 10) + ":" + strconv.FormatInt(addedUserID, 10),
	})
}

// notifyCommentReply tells the author of a root comment that someone replied.
// Skips self-replies; re-gates the recipient through space_access (they authored
// a comment there, but access can be revoked). One notification per reply.
func (s *Server) notifyCommentReply(ctx context.Context, replier *auth.User, pageID, parentAuthorID int64) {
	if parentAuthorID == replier.ID {
		return
	}
	var (
		title   string
		spaceID int64
	)
	if err := s.DB.QueryRowContext(ctx,
		`SELECT title, space_id FROM pages WHERE id = $1`, pageID).Scan(&title, &spaceID); err != nil {
		slog.Error("notify comment_reply: lookup page", "page_id", pageID, "err", err)
		return
	}
	var x int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT 1 FROM space_access WHERE space_id = $1 AND user_id = $2`, spaceID, parentAuthorID).Scan(&x); err != nil {
		return // no access (or lookup error) → don't notify
	}
	replierID := replier.ID
	s.emitNotifications(ctx, notificationInput{
		UserID:      parentAuthorID,
		Type:        notifCommentReply,
		ActorID:     &replierID,
		SubjectKind: "page",
		SubjectID:   pageID,
		SpaceID:     &spaceID,
		Data:        map[string]any{"page_title": title, "actor_username": replier.Username},
	})
}

// notificationDTO is the wire shape for the inbox. Data is the denormalized
// render payload; the frontend builds the deep-link from subject_kind/space_id/
// subject_id and renders copy from type + data.
type notificationDTO struct {
	ID            int64          `json:"id"`
	Type          string         `json:"type"`
	ActorUsername *string        `json:"actor_username"`
	SubjectKind   string         `json:"subject_kind"`
	SubjectID     int64          `json:"subject_id"`
	SpaceID       *int64         `json:"space_id"`
	Data          map[string]any `json:"data"`
	Read          bool           `json:"read"`
	CreatedAt     string         `json:"created_at"`
}

// ListNotifications returns the caller's notifications, newest first.
func (s *Server) ListNotifications(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	limit := clampLimit(r.URL.Query().Get("limit"), 30, 100)
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT n.id, n.type, au.username, n.subject_kind, n.subject_id, n.space_id, n.data,
		       CASE WHEN n.read_at IS NULL THEN 0 ELSE 1 END AS read, n.created_at
		  FROM notifications n
		  LEFT JOIN users au ON au.id = n.actor_id
		 WHERE n.user_id = $1
		 ORDER BY n.id DESC
		 LIMIT $2`, u.ID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list notifications failed")
		return
	}
	defer rows.Close()

	items := []notificationDTO{}
	for rows.Next() {
		var (
			it       notificationDTO
			username sql.NullString
			spaceID  sql.NullInt64
			rawData  []byte
			read     int
		)
		if err := rows.Scan(&it.ID, &it.Type, &username, &it.SubjectKind, &it.SubjectID, &spaceID, &rawData, &read, &it.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan notification row failed")
			return
		}
		it.ActorUsername = nullableString(username)
		if spaceID.Valid {
			it.SpaceID = &spaceID.Int64
		}
		it.Read = read == 1
		it.Data = map[string]any{}
		if len(rawData) > 0 {
			_ = json.Unmarshal(rawData, &it.Data)
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate notifications failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notifications": items})
}

// UnreadNotificationCount returns the caller's unread count for the bell badge.
func (s *Server) UnreadNotificationCount(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	var n int
	if err := s.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL`, u.ID).Scan(&n); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "count notifications failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": n})
}

// MarkNotificationRead marks one notification read. Idempotent and ownership-
// scoped: a non-owned or already-read id simply affects no rows.
func (s *Server) MarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	if _, err := s.DB.ExecContext(r.Context(),
		`UPDATE notifications SET read_at = tela_now() WHERE id = $1 AND user_id = $2 AND read_at IS NULL`,
		id, u.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "mark notification read failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// MarkAllNotificationsRead marks every unread notification of the caller read.
func (s *Server) MarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	if _, err := s.DB.ExecContext(r.Context(),
		`UPDATE notifications SET read_at = tela_now() WHERE user_id = $1 AND read_at IS NULL`, u.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "mark all notifications read failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
