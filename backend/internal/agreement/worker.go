package agreement

import (
	"context"
	"log/slog"
	"time"
)

// Debounced, coalescing background worker — the exact shape as summarize and
// rag/autoreindex. Handlers call Queue(pageID) after a committed write (next to
// rag.QueueReindex / summarize.Queue); the worker computes each page's agreement
// once its edits settle. A failed compute backs off exponentially; an independent
// stale sweep re-queues pages whose result is missing, failed, or computed from a
// now-changed body (recovering LLM outages and edits made while down).

var (
	agreeDebounce     = 6 * time.Second
	agreeTick         = 1 * time.Second
	agreeTimeout      = 2 * time.Minute
	agreeRetryBase    = 30 * time.Second
	agreeRetryMax     = 10 * time.Minute
	sweepInterval     = 10 * time.Minute
	sweepInitialDelay = 45 * time.Second
	sweepBatch        = 300
)

// Queue schedules pageID after the debounce window. Safe on every write; repeated
// calls coalesce. No-op when disabled or the worker isn't running.
func (s *Service) Queue(pageID int64) {
	if !s.Enabled() {
		return
	}
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	if s.pending == nil {
		return
	}
	s.pending[pageID] = time.Now().Add(agreeDebounce)
	delete(s.attempts, pageID)
}

// Start launches the worker + stale-sweep loops. Idempotent; no-op when disabled.
// Call once from api.New.
func (s *Service) Start(ctx context.Context) {
	if !s.Enabled() {
		return
	}
	s.queueMu.Lock()
	if s.pending != nil {
		s.queueMu.Unlock()
		return
	}
	s.pending = make(map[int64]time.Time)
	s.attempts = make(map[int64]int)
	s.queueMu.Unlock()
	go s.agreeLoop(ctx)
	go s.staleSweepLoop(ctx)
}

func (s *Service) agreeLoop(ctx context.Context) {
	t := time.NewTicker(agreeTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, id := range s.due() {
				cctx, cancel := context.WithTimeout(ctx, agreeTimeout)
				err := s.AgreePage(cctx, id, false)
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

func (s *Service) requeueAfterFailure(pageID int64) {
	s.queueMu.Lock()
	if s.pending == nil {
		s.queueMu.Unlock()
		return
	}
	s.attempts[pageID]++
	n := s.attempts[pageID]
	shift := n - 1
	if shift > 16 {
		shift = 16
	}
	backoff := agreeRetryBase << uint(shift)
	if backoff > agreeRetryMax || backoff <= 0 {
		backoff = agreeRetryMax
	}
	s.pending[pageID] = time.Now().Add(backoff)
	s.queueMu.Unlock()
	slog.Warn("agreement: compute failed, will retry", "page_id", pageID, "attempt", n, "retry_in", backoff)
}

func (s *Service) clearAttempts(pageID int64) {
	s.queueMu.Lock()
	delete(s.attempts, pageID)
	s.queueMu.Unlock()
}

// due removes and returns the page IDs whose debounce window has elapsed.
func (s *Service) due() []int64 {
	now := time.Now()
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	var ready []int64
	for id, deadline := range s.pending {
		if !deadline.After(now) {
			ready = append(ready, id)
			delete(s.pending, id)
		}
	}
	return ready
}

func (s *Service) staleSweepLoop(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(sweepInitialDelay):
	}
	s.sweepStale(ctx) // initial backfill right after the boot delay, not a full interval later
	t := time.NewTicker(sweepInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.sweepStale(ctx)
		}
	}
}

// sweepStale re-queues pages whose agreement is missing, failed, or computed from
// a body that has since changed. (Neighbour-only drift isn't detected by the body
// hash; the periodic sweep is the backstop that eventually catches it.)
func (s *Service) sweepStale(ctx context.Context) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id
		  FROM pages p
		  LEFT JOIN page_agreement a ON a.page_id = p.id
		 WHERE p.deleted_at IS NULL AND length(btrim(p.body)) > 0
		   AND (a.page_id IS NULL OR a.last_error <> ''
		        OR a.src_hash <> encode(sha256(convert_to($1 || p.body, 'UTF8')), 'hex'))
		 ORDER BY p.updated_at DESC
		 LIMIT $2`, hashSeed, sweepBatch)
	if err != nil {
		slog.Warn("agreement: stale sweep query failed", "err", err)
		return
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return
		}
		ids = append(ids, id)
	}
	for _, id := range ids {
		s.Queue(id)
	}
}
