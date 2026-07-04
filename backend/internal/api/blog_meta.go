package api

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Blog-card metadata derived from a page for the public index surfaces (a public
// space's front page and /u/{handle}). The index payloads stay one round trip —
// the server computes the excerpt / reading-time / cover here rather than
// shipping every body to the client. Read-only, public-by-design fields only.

const blogWordsPerMin = 220 // matches the reader's WORDS_PER_MIN (ReaderShell.tsx)

// blogCardMeta is the derived per-post summary an index card renders.
type blogCardMeta struct {
	Kind           string   `json:"kind,omitempty"` // "deck" for slide decks; "" for prose docs
	Excerpt        string   `json:"excerpt"`
	ReadingMinutes int      `json:"reading_minutes"`
	Cover          string   `json:"cover,omitempty"`
	Tags           []string `json:"tags,omitempty"`
}

// blogMetaFor builds the card metadata from a page's body + frontmatter props.
// The excerpt prefers an author-set standfirst in props (summary/excerpt/
// description) and otherwise falls back to the lead of the body as plain text.
func blogMetaFor(body string, props map[string]any) blogCardMeta {
	// A deck's body is Slidev source — running the prose excerpter over it yields
	// mangled YAML/separator text, and reading-time is meaningless. Use the
	// author/LLM summary; the cover (first slide) is filled in by the caller,
	// which has the space+page ids for the cover URL.
	if isDeckBag(props) {
		return blogCardMeta{
			Kind:    "deck",
			Excerpt: clip(strings.Join(strings.Fields(propString(props, "summary", "excerpt", "description")), " "), 180),
			Tags:    propStrings(props, "tags"),
		}
	}
	// A sheet's body is Defter markdown (compact tables + a style block) — the
	// prose excerpter mangles it and reading-time is meaningless. Use the summary;
	// the cover (grid-preview OG) is filled in by the caller (space+page ids).
	if isSheetBag(props) {
		return blogCardMeta{
			Kind:    "sheet",
			Excerpt: clip(strings.Join(strings.Fields(propString(props, "summary", "excerpt", "description")), " "), 180),
			Tags:    propStrings(props, "tags"),
		}
	}
	return blogCardMeta{
		Excerpt:        postExcerpt(body, props, 180),
		ReadingMinutes: readingMinutes(body),
		Cover:          propString(props, "cover", "image"),
		Tags:           propStrings(props, "tags"),
	}
}

// readingMinutes is an at-a-glance estimate, never a parser — whitespace-split
// word count over a fixed words-per-minute, floored at 1.
func readingMinutes(body string) int {
	words := len(strings.Fields(body))
	m := (words + blogWordsPerMin - 1) / blogWordsPerMin
	if m < 1 {
		return 1
	}
	return m
}

// postExcerpt returns a one-line standfirst for a post. An author-written
// summary in frontmatter wins; otherwise the body's opening prose, stripped of
// markdown and clipped to ~max chars at a word boundary.
func postExcerpt(body string, props map[string]any, max int) string {
	if s := propString(props, "summary", "excerpt", "description"); s != "" {
		return clip(strings.Join(strings.Fields(s), " "), max)
	}
	return clip(plainTextLead(body), max)
}

var (
	reCodeFence    = regexp.MustCompile("(?s)```.*?```")
	reImage        = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	reLink         = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	reWikilinkPipe = regexp.MustCompile(`\[\[[^\]|]+\|([^\]]+)\]\]`) // [[slug|alias]] → alias
	reWikilink     = regexp.MustCompile(`\[\[([^\]]+)\]\]`)          // [[slug]] → slug
	reHTMLTag      = regexp.MustCompile(`<[^>]+>`)
	reInlineMark   = regexp.MustCompile("[*_`~>#]+")
	reWhitespace   = regexp.MustCompile(`\s+`)
	reLeadingList  = regexp.MustCompile(`(?m)^\s*([-*+]|\d+\.)\s+`)
)

// plainTextLead flattens the lead of a markdown body to a single plain-text line
// suitable for an excerpt. It is a display heuristic, not a faithful renderer:
// drops code blocks, images, HTML and table/quote/heading punctuation, and
// keeps the visible text of links and wikilinks.
func plainTextLead(md string) string {
	s := reCodeFence.ReplaceAllString(md, " ")
	s = reImage.ReplaceAllString(s, " ")
	s = reWikilinkPipe.ReplaceAllString(s, "$1")
	s = reWikilink.ReplaceAllString(s, "$1")
	s = reLink.ReplaceAllString(s, "$1")
	s = reHTMLTag.ReplaceAllString(s, " ")
	s = reLeadingList.ReplaceAllString(s, "")
	s = reInlineMark.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "|", " ")
	s = reWhitespace.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// clip truncates to at most max runes, breaking on the last word boundary and
// appending an ellipsis when it actually cut something.
func clip(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	cut := string(r[:max])
	if i := strings.LastIndex(cut, " "); i > max/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:—-") + "…"
}

// decodeProps unmarshals a page's jsonb props column into a map, tolerating a
// NULL/empty column. Never returns nil so callers can index safely.
func decodeProps(raw []byte) map[string]any {
	props := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &props)
	}
	return props
}

// propString returns the first non-empty string value among the given prop keys.
func propString(props map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := props[k].(string); ok {
			if t := strings.TrimSpace(v); t != "" {
				return t
			}
		}
	}
	return ""
}

// propStrings returns a []string from a props array key (e.g. tags), skipping
// non-string / empty entries. Returns nil when absent so the JSON field omits.
func propStrings(props map[string]any, key string) []string {
	raw, ok := props[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			if t := strings.TrimSpace(s); t != "" {
				out = append(out, t)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
