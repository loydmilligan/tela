package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zcag/tela/backend/internal/auth"
	"github.com/zcag/tela/backend/internal/llm"
	"github.com/zcag/tela/backend/internal/mailer"
)

// Weekly digest assembly. Scope is every space the caller can see — the same
// set ListSpaces uses (space_access joined on user_id) — covering owned, shared,
// and org spaces. Built entirely from signals tela already records (pages,
// comments, page_revisions), so it's assembly, not new plumbing. The API layer
// fills mailer.DigestData; internal/mailer/digest.go renders it.

const digestSpaceScope = `space_id IN (SELECT space_id FROM space_access WHERE user_id = $1)`

// DigestPreview handles GET /api/me/digest/preview — renders the caller's
// current weekly digest as HTML (for eyeballing the content + design before the
// scheduled send exists). Auth-gated; the user only ever sees their own spaces.
func (s *Server) DigestPreview(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	since := time.Now().UTC().AddDate(0, 0, -7)
	data, err := s.buildDigest(r.Context(), u, since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "build digest failed")
		return
	}
	msg := mailer.Digest(u.Email, "Your week", data)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(msg.HTML))
}

func (s *Server) buildDigest(ctx context.Context, u *auth.User, since time.Time) (mailer.DigestData, error) {
	now := time.Now().UTC()
	sinceStr := since.Format(tsLayout)
	d := mailer.DigestData{
		Greeting:  firstName(s.digestGreeting(ctx, u.ID), u.Username),
		DateRange: dateRange(since, now),
		AppURL:    canonicalBaseURL(),
	}
	d.PrefsURL = strings.TrimRight(d.AppURL, "/") + "/settings?tab=notifications"
	d.UnsubURL = s.digestUnsubURL(u.ID)

	// Spaces the user can see.
	_ = s.DB.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT space_id) FROM space_access WHERE user_id = $1`, u.ID).
		Scan(&d.SpaceCount)

	// Stats. "New" = created this week; "Updated" = touched this week but older;
	// "Comments" = comments landed this week; "NewMembers" = access granted this
	// week (best-effort — 0 if the column/rows aren't there).
	scope := func(extra string) string {
		return `SELECT COUNT(*) FROM pages WHERE ` + digestSpaceScope +
			` AND deleted_at IS NULL AND ` + extra
	}
	_ = s.DB.QueryRowContext(ctx, scope(`created_at >= $2`), u.ID, sinceStr).Scan(&d.Stats.New)
	_ = s.DB.QueryRowContext(ctx, scope(`updated_at >= $2 AND created_at < $2`), u.ID, sinceStr).Scan(&d.Stats.Updated)
	_ = s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM comments c JOIN pages p ON p.id = c.page_id
		  WHERE p.`+digestSpaceScope+` AND c.deleted_at IS NULL AND c.created_at >= $2`,
		u.ID, sinceStr).Scan(&d.Stats.Comments)
	_ = s.DB.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT user_id) FROM space_access
		  WHERE `+digestSpaceScope+` AND created_at >= $2`, u.ID, sinceStr).Scan(&d.Stats.NewMembers)

	// New & updated pages (most-recent first).
	const updateLimit = 6
	updates, total, err := s.digestUpdates(ctx, u.ID, sinceStr, now, updateLimit)
	if err != nil {
		return d, err
	}
	d.Updates = updates
	if total > len(updates) {
		d.MoreCount = total - len(updates)
	}

	// Needs your eyes — contradictions first (a page disputed by another is the
	// most urgent), then Atlas-stale pages, then unresolved comment threads.
	// Each sub-query is best-effort; the section is capped to stay scannable.
	d.Attention = s.digestConflicts(ctx, u.ID)
	d.Attention = append(d.Attention, s.digestStale(ctx, u.ID)...)
	open, err := s.digestAttention(ctx, u.ID)
	if err != nil {
		return d, err
	}
	d.Attention = append(d.Attention, open...)
	if len(d.Attention) > 4 {
		d.Attention = d.Attention[:4]
	}

	d.Gist = s.digestGist(ctx, d)
	return d, nil
}

// --- preferences, unsubscribe, admin preview -------------------------------

// GetDigestPref handles GET /api/me/digest — the caller's current frequency.
func (s *Server) GetDigestPref(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	var freq string
	_ = s.DB.QueryRowContext(r.Context(),
		`SELECT digest_frequency FROM users WHERE id = $1`, u.ID).Scan(&freq)
	if freq == "" {
		freq = "off"
	}
	writeJSON(w, http.StatusOK, map[string]any{"frequency": freq})
}

// SetDigestPref handles PATCH /api/me/digest {frequency: "off"|"weekly"}.
func (s *Server) SetDigestPref(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req struct {
		Frequency string `json:"frequency"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not parse request body")
		return
	}
	if req.Frequency != "off" && req.Frequency != "weekly" {
		writeError(w, http.StatusBadRequest, "bad_request", "frequency must be 'off' or 'weekly'")
		return
	}
	if _, err := s.DB.ExecContext(r.Context(),
		`UPDATE users SET digest_frequency = $1 WHERE id = $2`, req.Frequency, u.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "update failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"frequency": req.Frequency})
}

// digestUnsubToken is an HMAC over the user id — a stable, unguessable
// one-click-unsubscribe credential that needs no session (the link lands in an
// email). Keyed by the share secret, like the other self-authenticating links.
func (s *Server) digestUnsubToken(userID int64) string {
	mac := hmac.New(sha256.New, s.shareSecret)
	fmt.Fprintf(mac, "digest-unsub:%d", userID)
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Server) digestUnsubURL(userID int64) string {
	base := strings.TrimRight(canonicalBaseURL(), "/")
	return fmt.Sprintf("%s/api/digest/unsubscribe?u=%d&t=%s", base, userID, s.digestUnsubToken(userID))
}

// DigestUnsubscribe handles GET /api/digest/unsubscribe?u=&t= — PUBLIC (on
// IsPublicPath); it self-authenticates via the HMAC token, sets the user's
// frequency to off, and shows a small confirmation page.
func (s *Server) DigestUnsubscribe(w http.ResponseWriter, r *http.Request) {
	uid, _ := strconv.ParseInt(r.URL.Query().Get("u"), 10, 64)
	tok := r.URL.Query().Get("t")
	if uid <= 0 || !hmac.Equal([]byte(tok), []byte(s.digestUnsubToken(uid))) {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid unsubscribe link")
		return
	}
	_, _ = s.DB.ExecContext(r.Context(),
		`UPDATE users SET digest_frequency = 'off' WHERE id = $1`, uid)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><body style="font-family:-apple-system,Segoe UI,Roboto,sans-serif;background:#f3f3f7;padding:60px 20px;text-align:center;color:#15161c;"><div style="max-width:420px;margin:0 auto;background:#fff;border:1px solid #e6e6ef;border-radius:12px;padding:32px;"><div style="height:4px;background:#4f46e5;border-radius:2px;width:40px;margin:0 auto 20px;"></div><h1 style="font-size:20px;margin:0 0 8px;">Unsubscribed</h1><p style="color:#5b6270;font-size:14px;line-height:1.5;margin:0;">You won't get the weekly digest anymore. You can turn it back on any time in notification settings.</p></div></body>`))
}

// AdminDigestPreview handles GET /api/admin/digest/preview?user=<username|id> —
// instance-admin only. Renders the digest that WOULD be sent to any user, for
// eyeballing real output before enabling sends. Scoping still uses that user's
// own accessible spaces, so it discloses nothing they couldn't already see.
func (s *Server) AdminDigestPreview(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	if !u.IsInstanceAdmin {
		writeError(w, http.StatusForbidden, "forbidden", "instance admin only")
		return
	}
	target, err := s.resolveUserByIdent(r.Context(), r.URL.Query().Get("user"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such user")
		return
	}
	since := time.Now().UTC().AddDate(0, 0, -7)
	data, err := s.buildDigest(r.Context(), target, since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "build digest failed")
		return
	}
	msg := mailer.Digest(target.Email, digestSubject(data), data)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(msg.HTML))
}

// RenderDigestForUser builds + renders the current weekly digest HTML for a user
// (username or id). Exported so the `tela digest preview` CLI can call it via a
// Server built with api.New — the same path the HTTP preview uses.
func (s *Server) RenderDigestForUser(ctx context.Context, ident string) (string, error) {
	target, err := s.resolveUserByIdent(ctx, ident)
	if err != nil {
		return "", err
	}
	since := time.Now().UTC().AddDate(0, 0, -7)
	data, err := s.buildDigest(ctx, target, since)
	if err != nil {
		return "", err
	}
	return mailer.Digest(target.Email, digestSubject(data), data).HTML, nil
}

func (s *Server) resolveUserByIdent(ctx context.Context, ident string) (*auth.User, error) {
	ident = strings.TrimSpace(ident)
	if ident == "" {
		return nil, fmt.Errorf("empty user")
	}
	var u auth.User
	var email string
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, username, COALESCE(email, '') FROM users
		  WHERE username = $1 OR CAST(id AS TEXT) = $1 LIMIT 1`, ident).
		Scan(&u.ID, &u.Username, &email)
	if err != nil {
		return nil, err
	}
	u.Email = email
	return &u, nil
}

// --- scheduled send --------------------------------------------------------

func digestSubject(d mailer.DigestData) string {
	return "Your week in tela · " + d.DateRange
}

// digestEmpty is true when there's nothing worth mailing: no pages to list AND
// nothing in "needs your eyes". A comments-only week (no listed updates) doesn't
// send; a week with only a conflict/stale alert still does — that's worth
// knowing even with zero page changes.
func digestEmpty(d mailer.DigestData) bool {
	return len(d.Updates) == 0 && len(d.Attention) == 0
}

// sendDueDigests sends the weekly digest to every opted-in user whose last send
// was ≥7 days ago (or never). Per-user cadence is gated on digest_last_sent_at,
// so this is safe to run on any schedule and across restarts. Empty digests
// (no activity) are skipped. dryRun builds + counts without sending.
func (s *Server) SendDueDigests(ctx context.Context, dryRun bool) (sent int, err error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, username, COALESCE(email, ''), digest_last_sent_at
		  FROM users
		 WHERE digest_frequency = 'weekly' AND is_active = 1
		   AND email IS NOT NULL AND email <> '' AND email_verified_at IS NOT NULL`)
	if err != nil {
		return 0, err
	}
	type due struct {
		u        auth.User
		lastSent string
	}
	var users []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.u.ID, &d.u.Username, &d.u.Email, &d.lastSent); err != nil {
			rows.Close()
			return sent, err
		}
		users = append(users, d)
	}
	rows.Close()

	now := time.Now().UTC()
	for _, d := range users {
		since := now.AddDate(0, 0, -7)
		if d.lastSent != "" {
			if t, e := time.Parse(tsLayout, d.lastSent); e == nil {
				if now.Sub(t) < 7*24*time.Hour {
					continue // not due yet
				}
				since = t
			}
		}
		data, e := s.buildDigest(ctx, &d.u, since)
		if e != nil {
			slog.Error("digest: build failed", "user_id", d.u.ID, "err", e)
			continue
		}
		if digestEmpty(data) {
			continue // nothing to say — don't send a hollow email
		}
		if dryRun {
			sent++
			continue
		}
		msg := mailer.Digest(d.u.Email, digestSubject(data), data)
		if e := s.Mailer.Send(ctx, msg); e != nil {
			slog.Error("digest: send failed", "user_id", d.u.ID, "err", e)
			continue
		}
		_, _ = s.DB.ExecContext(ctx,
			`UPDATE users SET digest_last_sent_at = $1 WHERE id = $2`, now.Format(tsLayout), d.u.ID)
		sent++
	}
	return sent, nil
}

// digestLoop runs the weekly send on a daily tick (per-user cadence is gated in
// sendDueDigests). A short initial delay keeps boot from being a send storm;
// with the default 'off' pref, nothing sends until a user opts in.
func (s *Server) digestLoop(ctx context.Context) {
	timer := time.NewTimer(2 * time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if n, err := s.SendDueDigests(ctx, false); err != nil {
			slog.Error("digest: weekly send failed", "err", err)
		} else if n > 0 {
			slog.Info("digest: weekly send", "sent", n)
		}
		timer.Reset(24 * time.Hour)
	}
}

func (s *Server) digestGreeting(ctx context.Context, userID int64) string {
	var name string
	_ = s.DB.QueryRowContext(ctx,
		`SELECT COALESCE(NULLIF(display_name, ''), username) FROM users WHERE id = $1`,
		userID).Scan(&name)
	return name
}

func (s *Server) digestUpdates(ctx context.Context, userID int64, sinceStr string, now time.Time, limit int) ([]mailer.DigestUpdate, int, error) {
	var total int
	_ = s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pages WHERE `+digestSpaceScope+
			` AND deleted_at IS NULL AND updated_at >= $2`, userID, sinceStr).Scan(&total)

	rows, err := s.DB.QueryContext(ctx, `
		SELECT p.id, p.space_id, p.title, s.name,
		       p.updated_at, (p.created_at >= $2) AS is_new,
		       COALESCE(p.props->>'summary', ''),
		       COALESCE(NULLIF(u.display_name, ''), u.username, ''),
		       COALESCE(p.props->>'generator', '')
		  FROM pages p
		  JOIN spaces s ON s.id = p.space_id
		  LEFT JOIN LATERAL (
		    SELECT author_id FROM page_revisions r
		     WHERE r.page_id = p.id ORDER BY r.created_at DESC LIMIT 1
		  ) lr ON true
		  LEFT JOIN users u ON u.id = lr.author_id
		 WHERE p.`+digestSpaceScope+`
		   AND p.deleted_at IS NULL AND p.updated_at >= $2
		 ORDER BY p.updated_at DESC
		 LIMIT $3`, userID, sinceStr, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	base := strings.TrimRight(canonicalBaseURL(), "/")
	var out []mailer.DigestUpdate
	for rows.Next() {
		var id, spaceID int64
		var title, spaceName, updatedAt, summary, actor, generator string
		var isNew bool
		if err := rows.Scan(&id, &spaceID, &title, &spaceName, &updatedAt, &isNew, &summary, &actor, &generator); err != nil {
			return nil, 0, err
		}
		verb := "edited"
		if isNew {
			verb = "created"
		}
		who := strings.ToUpper(verb[:1]) + verb[1:]
		badge := ""
		if generator == "atlas" {
			who, badge = "auto-refreshed", "ATLAS" // Atlas wrote it, not a person
		} else if actor != "" {
			who = actor + " " + verb
		}
		out = append(out, mailer.DigestUpdate{
			Title:     title,
			SpaceName: spaceName,
			Actor:     who,
			When:      relTime(updatedAt, now),
			Summary:   digestTruncate(summary, 140),
			URL:       fmt.Sprintf("%s/spaces/%d/pages/%d", base, spaceID, id),
			Badge:     badge,
		})
	}
	return out, total, rows.Err()
}

func (s *Server) digestAttention(ctx context.Context, userID int64) ([]mailer.DigestAttention, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT c.body, p.id, p.space_id, p.title,
		       COALESCE(NULLIF(u.display_name, ''), u.username, '')
		  FROM comments c
		  JOIN pages p ON p.id = c.page_id
		  JOIN users u ON u.id = c.author_id
		 WHERE c.parent_id IS NULL AND c.resolved = 0 AND c.deleted_at IS NULL
		   AND p.deleted_at IS NULL AND p.`+digestSpaceScope+`
		 ORDER BY c.created_at ASC
		 LIMIT 3`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	base := strings.TrimRight(canonicalBaseURL(), "/")
	var out []mailer.DigestAttention
	for rows.Next() {
		var body, title, actor string
		var pageID, spaceID int64
		if err := rows.Scan(&body, &pageID, &spaceID, &title, &actor); err != nil {
			return nil, err
		}
		if actor == "" {
			actor = "Someone"
		}
		out = append(out, mailer.DigestAttention{
			Kind:   "OPEN Q",
			Tone:   "info",
			Title:  digestTruncate(oneLine(body), 90),
			Detail: fmt.Sprintf("Asked by %s on %s · no answer yet.", actor, title),
			URL:    fmt.Sprintf("%s/spaces/%d/pages/%d", base, spaceID, pageID),
		})
	}
	return out, rows.Err()
}

// digestConflicts surfaces pages that another page contradicts (page_agreement
// disputes) — tela's "discrepancies" signal. Best-effort: any error yields no
// rows and never breaks the digest.
func (s *Server) digestConflicts(ctx context.Context, userID int64) []mailer.DigestAttention {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT p.id, p.space_id, p.title, a.dispute, a.disputes
		  FROM page_agreement a
		  JOIN pages p ON p.id = a.page_id
		 WHERE a.dispute > 0 AND a.last_error = '' AND p.deleted_at IS NULL
		   AND p.`+digestSpaceScope+`
		 ORDER BY a.dispute DESC
		 LIMIT 2`, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	base := strings.TrimRight(canonicalBaseURL(), "/")
	var out []mailer.DigestAttention
	for rows.Next() {
		var pageID, spaceID int64
		var count int
		var title, disputesRaw string
		if err := rows.Scan(&pageID, &spaceID, &title, &count, &disputesRaw); err != nil {
			return out
		}
		// disputes JSON: [{page_id,title,reason}] — lead with the first one's
		// reason; note the total if there are more.
		var disputes []struct {
			Title  string `json:"title"`
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal([]byte(disputesRaw), &disputes)
		detail := plural(count) // "s" or ""
		detail = fmt.Sprintf("Contradicts %d page%s", count, detail)
		if len(disputes) > 0 && disputes[0].Reason != "" {
			detail += " — " + oneLine(disputes[0].Reason)
		} else if len(disputes) > 0 && disputes[0].Title != "" {
			detail += " (e.g. " + disputes[0].Title + ")"
		}
		out = append(out, mailer.DigestAttention{
			Kind:   "CONFLICT",
			Tone:   "warn",
			Title:  title,
			Detail: digestTruncate(detail, 120),
			URL:    fmt.Sprintf("%s/spaces/%d/pages/%d", base, spaceID, pageID),
		})
	}
	return out
}

// digestStale surfaces Atlas-generated pages whose upstream has moved past the
// last generated ref (a stale atlas_source). Best-effort: any error — e.g. an
// instance with no Atlas tables — yields no rows and never breaks the digest.
func (s *Server) digestStale(ctx context.Context, userID int64) []mailer.DigestAttention {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT p.id, p.space_id, p.title
		  FROM atlas_page_map m
		  JOIN atlas_sources src ON src.id = m.source_id
		  JOIN pages p ON p.id = m.page_id
		 WHERE src.stale_since <> '' AND p.deleted_at IS NULL
		   AND p.`+digestSpaceScope+`
		 ORDER BY src.stale_since ASC
		 LIMIT 2`, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	base := strings.TrimRight(canonicalBaseURL(), "/")
	var out []mailer.DigestAttention
	for rows.Next() {
		var pageID, spaceID int64
		var title string
		if err := rows.Scan(&pageID, &spaceID, &title); err != nil {
			return out
		}
		out = append(out, mailer.DigestAttention{
			Kind:   "STALE",
			Tone:   "warn",
			Title:  title,
			Detail: "Atlas docs are behind upstream — likely out of date.",
			URL:    fmt.Sprintf("%s/spaces/%d/pages/%d", base, spaceID, pageID),
		})
	}
	return out
}

const digestGistSystem = `You write the single-sentence summary at the top of a team wiki's weekly digest email. From the activity brief, say what actually happened this week — concrete and specific, naming the notable pages or topics. Plain language, no greeting, no "this week" filler, no markdown. One sentence, at most ~35 words.`

// digestGist produces the "gist" line — an LLM one-sentence summary when the LLM
// is configured, else the deterministic fallback below. Any LLM error falls back
// silently so the digest always has a gist.
func (s *Server) digestGist(ctx context.Context, d mailer.DigestData) string {
	fallback := digestGistFallback(d)
	if s.llm == nil || !s.llm.Enabled() {
		return fallback
	}
	if d.Stats.New == 0 && d.Stats.Updated == 0 && d.Stats.Comments == 0 && len(d.Attention) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Counts: %d new pages, %d updated, %d comments, %d new members.\n",
		d.Stats.New, d.Stats.Updated, d.Stats.Comments, d.Stats.NewMembers)
	if len(d.Updates) > 0 {
		b.WriteString("Recent pages:\n")
		for _, up := range d.Updates {
			line := "- " + up.Title + " (" + up.SpaceName + ", " + up.Actor + ")"
			if up.Summary != "" {
				line += ": " + up.Summary
			}
			b.WriteString(line + "\n")
		}
	}
	if len(d.Attention) > 0 {
		b.WriteString("Needs attention:\n")
		for _, a := range d.Attention {
			b.WriteString("- [" + a.Kind + "] " + a.Title + "\n")
		}
	}
	out, err := s.llm.Complete(llm.WithBackground(ctx), digestGistSystem, b.String())
	if err != nil {
		return fallback
	}
	if out = strings.TrimSpace(out); out != "" {
		return out
	}
	return fallback
}

// digestGistFallback is a deterministic one-liner used when the LLM is off. It
// adds the "still waiting on a reply" nudge the stat tiles don't convey.
func digestGistFallback(d mailer.DigestData) string {
	if d.Stats.New == 0 && d.Stats.Updated == 0 && d.Stats.Comments == 0 {
		return ""
	}
	var b strings.Builder
	parts := []string{}
	if d.Stats.New > 0 {
		parts = append(parts, digestPlural(d.Stats.New, "new page", "new pages"))
	}
	if d.Stats.Updated > 0 {
		parts = append(parts, digestPlural(d.Stats.Updated, "update", "updates"))
	}
	if len(parts) == 0 {
		parts = append(parts, digestPlural(d.Stats.Comments, "new comment", "new comments"))
	}
	b.WriteString(joinAnd(parts))
	b.WriteString(" landed across your spaces this week")
	if n := len(d.Attention); n > 0 {
		b.WriteString(", and ")
		b.WriteString(digestPlural(n, "comment thread is", "comment threads are"))
		b.WriteString(" still waiting on a reply")
	}
	b.WriteString(".")
	return b.String()
}

// --- small formatting helpers ---

func firstName(name, fallback string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = fallback
	}
	if i := strings.IndexByte(name, ' '); i > 0 {
		return name[:i]
	}
	return name
}

func dateRange(a, b time.Time) string {
	if a.Year() == b.Year() {
		return fmt.Sprintf("%s – %s, %d", a.Format("Jan 2"), b.Format("Jan 2"), b.Year())
	}
	return fmt.Sprintf("%s, %d – %s, %d", a.Format("Jan 2"), a.Year(), b.Format("Jan 2"), b.Year())
}

func relTime(ts string, now time.Time) string {
	t, err := time.Parse(tsLayout, ts)
	if err != nil {
		return ""
	}
	dur := now.Sub(t)
	switch {
	case dur < time.Minute:
		return "just now"
	case dur < time.Hour:
		return digestPlural(int(dur.Minutes()), "min ago", "min ago")
	case dur < 24*time.Hour:
		return digestPlural(int(dur.Hours()), "hour ago", "hours ago")
	case dur < 48*time.Hour:
		return "yesterday"
	case dur < 7*24*time.Hour:
		return digestPlural(int(dur.Hours()/24), "day ago", "days ago")
	default:
		return digestPlural(int(dur.Hours()/24/7), "week ago", "weeks ago")
	}
}

func digestPlural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return itoaBE(n) + " " + many
}

func itoaBE(n int) string { return fmt.Sprintf("%d", n) }

func joinAnd(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	default:
		return strings.Join(parts[:len(parts)-1], ", ") + ", and " + parts[len(parts)-1]
	}
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.Join(strings.Fields(s), " ")
}

func digestTruncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return strings.TrimSpace(s[:n]) + "…"
}
