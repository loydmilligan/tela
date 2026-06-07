package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/zcag/tela/backend/internal/auth"
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
// page_links.target_id = {id}). Empty list when no incoming links. 403
// "not a member" both when the caller has no membership in the target's
// space AND when the target page does not exist — collapsing the two cases
// stops non-members from enumerating page ids. Sort: space_name ASC, title
// ASC. Source pages in spaces the caller is not a member of are filtered
// out — no title or snippet leakage across membership boundaries.
func (s *Server) Backlinks(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	k, _ := auth.APIKeyFromContext(r.Context())
	out, ae := s.backlinksCore(r.Context(), u, k, id)
	if ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"backlinks": out})
}

// backlinksCore is the transport-agnostic core behind GET /api/pages/{id}/backlinks
// and the MCP list_backlinks tool: pages linking TO pageID, filtered to spaces
// the caller can access (no cross-membership title/snippet leakage). Missing
// target page collapses to the same 403 a non-member sees.
func (s *Server) backlinksCore(ctx context.Context, u *auth.User, k *auth.APIKey, pageID int64) ([]backlinkHit, *apiErr) {
	var targetSpaceID int64
	err := s.DB.QueryRowContext(ctx, `SELECT space_id FROM pages WHERE id = $1 AND deleted_at IS NULL`, pageID).Scan(&targetSpaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &apiErr{http.StatusForbidden, "forbidden", "not a member"}
	}
	if err != nil {
		return nil, &apiErr{http.StatusInternalServerError, "internal", "lookup page failed"}
	}
	if _, ae := s.membershipCore(ctx, u, k, targetSpaceID); ae != nil {
		return nil, ae
	}

	// A space-pinned bearer key is a strict ceiling: only list source pages in its
	// own space, else the result leaks titles/snippets of linking pages in OTHER
	// spaces the underlying user belongs to (mirrors searchCore's pin).
	var (
		rows *sql.Rows
		err2 error
	)
	if k != nil && k.SpaceID != nil {
		rows, err2 = s.DB.QueryContext(ctx, `
			SELECT p.id, p.space_id, s.name, p.title, p.body
			  FROM page_links l
			  JOIN pages p ON p.id = l.source_id
			  JOIN spaces s ON s.id = p.space_id
			  JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = $1) sm ON sm.space_id = p.space_id
			 WHERE l.target_id = $2 AND p.space_id = $3 AND p.deleted_at IS NULL
			 ORDER BY s.name ASC, p.title ASC`, u.ID, pageID, *k.SpaceID)
	} else {
		rows, err2 = s.DB.QueryContext(ctx, `
			SELECT p.id, p.space_id, s.name, p.title, p.body
			  FROM page_links l
			  JOIN pages p ON p.id = l.source_id
			  JOIN spaces s ON s.id = p.space_id
			  JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = $1) sm ON sm.space_id = p.space_id
			 WHERE l.target_id = $2 AND p.deleted_at IS NULL
			 ORDER BY s.name ASC, p.title ASC`, u.ID, pageID)
	}
	if err2 != nil {
		return nil, &apiErr{http.StatusInternalServerError, "internal", "list backlinks failed"}
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
			return nil, &apiErr{http.StatusInternalServerError, "internal", "scan backlink row failed"}
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, &apiErr{http.StatusInternalServerError, "internal", "iterate backlinks failed"}
	}

	out := make([]backlinkHit, 0, len(items))
	for _, it := range items {
		bc, err := pageBreadcrumb(ctx, s.DB, it.SourceID)
		if err != nil {
			return nil, &apiErr{http.StatusInternalServerError, "internal", "build breadcrumb failed"}
		}
		out = append(out, backlinkHit{
			PageID:     it.SourceID,
			SpaceID:    it.SpaceID,
			SpaceName:  it.SpaceName,
			Title:      it.Title,
			Breadcrumb: bc,
			Snippet:    buildBacklinkSnippet(it.Body, pageID),
		})
	}
	return out, nil
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
