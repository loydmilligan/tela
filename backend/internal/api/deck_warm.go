package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// deckWarmer pre-builds a deck's interactive SPA after its source changes, so the
// next "Present" opens instantly instead of paying the ~5s cold `slidev build`.
//
// Correctness NEVER depends on this: every render/build is content-keyed (see
// CACHE_EPOCH in the sidecar), so a changed body always yields a new build id and
// a stale build can't be served. The warmer only moves the build cost off the
// click. It's triggered server-side from afterPageWrite / createPageCore, so
// EVERY write path warms uniformly — UI save, MCP update_page, WebDAV/rsync sync,
// any automation — without each transport needing to know about decks.
//
// A per-page debounce coalesces bursts (autosave, a sync rewriting a deck) into
// one build of the LATEST content (the fire re-reads the page from the DB). A
// global semaphore caps concurrent warms so a bulk sync of many decks can't flood
// the render sidecar or starve on-demand Present builds.
type deckWarmer struct {
	s      *Server
	mu     sync.Mutex
	timers map[int64]*time.Timer
	sem    chan struct{}
}

const (
	deckWarmDebounce = 2 * time.Second
	deckWarmMaxConc  = 2
	deckWarmTimeout  = 240 * time.Second
)

func newDeckWarmer(s *Server) *deckWarmer {
	return &deckWarmer{s: s, timers: map[int64]*time.Timer{}, sem: make(chan struct{}, deckWarmMaxConc)}
}

// schedule (re)arms a debounced warm for a page. Cheap and safe to call on any
// write; it no-ops at fire time if the page turns out not to be a deck. nil-safe
// so tests with a bare Server don't need the warmer wired.
func (w *deckWarmer) schedule(pageID int64) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if t, ok := w.timers[pageID]; ok {
		t.Reset(deckWarmDebounce)
		return
	}
	w.timers[pageID] = time.AfterFunc(deckWarmDebounce, func() {
		w.mu.Lock()
		delete(w.timers, pageID)
		w.mu.Unlock()
		w.run(pageID)
	})
}

// run builds the deck's SPA for the page's current source. Best-effort: any error
// just means the next Present pays the build itself.
func (w *deckWarmer) run(pageID int64) {
	w.sem <- struct{}{}
	defer func() { <-w.sem }()
	ctx, cancel := context.WithTimeout(context.Background(), deckWarmTimeout)
	defer cancel()
	p, err := selectPageByID(ctx, w.s.DB, pageID)
	if err != nil || !isDeckBag(p.Props) {
		return
	}
	base := fmt.Sprintf("/api/pages/%d/deck/spa/", p.ID)
	resp, err := deckSPA(ctx, p.Body, deckThemeConfig(p), base, "")
	if err != nil {
		slog.Debug("deck warm failed", "page_id", pageID, "err", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		slog.Debug("deck warm non-200", "page_id", pageID, "status", resp.StatusCode)
	}
}
