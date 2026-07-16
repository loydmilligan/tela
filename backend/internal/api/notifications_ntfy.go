package api

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// ntfy delivery for notifications. The third channel beside in-app
// (notifications.go, the always-on inbox) and email (notifications_email.go).
// ntfy (https://ntfy.sh) is a dead-simple push service: POST to a topic URL and
// every device subscribed to that topic gets a push. The killer feature here is
// the Click header — the push deep-links straight to the page.
//
// Gated INDEPENDENTLY of in-app/email via notification_prefs(channel='ntfy') —
// opt-out, like the other channels. Per-user delivery target is users.ntfy_topic
// (empty = channel off for that user, same shape as "no email on file"). Built
// and sent like email: recipient/content resolved synchronously (ctx live), the
// HTTP POSTs fired detached so relay latency never slows the request. An unset
// TELA_NTFY_URL makes the whole channel inert (mirrors the SMTP-unset LogMailer).
// See docs/notifications.md.

// ntfyConfig is the ntfy channel's delivery config, resolved from env once at
// boot. URL == "" → the channel is inert (dispatch is a no-op). Tests overwrite
// s.ntfy with a config pointing Client at an httptest.Server.
type ntfyConfig struct {
	// URL is the ntfy server base (e.g. https://ntfy.sh); the user's topic is
	// appended as /{topic}. Trailing slash trimmed. Empty = channel inert.
	URL string
	// Token is an optional access token for protected topics (sent as
	// Authorization: Bearer). "" = no auth header.
	Token string
	// TopicPrefix is prepended to every user's stored topic before publishing
	// (TELA_NTFY_TOPIC_PREFIX). It keeps all pushes inside a token's topic scope
	// (e.g. an ntfy access grant of rw to `tela*`): set it to `tela-` and a user
	// who stores `alice` gets published to `tela-alice`. "" = publish topics
	// verbatim. Idempotent — a topic already carrying the prefix isn't doubled.
	TopicPrefix string
	Client      *http.Client
}

// ntfyConfigFromEnv resolves the channel config from TELA_NTFY_URL /
// TELA_NTFY_TOKEN. Never returns a nil Client, so a test that sets only URL still
// has a working sender.
func ntfyConfigFromEnv() ntfyConfig {
	return ntfyConfig{
		URL:         strings.TrimRight(strings.TrimSpace(os.Getenv("TELA_NTFY_URL")), "/"),
		Token:       strings.TrimSpace(os.Getenv("TELA_NTFY_TOKEN")),
		TopicPrefix: strings.TrimSpace(os.Getenv("TELA_NTFY_TOPIC_PREFIX")),
		Client:      &http.Client{Timeout: 10 * time.Second},
	}
}

func (c ntfyConfig) enabled() bool { return c.URL != "" }

// publishTopic is the actual topic a stored user topic is published to: the
// user's topic with TopicPrefix prepended (once — a topic already carrying the
// prefix is left as-is, so a habitual `tela-alice` never becomes `tela-tela-alice`).
// This is what the user must subscribe to in their ntfy app. "" stays "".
func (c ntfyConfig) publishTopic(userTopic string) string {
	if userTopic == "" || c.TopicPrefix == "" || strings.HasPrefix(userTopic, c.TopicPrefix) {
		return userTopic
	}
	return c.TopicPrefix + userTopic
}

// ntfyMsg is one push: a topic + the ntfy headers/body.
type ntfyMsg struct {
	Title string // ntfy "Title" header — the event summary
	Body  string // request body — the detail line (snippet / diff stat)
	Tags  string // ntfy "Tags" header — comma-separated emoji shortcodes
	Click string // ntfy "Click" header — deep link opened on tap
}

// send POSTs one push to URL/{topic}. Returns an error on transport failure or a
// non-2xx status so the caller can log it (best-effort; a notification never
// fails the triggering action).
func (c ntfyConfig) send(ctx context.Context, topic string, m ntfyMsg) error {
	endpoint := c.URL + "/" + url.PathEscape(c.publishTopic(topic))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(m.Body))
	if err != nil {
		return err
	}
	if m.Title != "" {
		req.Header.Set("Title", m.Title)
	}
	if m.Tags != "" {
		req.Header.Set("Tags", m.Tags)
	}
	if m.Click != "" {
		req.Header.Set("Click", m.Click)
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	client := c.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		return errors.New("ntfy: unexpected status " + strconv.Itoa(resp.StatusCode))
	}
	return nil
}

// ntfyEnabled reports whether the user wants ntfy pushes of this event type.
// Opt-out: absence of a row (or a lookup error) means enabled — mirrors
// emailEnabled/inAppEnabled, just the ntfy channel.
func (s *Server) ntfyEnabled(ctx context.Context, userID int64, eventType string) bool {
	var enabled int
	err := s.DB.QueryRowContext(ctx,
		`SELECT enabled FROM notification_prefs WHERE user_id = $1 AND event_type = $2 AND channel = $3`,
		userID, eventType, channelNtfy).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return true
	}
	if err != nil {
		slog.Error("notification ntfy pref lookup", "event_type", eventType, "user_id", userID, "err", err)
		return true
	}
	return enabled == 1
}

// userNtfyTopic returns the user's ntfy topic, or "" (channel off / skip send).
func (s *Server) userNtfyTopic(ctx context.Context, userID int64) string {
	var topic string
	if err := s.DB.QueryRowContext(ctx,
		`SELECT ntfy_topic FROM users WHERE id = $1`, userID).Scan(&topic); err != nil {
		return ""
	}
	return strings.TrimSpace(topic)
}

// claimPageUpdatedNtfy is the ntfy twin of claimPageUpdatedEmail: it atomically
// reserves a page_updated push for (user, page), true only when none was sent
// inside the window. It reuses the notification_email_throttle table but under a
// DISTINCT event_type key ('page_updated_ntfy') so the email and ntfy throttles
// never collide — each channel throttles independently. Same window as email.
func (s *Server) claimPageUpdatedNtfy(ctx context.Context, userID, pageID int64) bool {
	var x int64
	err := s.DB.QueryRowContext(ctx, `
		INSERT INTO notification_email_throttle (user_id, event_type, subject_id)
		VALUES ($1, 'page_updated_ntfy', $2)
		ON CONFLICT (user_id, event_type, subject_id) DO UPDATE
		   SET sent_at = tela_now()
		 WHERE notification_email_throttle.sent_at::timestamp
		       < (tela_now())::timestamp - interval '`+pageUpdatedEmailWindow+`'
		RETURNING user_id`, userID, pageID).Scan(&x)
	if errors.Is(err, sql.ErrNoRows) {
		return false // inside the window → throttled
	}
	if err != nil {
		slog.Error("page_updated ntfy throttle", "user_id", userID, "page_id", pageID, "err", err)
		return false
	}
	return true
}

// dispatchNtfy sends one ntfy push per input to each enabled recipient with a
// topic set. Resolution (channel enabled, prefs, throttle, topic, actor, links)
// is synchronous with the live ctx; the HTTP POSTs run detached. No-op when the
// channel is inert (TELA_NTFY_URL unset).
func (s *Server) dispatchNtfy(ctx context.Context, ins []notificationInput) {
	if !s.ntfy.enabled() {
		return
	}
	actorNames := map[int64]string{} // cache: one actor often fans out to many recipients
	type delivery struct {
		topic string
		msg   ntfyMsg
	}
	var out []delivery

	for _, in := range ins {
		if !s.ntfyEnabled(ctx, in.UserID, in.Type) {
			continue
		}
		if in.Type == notifPageUpdated && !s.claimPageUpdatedNtfy(ctx, in.UserID, in.SubjectID) {
			continue
		}
		topic := s.userNtfyTopic(ctx, in.UserID)
		if topic == "" {
			continue // no topic → channel off for this user
		}
		actor := s.actorName(ctx, in.ActorID, in.Data, actorNames)
		msg, ok := s.buildNtfyMessage(ctx, in, actor)
		if !ok {
			continue
		}
		out = append(out, delivery{topic: topic, msg: msg})
	}
	if len(out) == 0 {
		return
	}
	cfg := s.ntfy
	go func() {
		for _, d := range out {
			if err := cfg.send(context.Background(), d.topic, d.msg); err != nil {
				slog.Error("notification ntfy send", "topic", d.topic, "err", err)
			}
		}
	}()
}

// buildNtfyMessage turns one input into a ready-to-send push. Returns false for a
// type we don't push (same set the email channel templates), so an unknown/quiet
// event type simply doesn't fan out to ntfy.
func (s *Server) buildNtfyMessage(ctx context.Context, in notificationInput, actor string) (ntfyMsg, bool) {
	title, _ := in.Data["page_title"].(string)
	spaceName, _ := in.Data["space_name"].(string)
	snippet, _ := in.Data["snippet"].(string)

	m := ntfyMsg{}
	switch in.Type {
	case notifMention:
		m.Title = actor + " mentioned you in " + title
		m.Body = snippet
		m.Tags = "speech_balloon"
		m.Click = s.pageLink(ctx, in)
	case notifCommentReply:
		m.Title = actor + " replied to your comment"
		m.Body = snippet
		m.Tags = "speech_balloon"
		m.Click = s.pageLink(ctx, in)
	case notifPageComment:
		m.Title = actor + " commented on " + title
		m.Body = snippet
		m.Tags = "speech_balloon"
		m.Click = s.pageLink(ctx, in)
	case notifSpaceAdded:
		origin := s.shareOrigin(ctx, in.SubjectID)
		m.Title = actor + " added you to " + spaceName
		m.Tags = "busts_in_silhouette"
		m.Click = origin + "/spaces/" + strconv.FormatInt(in.SubjectID, 10)
	case notifPageUpdated:
		m.Title = actor + " updated " + title
		m.Body = in.ChangeStat
		m.Tags = "pencil2"
		m.Click = s.pageLink(ctx, in)
	case notifPageCreated:
		m.Title = actor + " created " + title
		m.Tags = "new"
		m.Click = s.pageLink(ctx, in)
	case notifUserRegistered:
		name, _ := in.Data["new_display_name"].(string)
		email, _ := in.Data["new_email"].(string)
		handle, _ := in.Data["new_username"].(string)
		m.Title = "New tela signup: " + name
		m.Body = email
		m.Tags = "wave"
		m.Click = canonicalBaseURL() + "/" + handle
	default:
		return ntfyMsg{}, false
	}
	return m, true
}
