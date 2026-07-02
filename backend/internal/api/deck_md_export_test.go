package api

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// GET /api/pages/{id}/deck.md returns the deck's raw Slidev source — the body
// verbatim (headmatter and all), with NO tela frontmatter block prepended (that
// distinguishes it from the generic /md export). A non-deck page is rejected.
func TestExportPageDeckMarkdown(t *testing.T) {
	ts, d := newWiredServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)
	sp := seedSpace(t, d, "Talks", "talks", alice)

	var deckID int64
	if err := d.QueryRow(`INSERT INTO pages (space_id, parent_id, title, body, position, props)
	     VALUES ($1, NULL, 'My Talk', '---'||chr(10)||'layout: cover'||chr(10)||'title: Hi'||chr(10)||'---'||chr(10)||'# Slide'||chr(10), 0, '{"deck":true}')
	     RETURNING id`, sp).Scan(&deckID); err != nil {
		t.Fatalf("seed deck: %v", err)
	}
	var docID int64
	if err := d.QueryRow(`INSERT INTO pages (space_id, parent_id, title, body, position, props)
	     VALUES ($1, NULL, 'Doc', '# Hi', 0, '{}') RETURNING id`, sp).Scan(&docID); err != nil {
		t.Fatalf("seed doc: %v", err)
	}

	c := loginClient(t, ts, "alice", "alicepw12")

	r, err := c.Get(fmt.Sprintf("%s/api/pages/%d/deck.md", ts.URL, deckID))
	if err != nil {
		t.Fatalf("GET deck.md: %v", err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("deck.md status=%d body=%s", r.StatusCode, body)
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Fatalf("content-type=%q", ct)
	}
	if cd := r.Header.Get("Content-Disposition"); !strings.Contains(cd, "My-Talk.md") {
		t.Fatalf("content-disposition=%q", cd)
	}
	// Verbatim body, first bytes are the deck's own headmatter — no tela
	// frontmatter (no id:/link: keys) got prepended.
	if !strings.HasPrefix(string(body), "---\nlayout: cover") {
		t.Fatalf("deck.md not verbatim source:\n%s", body)
	}
	if strings.Contains(string(body), "link:") || strings.Contains(string(body), "id:") {
		t.Fatalf("tela frontmatter leaked into deck.md:\n%s", body)
	}

	// A non-deck page is rejected.
	r2, err := c.Get(fmt.Sprintf("%s/api/pages/%d/deck.md", ts.URL, docID))
	if err != nil {
		t.Fatalf("GET doc deck.md: %v", err)
	}
	r2.Body.Close()
	if r2.StatusCode != http.StatusBadRequest {
		t.Fatalf("non-deck deck.md status=%d, want 400", r2.StatusCode)
	}
}
