package api

import (
	"database/sql"
	"errors"
	"fmt"
	"html"
	"net/http"
	"os"
	"regexp"
	"strings"
)

// botUASubstrings is the lowercase substring allowlist that gates whether
// GET /p/{id} returns OG HTML (bot) vs. 302 to the SPA (real browser). The
// trailing two entries (`bot/` and `bot `) catch the long tail of crawlers
// that follow the convention `<Name>Bot/<version>` or `<Name>Bot ...`.
var botUASubstrings = []string{
	"slackbot-linkexpanding",
	"twitterbot",
	"facebookexternalhit",
	"discordbot",
	"telegrambot",
	"linkedinbot",
	"whatsapp",
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
		title     string
		body      string
		spaceName string
	)
	err := s.DB.QueryRowContext(r.Context(),
		`SELECT p.title, p.body, sp.name
		   FROM pages p
		   JOIN spaces sp ON sp.id = p.space_id
		  WHERE p.id = ?`, pageID,
	).Scan(&title, &body, &spaceName)
	if errors.Is(err, sql.ErrNoRows) {
		writeNotFoundHTML(w)
		return
	}
	if err != nil {
		writeInternalHTML(w)
		return
	}

	if !isBotUA(r.Header.Get("User-Agent")) {
		http.Redirect(w, r, fmt.Sprintf("/pages/%d", pageID), http.StatusFound)
		return
	}

	writeOGHTML(w, pageID, title, body, spaceName)
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

// publicBaseURL returns the env-configured base URL with a single trailing
// slash trimmed. Empty when TELA_PUBLIC_BASE_URL is unset, producing path-only
// og:url / og:image — Slack and Twitter handle that fine in dev.
func publicBaseURL() string {
	return strings.TrimRight(os.Getenv("TELA_PUBLIC_BASE_URL"), "/")
}

// writeOGHTML emits the OG HTML payload. All user-controlled fields go through
// html.EscapeString — page titles and bodies are end-user input, and a stored
// XSS via crawler-rendered OG cards is a real concern even though the bot
// path bypasses the SPA.
func writeOGHTML(w http.ResponseWriter, pageID int64, title, body, spaceName string) {
	ogTitle := runeTruncate(title+" — "+spaceName, 100)
	plain := stripMarkdownToText(body)
	ogDesc := runeTruncate(plain, 200)

	base := publicBaseURL()
	pageURL := fmt.Sprintf("%s/p/%d", base, pageID)
	imageURL := fmt.Sprintf("%s/p/%d/og.png", base, pageID)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>%s</title>
  <meta property="og:title" content="%s">
  <meta property="og:description" content="%s">
  <meta property="og:image" content="%s">
  <meta property="og:url" content="%s">
  <meta property="og:type" content="article">
  <meta name="twitter:card" content="summary_large_image">
</head>
<body></body>
</html>`,
		html.EscapeString(ogTitle),
		html.EscapeString(ogTitle),
		html.EscapeString(ogDesc),
		html.EscapeString(imageURL),
		html.EscapeString(pageURL),
	)
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

