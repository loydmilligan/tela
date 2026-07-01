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

	// For you — the personal, actionable lead: mentions, replies, and changes to
	// pages you follow (straight from this user's notification feed).
	d.ForYou = s.digestForYou(ctx, u.ID, sinceStr)

	// Notable this week — HUMAN-authored changes only. Bulk Atlas generation is
	// not "activity" a person did, so it's excluded here and rolled into one line.
	const updateLimit = 6
	updates, total, err := s.digestUpdates(ctx, u.ID, sinceStr, now, updateLimit)
	if err != nil {
		return d, err
	}
	d.Updates = updates
	if total > len(updates) {
		d.MoreCount = total - len(updates)
	}
	d.AtlasLine = s.digestAtlasLine(ctx, u.ID, sinceStr)

	// Needs attention — open questions first (someone's waiting on a human), then
	// stale Atlas docs as FYI. No conflicts: the agreement engine can't tell a
	// real contradiction from two repos sharing a section title, so its digest
	// output was false-positive noise.
	open, err := s.digestAttention(ctx, u.ID)
	if err != nil {
		return d, err
	}
	d.Attention = append(open, s.digestStale(ctx, u.ID)...)
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

// digestEmpty is true when there's nothing worth mailing: nothing personal, no
// human pages to list, and nothing needing attention. A quiet week (or one that
// was only bulk Atlas generation) doesn't send a hollow email.
func digestEmpty(d mailer.DigestData) bool {
	return len(d.ForYou) == 0 && len(d.Updates) == 0 && len(d.Attention) == 0
}

// digestJobLock is the fixed key for the advisory lock that serializes the send
// job across the whole deployment (every backend instance shares one Postgres).
const digestJobLock int64 = 4088231755

// SendDueDigests sends the weekly digest to every opted-in user who is due
// (never sent, or last send ≥7 days ago). It is engineered to NEVER double-send,
// through redeploys, crashes, concurrent CLI runs, or multiple instances:
//
//   - A Postgres advisory lock serializes the whole job. A second run — another
//     tick, a `tela digest run`, another instance — that can't grab the lock
//     just returns, so runs never overlap.
//   - Each user is CLAIMED with one atomic conditional UPDATE that stamps
//     digest_last_sent_at only while the user is still due. Two racers can't both
//     match, so at most one claims a given week.
//   - The claim (stamp) commits BEFORE the email is sent. A crash or redeploy
//     between claim and send makes the user MISS this week — never receive it
//     twice. A miss is recoverable next week; a duplicate isn't.
//   - Empty digests are skipped BEFORE claiming, so a quiet week doesn't burn the
//     slot.
//
// dryRun builds + counts without claiming or sending.
func (s *Server) SendDueDigests(ctx context.Context, dryRun bool) (sent int, err error) {
	now := time.Now().UTC()
	nowStr := now.Format(tsLayout)
	cutoff := now.AddDate(0, 0, -7).Format(tsLayout) // due when last_sent <= cutoff

	// Serialize the whole job on a single connection's session advisory lock.
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	var locked bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, digestJobLock).Scan(&locked); err != nil {
		return 0, err
	}
	if !locked {
		slog.Info("digest: another send run holds the lock — skipping")
		return 0, nil
	}
	// Session advisory locks outlive a pool checkout, so release explicitly
	// before conn.Close() returns it (deferred second → LIFO runs it first).
	defer func() { _, _ = conn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, digestJobLock) }()

	// Candidate set: opted in, active, verified email, and due. The per-user
	// claim below re-checks dueness atomically, so this is only a starting list.
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, username, COALESCE(email, ''), digest_last_sent_at
		  FROM users
		 WHERE digest_frequency = 'weekly' AND is_active = 1
		   AND email IS NOT NULL AND email <> '' AND email_verified_at IS NOT NULL
		   AND (digest_last_sent_at = '' OR digest_last_sent_at <= $1)`, cutoff)
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

	for _, d := range users {
		since := now.AddDate(0, 0, -7)
		if d.lastSent != "" {
			if t, e := time.Parse(tsLayout, d.lastSent); e == nil {
				since = t
			}
		}
		data, e := s.buildDigest(ctx, &d.u, since)
		if e != nil {
			slog.Error("digest: build failed", "user_id", d.u.ID, "err", e)
			continue
		}
		if digestEmpty(data) {
			continue // quiet week — skip WITHOUT claiming, so the slot is preserved
		}
		if dryRun {
			sent++
			continue
		}
		// Atomic claim: stamp now only if still due. 0 rows → another run already
		// took this week. This conditional UPDATE is the duplicate guard.
		res, e := s.DB.ExecContext(ctx, `
			UPDATE users SET digest_last_sent_at = $1
			 WHERE id = $2 AND digest_frequency = 'weekly'
			   AND (digest_last_sent_at = '' OR digest_last_sent_at <= $3)`,
			nowStr, d.u.ID, cutoff)
		if e != nil {
			slog.Error("digest: claim failed", "user_id", d.u.ID, "err", e)
			continue
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue // already claimed elsewhere — never double-send
		}
		// Stamp is committed. Now send; a failure here means this user misses THIS
		// week (logged), never a duplicate.
		if e := s.Mailer.Send(ctx, mailer.Digest(d.u.Email, digestSubject(data), data)); e != nil {
			slog.Error("digest: send failed after claim (user misses this week)", "user_id", d.u.ID, "err", e)
			continue
		}
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

// humanUpdateFilter keeps "Notable this week" to real human authoring: no Atlas
// auto-generation (rolled up separately) and no blank/"Untitled" pages.
const humanUpdateFilter = ` AND COALESCE(p.props->>'generator','') <> 'atlas'` +
	` AND p.title <> '' AND lower(p.title) <> 'untitled'`

func (s *Server) digestUpdates(ctx context.Context, userID int64, sinceStr string, now time.Time, limit int) ([]mailer.DigestUpdate, int, error) {
	var total int
	_ = s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pages p WHERE p.`+digestSpaceScope+
			` AND p.deleted_at IS NULL AND p.updated_at >= $2`+humanUpdateFilter, userID, sinceStr).Scan(&total)

	rows, err := s.DB.QueryContext(ctx, `
		SELECT p.id, p.space_id, p.title, s.name,
		       p.updated_at, (p.created_at >= $2) AS is_new,
		       COALESCE(p.props->>'summary', ''),
		       COALESCE(NULLIF(u.display_name, ''), u.username, '')
		  FROM pages p
		  JOIN spaces s ON s.id = p.space_id
		  LEFT JOIN LATERAL (
		    SELECT author_id FROM page_revisions r
		     WHERE r.page_id = p.id ORDER BY r.created_at DESC LIMIT 1
		  ) lr ON true
		  LEFT JOIN users u ON u.id = lr.author_id
		 WHERE p.`+digestSpaceScope+`
		   AND p.deleted_at IS NULL AND p.updated_at >= $2`+humanUpdateFilter+`
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
		var title, spaceName, updatedAt, summary, actor string
		var isNew bool
		if err := rows.Scan(&id, &spaceID, &title, &spaceName, &updatedAt, &isNew, &summary, &actor); err != nil {
			return nil, 0, err
		}
		verb := "edited"
		if isNew {
			verb = "created"
		}
		who := strings.ToUpper(verb[:1]) + verb[1:]
		if actor != "" {
			who = actor + " " + verb
		}
		out = append(out, mailer.DigestUpdate{
			Title:     title,
			SpaceName: spaceName,
			Actor:     who,
			When:      relTime(updatedAt, now),
			Summary:   digestTruncate(summary, 140),
			URL:       fmt.Sprintf("%s/spaces/%d/pages/%d", base, spaceID, id),
		})
	}
	return out, total, rows.Err()
}

// digestForYou is the personal lead — this user's notification feed for the
// week (mentions, replies, and changes to pages they follow), rendered as ready
// sentences. Best-effort.
func (s *Server) digestForYou(ctx context.Context, userID int64, sinceStr string) []mailer.DigestForYou {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT n.type, n.subject_id, COALESCE(n.space_id, 0),
		       COALESCE(n.data->>'page_title', ''), COALESCE(n.data->>'actor_username', ''),
		       COALESCE(n.data->>'snippet', '')
		  FROM notifications n
		 WHERE n.user_id = $1 AND n.created_at >= $2
		 ORDER BY n.created_at DESC
		 LIMIT 5`, userID, sinceStr)
	if err != nil {
		return nil
	}
	defer rows.Close()
	base := strings.TrimRight(canonicalBaseURL(), "/")
	var out []mailer.DigestForYou
	for rows.Next() {
		var ntype, pageTitle, actor, snippet string
		var subjectID, spaceID int64
		if err := rows.Scan(&ntype, &subjectID, &spaceID, &pageTitle, &actor, &snippet); err != nil {
			return out
		}
		if pageTitle == "" {
			pageTitle = "a page"
		}
		who := actor
		if who == "" {
			who = "Someone"
		}
		var text string
		showSnippet := false
		switch ntype {
		case "mention":
			text, showSnippet = fmt.Sprintf("%s mentioned you in %s", who, pageTitle), true
		case "comment_reply":
			text, showSnippet = fmt.Sprintf("%s replied to you in %s", who, pageTitle), true
		case "page_updated":
			text = fmt.Sprintf("%s — a page you follow — was updated", pageTitle)
		default:
			continue
		}
		item := mailer.DigestForYou{Text: text, URL: base}
		if spaceID > 0 && subjectID > 0 {
			item.URL = fmt.Sprintf("%s/spaces/%d/pages/%d", base, spaceID, subjectID)
		}
		if showSnippet {
			item.Snippet = digestTruncate(oneLine(snippet), 100)
		}
		out = append(out, item)
	}
	return out
}

// digestAtlasLine rolls all this week's Atlas auto-generation into one honest
// line ("" when there was none), so bulk output is acknowledged without being
// mistaken for human activity.
func (s *Server) digestAtlasLine(ctx context.Context, userID int64, sinceStr string) string {
	var pages, sources int
	err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*), COUNT(DISTINCT parent_id) FROM pages p
		  WHERE p.`+digestSpaceScope+` AND p.deleted_at IS NULL
		    AND p.updated_at >= $2 AND p.props->>'generator' = 'atlas'`,
		userID, sinceStr).Scan(&pages, &sources)
	if err != nil || pages == 0 {
		return ""
	}
	if sources <= 1 {
		return fmt.Sprintf("Atlas refreshed %d page%s this week.", pages, plural(pages))
	}
	return fmt.Sprintf("Atlas refreshed %d pages across %d sources this week.", pages, sources)
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
			Kind:   "QUESTION",
			Tone:   "info",
			Title:  digestTruncate(oneLine(body), 90),
			Detail: fmt.Sprintf("Asked by %s on %s · no answer yet.", actor, title),
			URL:    fmt.Sprintf("%s/spaces/%d/pages/%d", base, spaceID, pageID),
		})
	}
	return out, rows.Err()
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

const digestGistSystem = `You write the one-sentence opener of a team wiki's weekly digest email. From the brief, say what MATTERS to the reader this week — what's waiting on them, the notable pages or decisions, anything needing attention. IGNORE routine bulk / auto-generated content; never credit it to a person. Concrete and specific, plain language, no greeting, no raw counts, no markdown. One sentence, ~30 words max.`

// digestGist produces the "gist" line from what actually matters (personal
// items, notable human edits, attention) — an LLM sentence when configured, else
// the deterministic fallback. Bulk Atlas output is passed only as background so
// the model doesn't headline it.
func (s *Server) digestGist(ctx context.Context, d mailer.DigestData) string {
	fallback := digestGistFallback(d)
	if s.llm == nil || !s.llm.Enabled() {
		return fallback
	}
	if len(d.ForYou) == 0 && len(d.Updates) == 0 && len(d.Attention) == 0 {
		return ""
	}
	var b strings.Builder
	if len(d.ForYou) > 0 {
		b.WriteString("Waiting on the reader:\n")
		for _, f := range d.ForYou {
			b.WriteString("- " + f.Text + "\n")
		}
	}
	if len(d.Updates) > 0 {
		b.WriteString("Notable human edits:\n")
		for _, up := range d.Updates {
			line := "- " + up.Title
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
	if d.AtlasLine != "" {
		b.WriteString("Background (do not headline): " + d.AtlasLine + "\n")
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

// digestGistFallback is the deterministic opener when the LLM is off: lead with
// what's personal, else summarize notable edits + attention. No bulk counts.
func digestGistFallback(d mailer.DigestData) string {
	if len(d.ForYou) > 0 {
		return digestPlural(len(d.ForYou), "thing is waiting for you", "things are waiting for you") + " this week."
	}
	parts := []string{}
	if n := len(d.Updates) + d.MoreCount; n > 0 {
		parts = append(parts, digestPlural(n, "page changed", "pages changed"))
	}
	if len(d.Attention) > 0 {
		parts = append(parts, digestPlural(len(d.Attention), "item needs attention", "items need attention"))
	}
	if len(parts) == 0 {
		return ""
	}
	return joinAnd(parts) + " this week."
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
