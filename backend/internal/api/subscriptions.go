package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

// --- Notification preferences ---

// notificationEventTypes / notificationChannels define the matrix the prefs UI
// renders. Adding an event type or channel here exposes it everywhere.
var notificationEventTypes = []string{notifMention, notifPageUpdated}
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
