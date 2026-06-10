package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strings"
)

// botUASubstrings is the lowercase substring allowlist that gates whether
// GET /p/{id} returns OG HTML (bot) vs. 302 to the SPA (real browser). The
// trailing two entries (`bot/` and `bot `) catch the long tail of crawlers
// that follow the convention `<Name>Bot/<version>` or `<Name>Bot ...`.
//
// Mirror of @share_bots regex in deploy/proxy/Caddyfile — keep in sync.
var botUASubstrings = []string{
	"slackbot-linkexpanding",
	"twitterbot",
	"facebookexternalhit",
	"discordbot",
	"telegrambot",
	"linkedinbot",
	"whatsapp",
	"mastodon",        // Mastodon link preview (http.rb/… (Mastodon/4.x; …))
	"cardyb",          // Bluesky link-card service (Bluesky Cardyb/1.1)
	"slack-imgproxy",  // Slack image unfurl fetcher
	"facebookcatalog", // Facebook catalog/share crawler (not …externalhit)
	"pinterest",       // Pinterest rich-pin fetcher
	"bot/",
	"bot ",
}

// HandlePublicShare returns a handler for GET /p/{id} (and /p/{id}/{slug} —
// slug is ignored on read, it is a human-friendly trailing segment for share
// links). Bot UAs receive a minimal OG HTML document; real browsers are 302'd
// to the SPA route. NO session/cookie check — the route MUST be bypassed by
// auth.Middleware (see auth.IsPublicPath) because crawlers don't carry sessions.
func (s *Server) HandlePublicShare(w http.ResponseWriter, r *http.Request) {
	pageID, ok := parseIDParam(w, r, "id")
	if !ok {
		writeNotFoundHTML(w)
		return
	}

	var (
		title      string
		body       string
		spaceName  string
		spaceID    int64
		visibility string
	)
	err := s.DB.QueryRowContext(r.Context(),
		`SELECT p.title, p.body, sp.name, p.space_id, sp.visibility
		   FROM pages p
		   JOIN spaces sp ON sp.id = p.space_id
		  WHERE p.id = $1 AND p.deleted_at IS NULL`, pageID,
	).Scan(&title, &body, &spaceName, &spaceID, &visibility)
	if errors.Is(err, sql.ErrNoRows) {
		writeNotFoundHTML(w)
		return
	}
	if err != nil {
		writeInternalHTML(w)
		return
	}

	if !isBotUA(r.Header.Get("User-Agent")) {
		// A page in a PUBLIC space reads without login — send the browser to the
		// no-token public reader. Everything else goes to the in-app page route
		// (the SPA gates it on a session as before). The SPA page route is nested
		// under the space (/spaces/{spaceID}/pages/{id}/{slug}); a bare
		// /pages/{id} no longer resolves and renders the SPA's not-found view.
		dest := pageAppPath(spaceID, pageID, title)
		if visibility == spaceVisibilityPublic {
			dest = publicReaderPath(spaceID, pageID, title)
		}
		http.Redirect(w, r, dest, http.StatusFound)
		return
	}

	s.writeOGHTML(r.Context(), w, pageID, title, body, spaceName)
}

// isBotUA reports whether ua matches any entry in the bot allowlist. Match is
// case-insensitive substring.
func isBotUA(ua string) bool {
	if ua == "" {
		return false
	}
	lower := strings.ToLower(ua)
	for _, needle := range botUASubstrings {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

// writeOGHTML emits the OG HTML payload. All user-controlled fields go through
// html.EscapeString — page titles and bodies are end-user input, and a stored
// XSS via crawler-rendered OG cards is a real concern even though the bot
// path bypasses the SPA. og:url here is the /p/{id} permalink; the share
// surface (M15.5) reuses writeOGHTMLWithURL with /share/{token}.
//
// og:url + og:image are branded to the page's org custom domain when it has one
// (shareOriginForPage) — /p/* is served on custom domains too, so a permalink
// copied from a white-label app should unfurl as that org's domain, matching the
// /share/* surface. Falls back to the canonical origin (or path-only in dev).
func (s *Server) writeOGHTML(ctx context.Context, w http.ResponseWriter, pageID int64, title, body, spaceName string) {
	origin := s.shareOriginForPage(ctx, pageID)
	// Canonical permalink carries the cosmetic slug (/p/{id}/{slug}); the id is
	// still what resolves, so a stale slug never breaks.
	writeOGHTMLWithURL(w, pageID, title, body, spaceName,
		origin+pagePermalinkPath(pageID, title), origin)
}

func writeNotFoundHTML(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`<!doctype html><title>Not found</title>`))
}

func writeInternalHTML(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write([]byte(`<!doctype html><title>Server error</title>`))
}

// runeTruncate returns at most n runes of s. If s is longer than n it appends
// a horizontal ellipsis (…) to signal truncation. Rune-aware so emoji / CJK
// titles don't split mid-codepoint and turn into � in Slack.
func runeTruncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

var (
	fencedCodeRE = regexp.MustCompile("(?s)```.*?```|~~~.*?~~~")
	atxHeadingRE = regexp.MustCompile(`(?m)^#+\s+`)
	imageRE      = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	linkRE       = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	inlineCodeRE = regexp.MustCompile("`([^`]*)`")
	whitespaceRE = regexp.MustCompile(`\s+`)
)

// stripMarkdownToText reduces a markdown body to a plain-text excerpt suitable
// for og:description. The rules are intentionally minimal regex — pulling in a
// full markdown parser for a 200-char excerpt would be overkill and risk
// dragging code-fence content into the description through edge cases the
// parser handles "correctly" but we don't want surfaced.
//
//   - Fenced code blocks (```…``` and ~~~…~~~) are dropped entirely.
//   - ATX heading markers (#+\s+) are stripped, keeping the heading text.
//   - Image syntax ![alt](url) is dropped (alt-text won't help a crawler card).
//   - Link / wikilink syntax [text](url) collapses to just `text`. Wikilinks
//     ride the same regex because their wire form is [Title](tela://page/N).
//   - Inline code `code` collapses to its contents.
//   - All whitespace runs collapse to single spaces; result is trimmed.
func stripMarkdownToText(body string) string {
	s := fencedCodeRE.ReplaceAllString(body, " ")
	s = atxHeadingRE.ReplaceAllString(s, "")
	s = imageRE.ReplaceAllString(s, "")
	s = linkRE.ReplaceAllString(s, "$1")
	s = inlineCodeRE.ReplaceAllString(s, "$1")
	s = whitespaceRE.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}
