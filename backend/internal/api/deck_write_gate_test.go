package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeDeckLint stands in for the deck sidecar's /lint endpoint. It flags any body
// containing the sentinel BROKENDECK as having one structural error, everything
// else as clean — enough to drive the write-gate's reject / pass paths without a
// real Slidev build.
func fakeDeckLint(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/lint" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		b, _ := io.ReadAll(r.Body)
		out := lintDeckOut{OK: true}
		if strings.Contains(string(b), "BROKENDECK") {
			out = lintDeckOut{Errors: 1, Issues: []lintIssue{{Slide: 2, Level: "error", Message: "slide body starts with frontmatter keys"}}}
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("TELA_DECK_URL", srv.URL)
}

func deckReq(spaceID int64, body string) pageCreateRequest {
	return pageCreateRequest{SpaceID: spaceID, Title: "D", Body: body, Props: map[string]any{"deck": true}}
}

// The agent-write deck gate rejects a structurally broken deck, lets a clean one
// (and a non-deck) through, and leaves interactive (non-agent) writes ungated.
func TestDeckWriteGate(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	owner := seedUser(t, d, "owner", "ownerpw123", false)
	u := authUser(owner, "owner", false)
	sp := seedSpace(t, d, "Deck Space", "deck-space", owner)

	const broken = "---\nlayout: cover\ntitle: BROKENDECK\n---\n"
	const clean = "---\nlayout: cover\ntitle: fine\n---\n"

	t.Run("agent broken deck rejected", func(t *testing.T) {
		fakeDeckLint(t)
		_, ae := srv.createPageCore(withAgentWrite(context.Background()), u, nil, deckReq(sp, broken))
		if ae == nil || ae.Code != "deck_invalid" {
			t.Fatalf("want deck_invalid, got %+v", ae)
		}
		if ae.Status != http.StatusUnprocessableEntity {
			t.Fatalf("want 422, got %d", ae.Status)
		}
	})

	t.Run("agent clean deck saved", func(t *testing.T) {
		fakeDeckLint(t)
		if _, ae := srv.createPageCore(withAgentWrite(context.Background()), u, nil, deckReq(sp, clean)); ae != nil {
			t.Fatalf("clean deck rejected: %+v", ae)
		}
	})

	t.Run("interactive broken deck NOT gated", func(t *testing.T) {
		fakeDeckLint(t)
		// Plain context (no withAgentWrite) = the FE autosave path; must save.
		if _, ae := srv.createPageCore(context.Background(), u, nil, deckReq(sp, broken)); ae != nil {
			t.Fatalf("interactive write should not be gated: %+v", ae)
		}
	})

	t.Run("sidecar down fails open", func(t *testing.T) {
		t.Setenv("TELA_DECK_URL", "http://127.0.0.1:1") // connection refused
		if _, ae := srv.createPageCore(withAgentWrite(context.Background()), u, nil, deckReq(sp, broken)); ae != nil {
			t.Fatalf("sidecar-down should fail open, got %+v", ae)
		}
	})

	t.Run("agent broken body on update rejected", func(t *testing.T) {
		fakeDeckLint(t)
		p, ae := srv.createPageCore(withAgentWrite(context.Background()), u, nil, deckReq(sp, clean))
		if ae != nil {
			t.Fatalf("seed deck: %+v", ae)
		}
		bad := broken
		_, ae = srv.updatePageCore(context.Background(), u, nil, p.ID, pageUpdateRequest{Body: &bad}, true)
		if ae == nil || ae.Code != "deck_invalid" {
			t.Fatalf("update want deck_invalid, got %+v", ae)
		}
	})
}
