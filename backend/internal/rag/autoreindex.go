package rag

import (
	"context"
	"log"
	"time"
)

// Auto-reindex: a debounced, coalescing background worker that keeps page_chunks
// fresh as pages are written, WITHOUT blocking the write path (embedding is a
// network op to the embedder). Handlers call QueueReindex(pageID) after a
// committed write; the worker reindexes each page once its edits settle, so a
// burst of saves on one page collapses to a single reindex.
//
// Best-effort by design: the index is a disposable cache, so a lost reindex
// (process exit, transient embed failure) just leaves the page stale until the
// next edit or a manual reindex — surfaced by the freshness API, never a hard
// error on the user's save.

// Tunable (var, not const) so tests can shrink the windows. Production values:
var (
	// reindexDebounce is how long after the last edit to a page we wait before
	// reindexing it — long enough to coalesce an active editing burst.
	reindexDebounce = 3 * time.Second
	// reindexTick is how often the worker checks for pages whose debounce elapsed.
	reindexTick = 1 * time.Second
	// reindexTimeout caps a single page's reindex (chunk + embed round-trips).
	reindexTimeout = 2 * time.Minute
)

// QueueReindex schedules pageID to be reindexed after the debounce window. Safe
// to call on every write; repeated calls for the same page coalesce and push the
// deadline forward. No-op when the embedder is unconfigured or the worker isn't
// running.
func (s *Service) QueueReindex(pageID int64) {
	if !s.Enabled() {
		return
	}
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	if s.pending == nil {
		return // worker not started
	}
	s.pending[pageID] = time.Now().Add(reindexDebounce)
}

// StartAutoReindex launches the background reindex worker. Idempotent; no-op when
// disabled. The worker stops when ctx is cancelled. Call once from api.New.
func (s *Service) StartAutoReindex(ctx context.Context) {
	if !s.Enabled() {
		return
	}
	s.queueMu.Lock()
	if s.pending != nil {
		s.queueMu.Unlock()
		return // already started
	}
	s.pending = make(map[int64]time.Time)
	s.queueMu.Unlock()
	go s.reindexLoop(ctx)
}

func (s *Service) reindexLoop(ctx context.Context) {
	t := time.NewTicker(reindexTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, id := range s.dueReindexes() {
				rctx, cancel := context.WithTimeout(ctx, reindexTimeout)
				if _, err := s.ReindexPage(rctx, id); err != nil {
					log.Printf("rag: auto-reindex page %d: %v", id, err)
				}
				cancel()
			}
		}
	}
}

// dueReindexes removes and returns the page IDs whose debounce window has
// elapsed. Pages still settling stay queued.
func (s *Service) dueReindexes() []int64 {
	now := time.Now()
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	var due []int64
	for id, deadline := range s.pending {
		if !now.Before(deadline) {
			due = append(due, id)
			delete(s.pending, id)
		}
	}
	return due
}
