package rag

import (
	"context"
	"log/slog"
	"time"
)

// Auto-reindex: a debounced, coalescing background worker that keeps page_chunks
// fresh as pages are written, WITHOUT blocking the write path (embedding is a
// network op to the embedder). Handlers call QueueReindex(pageID) after a
// committed write; the worker reindexes each page once its edits settle, so a
// burst of saves on one page collapses to a single reindex.
//
// Self-healing by design: the index is a disposable cache. A reindex that fails
// (embedder down, transient network error) is RE-ENQUEUED with exponential
// backoff instead of dropped, and an independent stale-sweep periodically scans
// the whole corpus for pages whose index is missing or out of date and re-queues
// them. So an embedder outage degrades to "stale until the embedder returns and
// the next sweep/retry fires", never a permanent silent gap, and never a hard
// error on the user's save. The sweep also recovers edits made while the process
// was down (the in-memory queue doesn't survive a restart, the corpus does).

// Tunable (var, not const) so tests can shrink the windows. Production values:
var (
	// reindexDebounce is how long after the last edit to a page we wait before
	// reindexing it — long enough to coalesce an active editing burst.
	reindexDebounce = 3 * time.Second
	// reindexTick is how often the worker checks for pages whose debounce elapsed.
	reindexTick = 1 * time.Second
	// reindexTimeout caps a single page's reindex (chunk + embed round-trips).
	reindexTimeout = 2 * time.Minute
	// reindexRetryBase / reindexRetryMax bound the exponential backoff applied to
	// a page whose reindex just failed: base * 2^(attempts-1), capped at max. A
	// failing embedder thus retries roughly every 30s → 1m → 2m → … up to 10m,
	// rather than hot-looping or giving up.
	reindexRetryBase = 30 * time.Second
	reindexRetryMax  = 10 * time.Minute
	// staleSweepInterval is how often the background sweep re-queues stale/unindexed
	// pages. staleSweepInitialDelay defers the first sweep past boot so a fresh
	// process isn't hammered. staleSweepBatch caps how many pages one sweep enqueues.
	staleSweepInterval     = 5 * time.Minute
	staleSweepInitialDelay = 30 * time.Second
	staleSweepBatch        = 500
)

// QueueReindex schedules pageID to be reindexed after the debounce window. Safe
// to call on every write; repeated calls for the same page coalesce and push the
// deadline forward. A fresh edit clears any accumulated retry backoff — new
// content deserves a prompt attempt. No-op when the embedder is unconfigured or
// the worker isn't running.
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
	delete(s.attempts, pageID)
}

// StartAutoReindex launches the background reindex worker plus the stale-sweep
// loop. Idempotent; no-op when disabled. Both stop when ctx is cancelled. Call
// once from api.New.
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
	s.attempts = make(map[int64]int)
	s.queueMu.Unlock()
	go s.reindexLoop(ctx)
	go s.staleSweepLoop(ctx)
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
				_, err := s.ReindexPage(rctx, id)
				cancel()
				if err != nil {
					s.requeueAfterFailure(id)
				} else {
					s.clearAttempts(id)
				}
			}
		}
	}
}

// requeueAfterFailure re-enqueues a page whose reindex failed, with exponential
// backoff keyed on its consecutive-failure count, and logs the failure with the
// computed next-retry delay so an outage is visible without being noisy per-tick.
func (s *Service) requeueAfterFailure(pageID int64) {
	s.queueMu.Lock()
	if s.pending == nil { // worker stopped
		s.queueMu.Unlock()
		return
	}
	s.attempts[pageID]++
	n := s.attempts[pageID]
	backoff := reindexRetryBase << (n - 1)
	if backoff > reindexRetryMax || backoff <= 0 { // also guards shift overflow
		backoff = reindexRetryMax
	}
	s.pending[pageID] = time.Now().Add(backoff)
	s.queueMu.Unlock()
	slog.Warn("rag: auto-reindex failed, will retry", "page_id", pageID, "attempt", n, "retry_in", backoff)
}

func (s *Service) clearAttempts(pageID int64) {
	s.queueMu.Lock()
	delete(s.attempts, pageID)
	s.queueMu.Unlock()
}

// dueReindexes removes and returns the page IDs whose debounce window has
// elapsed. Pages still settling (or backing off after a failure) stay queued.
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

// staleSweepLoop periodically re-queues every page whose index is missing or out
// of date — the safety net that heals a stale backlog after an embedder outage
// or a process restart (which loses the in-memory queue but not the corpus). It
// also logs an index-health summary each cycle so the corpus's freshness is
// observable in the logs (scrapeable by the ops stack) without anyone querying
// the freshness API.
func (s *Service) staleSweepLoop(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(staleSweepInitialDelay):
	}
	t := time.NewTicker(staleSweepInterval)
	defer t.Stop()
	for {
		s.sweepStale(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// sweepStale enqueues up to staleSweepBatch stale/unindexed pages and logs a
// health line. Best-effort: query errors are logged, never fatal.
func (s *Service) sweepStale(ctx context.Context) {
	h, err := s.IndexHealth(ctx)
	if err != nil {
		slog.Error("rag: index-health query", "err", err)
		return
	}
	slog.Info("rag: index health",
		"content_pages", h.ContentPages, "indexed_pages", h.IndexedPages,
		"stale_pages", h.StalePages, "chunks", h.Chunks, "model_drift_chunks", h.ModelDriftChunks)
	if h.StalePages == 0 {
		return
	}
	ids, err := s.stalePageIDs(ctx, staleSweepBatch)
	if err != nil {
		slog.Error("rag: stale-sweep query", "err", err)
		return
	}
	for _, id := range ids {
		s.QueueReindex(id)
	}
	slog.Info("rag: stale-sweep enqueued", "pages", len(ids), "stale_total", h.StalePages)
}
