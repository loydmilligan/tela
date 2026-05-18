package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
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
// list when no incoming links. Sort: space_name ASC, title ASC.
func (s *Server) Backlinks(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}

	var exists int
	err := s.DB.QueryRowContext(r.Context(), `SELECT 1 FROM pages WHERE id = ?`, id).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "page not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup page failed")
		return
	}

	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT p.id, p.space_id, s.name, p.title, p.body
		  FROM page_links l
		  JOIN pages p ON p.id = l.source_id
		  JOIN spaces s ON s.id = p.space_id
		 WHERE l.target_id = ?
		 ORDER BY s.name ASC, p.title ASC`, id)
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

// buildBacklinkSnippet returns a best-effort plain-text excerpt around the
// first occurrence of `tela://page/{targetID}` in body, with the URL itself
// stripped and a nearby word fragment wrapped in literal <mark>…</mark>.
// Returns "" when no usable window can be built.
//
// Per the FTS5 snippet() contract used by /api/search, body is emitted RAW —
// the frontend uses an XSS-safe splitter that treats <mark> only as a
// delimiter. Do NOT escape <, >, & here.
func buildBacklinkSnippet(body string, targetID int64) string {
	urlPattern := fmt.Sprintf("tela://page/%d", targetID)
	idx := strings.Index(body, urlPattern)
	if idx < 0 {
		return ""
	}
	urlEnd := idx + len(urlPattern)

	start := idx - backlinksSnippetRadius
	if start < 0 {
		start = 0
	}
	for start > 0 && !utf8.RuneStart(body[start]) {
		start--
	}
	end := urlEnd + backlinksSnippetRadius
	if end > len(body) {
		end = len(body)
	}
	for end < len(body) && !utf8.RuneStart(body[end]) {
		end++
	}

	before := body[start:idx]
	after := body[urlEnd:end]

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
	if end < len(body) {
		b.WriteString("…")
	}
	return strings.TrimSpace(b.String())
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
