package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const backlinksSnippetRadius = 60

type backlinkHit struct {
	PageID     int64    `json:"page_id"`
	SpaceID    int64    `json:"space_id"`
	SpaceName  string   `json:"space_name"`
	Title      string   `json:"title"`
	Breadcrumb []string `json:"breadcrumb"`
	Snippet    string   `json:"snippet"`
}

// Backlinks lists pages that link TO the requested page (i.e. sources where
// page_links.target_id = {id}). 404 if the page itself doesn't exist; empty
// list when no incoming links. Sort: space_name ASC, title ASC. Source pages
// in spaces the caller is not a member of are filtered out — no title or
// snippet leakage across membership boundaries.
func (s *Server) Backlinks(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}

	var targetSpaceID int64
	err := s.DB.QueryRowContext(r.Context(), `SELECT space_id FROM pages WHERE id = ?`, id).Scan(&targetSpaceID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "page not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup page failed")
		return
	}
	if _, ok := s.requireMembership(w, r, targetSpaceID); !ok {
		return
	}

	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT p.id, p.space_id, s.name, p.title, p.body
		  FROM page_links l
		  JOIN pages p ON p.id = l.source_id
		  JOIN spaces s ON s.id = p.space_id
		  JOIN space_members sm ON sm.space_id = p.space_id AND sm.user_id = ?
		 WHERE l.target_id = ?
		 ORDER BY s.name ASC, p.title ASC`, u.ID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list backlinks failed")
		return
	}
	defer rows.Close()

	type rowItem struct {
		SourceID, SpaceID      int64
		SpaceName, Title, Body string
	}
	items := []rowItem{}
	for rows.Next() {
		var it rowItem
		if err := rows.Scan(&it.SourceID, &it.SpaceID, &it.SpaceName, &it.Title, &it.Body); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan backlink row failed")
			return
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate backlinks failed")
		return
	}

	out := make([]backlinkHit, 0, len(items))
	for _, it := range items {
		bc, err := pageBreadcrumb(r.Context(), s.DB, it.SourceID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "build breadcrumb failed")
			return
		}
		out = append(out, backlinkHit{
			PageID:     it.SourceID,
			SpaceID:    it.SpaceID,
			SpaceName:  it.SpaceName,
			Title:      it.Title,
			Breadcrumb: bc,
			Snippet:    buildBacklinkSnippet(it.Body, id),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"backlinks": out})
}

// wikilinkMarkdownRE matches `[display text](tela://page/{N})` — the exact
// form syncPageLinks expects (and Milkdown emits) on save. Used to collapse
// wrapped wikilinks to their display text before snippet windowing so that
// raw markdown punctuation (`](`, `]`) doesn't bleed into the rendered
// snippet.
var wikilinkMarkdownRE = regexp.MustCompile(`\[([^\]]+)\]\(tela://page/(\d+)\)`)

// buildBacklinkSnippet returns a best-effort plain-text excerpt around the
// first occurrence of a reference to targetID in body, with markdown link
// wrappers collapsed to their display text, bare URLs stripped, and a nearby
// word fragment wrapped in literal <mark>…</mark>. Returns "" when no usable
// window can be built.
//
// Per the FTS5 snippet() contract used by /api/search, the body is emitted
// RAW — the frontend uses an XSS-safe splitter that treats <mark> only as a
// delimiter. Do NOT escape <, >, & here.
func buildBacklinkSnippet(body string, targetID int64) string {
	cleaned, anchorStart, anchorEnd := cleanBodyAndAnchor(body, targetID)
	if anchorStart < 0 {
		return ""
	}

	start := anchorStart - backlinksSnippetRadius
	if start < 0 {
		start = 0
	}
	for start > 0 && !utf8.RuneStart(cleaned[start]) {
		start--
	}
	end := anchorEnd + backlinksSnippetRadius
	if end > len(cleaned) {
		end = len(cleaned)
	}
	for end < len(cleaned) && !utf8.RuneStart(cleaned[end]) {
		end++
	}

	before := cleaned[start:anchorStart]
	after := cleaned[anchorEnd:end]

	bStart, bEnd := findTrailingWord(before)
	aStart, aEnd := findLeadingWord(after)

	var b strings.Builder
	if start > 0 {
		b.WriteString("…")
	}
	switch {
	case bStart >= 0:
		b.WriteString(before[:bStart])
		b.WriteString("<mark>")
		b.WriteString(before[bStart:bEnd])
		b.WriteString("</mark>")
		b.WriteString(before[bEnd:])
		b.WriteString(after)
	case aStart >= 0:
		b.WriteString(before)
		b.WriteString(after[:aStart])
		b.WriteString("<mark>")
		b.WriteString(after[aStart:aEnd])
		b.WriteString("</mark>")
		b.WriteString(after[aEnd:])
	default:
		return ""
	}
	if end < len(cleaned) {
		b.WriteString("…")
	}
	return strings.TrimSpace(b.String())
}

// cleanBodyAndAnchor collapses every `[X](tela://page/N)` wikilink in body to
// its display text X, and returns:
//   - cleaned: the resulting body with all wrapped wikilinks collapsed;
//   - anchorStart, anchorEnd: the byte offsets in cleaned that mark the
//     window's anchor — for a wrapped link to targetID, this is a zero-width
//     anchor at the end of its display text (so the title text is preserved
//     in the snippet); for a bare `tela://page/{targetID}` URL not part of a
//     wrapped link, this is the URL's byte range (so the URL itself is
//     stripped from the snippet).
//
// The earliest of (a) first wrapped link with targetID, (b) first bare URL
// with targetID wins. Returns (-1, -1) when targetID is not referenced.
func cleanBodyAndAnchor(body string, targetID int64) (string, int, int) {
	targetURL := fmt.Sprintf("tela://page/%d", targetID)
	anchorStart, anchorEnd := -1, -1

	var out strings.Builder
	cursor := 0

	matches := wikilinkMarkdownRE.FindAllStringSubmatchIndex(body, -1)
	for _, m := range matches {
		fullStart, fullEnd := m[0], m[1]
		textStart, textEnd := m[2], m[3]
		idStart, idEnd := m[4], m[5]

		gap := body[cursor:fullStart]
		if anchorStart < 0 {
			if idx := strings.Index(gap, targetURL); idx >= 0 {
				absInCleaned := out.Len() + idx
				anchorStart = absInCleaned
				anchorEnd = absInCleaned + len(targetURL)
			}
		}
		out.WriteString(gap)

		out.WriteString(body[textStart:textEnd])

		if anchorStart < 0 {
			if id, err := strconv.ParseInt(body[idStart:idEnd], 10, 64); err == nil && id == targetID {
				anchorStart = out.Len()
				anchorEnd = out.Len()
			}
		}

		cursor = fullEnd
	}

	tail := body[cursor:]
	if anchorStart < 0 {
		if idx := strings.Index(tail, targetURL); idx >= 0 {
			absInCleaned := out.Len() + idx
			anchorStart = absInCleaned
			anchorEnd = absInCleaned + len(targetURL)
		}
	}
	out.WriteString(tail)

	return out.String(), anchorStart, anchorEnd
}

// findTrailingWord returns the byte offsets of the last contiguous run of
// letter/digit runes in s, skipping any trailing non-word characters. Returns
// (-1, -1) when no word is present.
func findTrailingWord(s string) (start, end int) {
	end = len(s)
	for end > 0 {
		r, size := utf8.DecodeLastRuneInString(s[:end])
		if isWordRune(r) {
			break
		}
		end -= size
	}
	start = end
	for start > 0 {
		r, size := utf8.DecodeLastRuneInString(s[:start])
		if !isWordRune(r) {
			break
		}
		start -= size
	}
	if start == end {
		return -1, -1
	}
	return start, end
}

// findLeadingWord returns the byte offsets of the first contiguous run of
// letter/digit runes in s, skipping any leading non-word characters. Returns
// (-1, -1) when no word is present.
func findLeadingWord(s string) (start, end int) {
	for start < len(s) {
		r, size := utf8.DecodeRuneInString(s[start:])
		if isWordRune(r) {
			break
		}
		start += size
	}
	end = start
	for end < len(s) {
		r, size := utf8.DecodeRuneInString(s[end:])
		if !isWordRune(r) {
			break
		}
		end += size
	}
	if start == end {
		return -1, -1
	}
	return start, end
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}
