package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/zcag/tela/backend/internal/auth"
)

// Follow/subscribe + notification preferences. Following a page or space opts
// you into its `page_updated` notifications; prefs let you turn event types off
// per channel. See docs/notifications.md.

func (s *Server) setSubscription(ctx context.Context, userID int64, kind string, subjectID int64) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO subscriptions (user_id, subject_kind, subject_id) VALUES ($1, $2, $3)
		 ON CONFLICT DO NOTHING`, userID, kind, subjectID)
	return err
}

func (s *Server) clearSubscription(ctx context.Context, userID int64, kind string, subjectID int64) error {
	_, err := s.DB.ExecContext(ctx,
		`DELETE FROM subscriptions WHERE user_id = $1 AND subject_kind = $2 AND subject_id = $3`,
		userID, kind, subjectID)
	return err
}

func (s *Server) isSubscribed(ctx context.Context, userID int64, kind string, subjectID int64) (bool, error) {
	var x int
	err := s.DB.QueryRowContext(ctx,
		`SELECT 1 FROM subscriptions WHERE user_id = $1 AND subject_kind = $2 AND subject_id = $3`,
		userID, kind, subjectID).Scan(&x)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// --- Page subscriptions (viewer+ on the page's space) ---

// pageSubGate resolves the page's space and confirms the caller can see it,
// returning the space id. Shared by the three page-subscription handlers.
func (s *Server) pageSubGate(w http.ResponseWriter, r *http.Request, pageID int64) (*auth.User, int64, bool) {
	u, ok := requireUser(w, r)
	if !ok {
		return nil, 0, false
	}
	spaceID, err := pageSpaceID(r, s.DB, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "page not found")
		return nil, 0, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup page failed")
		return nil, 0, false
	}
	k, _ := auth.APIKeyFromContext(r.Context())
	if _, ae := s.membershipCore(r.Context(), u, k, spaceID); ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return nil, 0, false
	}
	return u, spaceID, true
}

func (s *Server) GetPageSubscription(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, _, ok := s.pageSubGate(w, r, id)
	if !ok {
		return
	}
	sub, err := s.isSubscribed(r.Context(), u.ID, "page", id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup subscription failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"subscribed": sub})
}

func (s *Server) SubscribePage(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, _, ok := s.pageSubGate(w, r, id)
	if !ok {
		return
	}
	if err := s.setSubscription(r.Context(), u.ID, "page", id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "subscribe failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"subscribed": true})
}

func (s *Server) UnsubscribePage(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	if err := s.clearSubscription(r.Context(), u.ID, "page", id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "unsubscribe failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Space subscriptions (viewer+ on the space) ---

func (s *Server) GetSpaceSubscription(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	k, _ := auth.APIKeyFromContext(r.Context())
	if _, ae := s.membershipCore(r.Context(), u, k, id); ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	sub, err := s.isSubscribed(r.Context(), u.ID, "space", id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup subscription failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"subscribed": sub})
}

func (s *Server) SubscribeSpace(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	k, _ := auth.APIKeyFromContext(r.Context())
	if _, ae := s.membershipCore(r.Context(), u, k, id); ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	if err := s.setSubscription(r.Context(), u.ID, "space", id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "subscribe failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"subscribed": true})
}

func (s *Server) UnsubscribeSpace(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	if err := s.clearSubscription(r.Context(), u.ID, "space", id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "unsubscribe failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Autowatch (auto-follow on create/edit/comment) ---

// autowatchEnabled reports whether the user wants to auto-follow pages they act
// on. Default on (missing/error → true), mirroring the notification opt-out
// posture. Gates every auto-subscribe; never gates a manual follow.
func (s *Server) autowatchEnabled(ctx context.Context, userID int64) bool {
	var v int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT autowatch FROM users WHERE id = $1`, userID).Scan(&v); err != nil {
		return true
	}
	return v == 1
}

// autoFollow subscribes the user to a page when autowatch is on. Best-effort.
func (s *Server) autoFollow(ctx context.Context, userID, pageID int64) {
	if !s.autowatchEnabled(ctx, userID) {
		return
	}
	if err := s.setSubscription(ctx, userID, "page", pageID); err != nil {
		slog.Error("autowatch auto-subscribe failed", "page_id", pageID, "user_id", userID, "err", err)
	}
}

// GetAutowatch returns the caller's autowatch preference.
func (s *Server) GetAutowatch(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"autowatch": s.autowatchEnabled(r.Context(), u.ID)})
}

// SetAutowatch updates the caller's autowatch preference.
func (s *Server) SetAutowatch(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req struct {
		Autowatch bool `json:"autowatch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	v := 0
	if req.Autowatch {
		v = 1
	}
	if _, err := s.DB.ExecContext(r.Context(),
		`UPDATE users SET autowatch = $1 WHERE id = $2`, v, u.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "update autowatch failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"autowatch": req.Autowatch})
}

// --- Following list ("what am I watching") ---

type subscriptionDTO struct {
	Kind      string `json:"kind"` // "page" | "space"
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	SpaceID   *int64 `json:"space_id,omitempty"` // for pages, to build the link
	CreatedAt string `json:"created_at"`
}

// ListSubscriptions returns everything the caller follows — pages and spaces —
// resolved to current titles and access-gated (a sub to a page/space they can no
// longer see is omitted). Powers the Settings → Following management list.
func (s *Server) ListSubscriptions(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT 'page' AS kind, p.id, p.title, p.space_id, sub.created_at
		  FROM subscriptions sub
		  JOIN pages p ON p.id = sub.subject_id AND p.deleted_at IS NULL
		  JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = $1) sm ON sm.space_id = p.space_id
		 WHERE sub.user_id = $1 AND sub.subject_kind = 'page'
		UNION ALL
		SELECT 'space' AS kind, sp.id, sp.name, NULL::bigint, sub.created_at
		  FROM subscriptions sub
		  JOIN spaces sp ON sp.id = sub.subject_id
		  JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = $1) sm ON sm.space_id = sp.id
		 WHERE sub.user_id = $1 AND sub.subject_kind = 'space'
		 ORDER BY created_at DESC`, u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list subscriptions failed")
		return
	}
	defer rows.Close()
	items := []subscriptionDTO{}
	for rows.Next() {
		var it subscriptionDTO
		var spaceID sql.NullInt64
		if err := rows.Scan(&it.Kind, &it.ID, &it.Title, &spaceID, &it.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan subscription failed")
			return
		}
		if spaceID.Valid {
			it.SpaceID = &spaceID.Int64
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate subscriptions failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"subscriptions": items})
}

// --- Notification preferences ---

// notificationEventTypes / notificationChannels define the matrix the prefs UI
// renders. Adding an event type or channel here exposes it everywhere.
var notificationEventTypes = []string{notifMention, notifPageUpdated, notifPageCreated, notifSpaceAdded, notifCommentReply, notifAtlasRun}
var notificationChannels = []string{channelInApp, channelEmail}

type notificationPrefDTO struct {
	EventType string `json:"event_type"`
	Channel   string `json:"channel"`
	Enabled   bool   `json:"enabled"`
}

// GetNotificationPrefs returns the full (event_type × channel) matrix, defaulting
// to enabled and overlaying the caller's stored opt-outs.
func (s *Server) GetNotificationPrefs(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT event_type, channel, enabled FROM notification_prefs WHERE user_id = $1`, u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "load notification prefs failed")
		return
	}
	defer rows.Close()
	stored := map[string]bool{}
	for rows.Next() {
		var (
			et, ch  string
			enabled int
		)
		if err := rows.Scan(&et, &ch, &enabled); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan notification pref failed")
			return
		}
		stored[et+"|"+ch] = enabled == 1
	}
	prefs := []notificationPrefDTO{}
	for _, et := range notificationEventTypes {
		for _, ch := range notificationChannels {
			enabled := true
			if v, ok := stored[et+"|"+ch]; ok {
				enabled = v
			}
			prefs = append(prefs, notificationPrefDTO{EventType: et, Channel: ch, Enabled: enabled})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"prefs": prefs})
}

type notificationPrefUpdate struct {
	EventType string `json:"event_type"`
	Channel   string `json:"channel"`
	Enabled   bool   `json:"enabled"`
}

func validNotificationEvent(et string) bool {
	for _, v := range notificationEventTypes {
		if v == et {
			return true
		}
	}
	return false
}

func validNotificationChannel(ch string) bool {
	for _, v := range notificationChannels {
		if v == ch {
			return true
		}
	}
	return false
}

// UpdateNotificationPref upserts one (event_type, channel) preference for the
// caller. Only writes a row when toggling off (or back on) — the absence of a
// row is the enabled default.
func (s *Server) UpdateNotificationPref(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req notificationPrefUpdate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	if !validNotificationEvent(req.EventType) {
		writeError(w, http.StatusBadRequest, "invalid_event_type", "unknown event_type")
		return
	}
	if !validNotificationChannel(req.Channel) {
		writeError(w, http.StatusBadRequest, "invalid_channel", "unknown channel")
		return
	}
	enabled := 0
	if req.Enabled {
		enabled = 1
	}
	if _, err := s.DB.ExecContext(r.Context(), `
		INSERT INTO notification_prefs (user_id, event_type, channel, enabled)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, event_type, channel) DO UPDATE SET enabled = EXCLUDED.enabled`,
		u.ID, req.EventType, req.Channel, enabled); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "update notification pref failed")
		return
	}
	writeJSON(w, http.StatusOK, notificationPrefDTO{EventType: req.EventType, Channel: req.Channel, Enabled: req.Enabled})
}
