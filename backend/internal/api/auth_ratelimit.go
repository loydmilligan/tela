package api

import (
	"context"
	"sync"
	"time"
)

// Email-sending auth endpoints (register / resend-verification /
// forgot-password) are unauthenticated and trigger a relay send, so they are
// throttled per client IP to keep tela from being used as a mail bomb. The
// window/limit are deliberately loose enough for honest retries.
const (
	authRateWindow = 15 * time.Minute
	authRateLimit  = 6

	// Managed-compute proxies (cloud embed/chat, ask-your-docs) are keyed per
	// ACCOUNT, not per IP, and need a far more generous budget than the email
	// endpoints — but still bounded so a single entitled PAT can't hammer paid
	// LLM/embedder compute into an unbounded bill / DoS.
	cloudRateWindow = 1 * time.Minute
	cloudRateLimit  = 60
)

// authRateLimiter is an in-memory sliding-window limiter keyed by
// (purpose, key). Process-local; a restart resets it — fine for v0, mirrors
// shareRateLimiter so the two share the same sweep/normalize machinery. The
// window/limit are per-instance so the same machinery backs both the per-IP
// email throttle and the per-account compute throttle.
type authRateLimiter struct {
	mu      sync.Mutex
	buckets map[string][]time.Time
	window  time.Duration
	limit   int
}

func newAuthRateLimiter(window time.Duration, limit int) *authRateLimiter {
	return &authRateLimiter{buckets: map[string][]time.Time{}, window: window, limit: limit}
}

func (rl *authRateLimiter) sweep() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := time.Now().Add(-rl.window)
	for k, times := range rl.buckets {
		kept := times[:0]
		for _, t := range times {
			if t.After(cutoff) {
				kept = append(kept, t)
			}
		}
		if len(kept) == 0 {
			delete(rl.buckets, k)
		} else {
			rl.buckets[k] = kept
		}
	}
}

func (rl *authRateLimiter) sweepLoop(ctx context.Context) {
	t := time.NewTicker(rl.window)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			rl.sweep()
		}
	}
}

// allow returns (ok, retryAfter). purpose namespaces the bucket so two callers
// keyed on the same value (IP or account) don't share a budget.
func (rl *authRateLimiter) allow(purpose, key string) (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	bk := purpose + "\x00" + key
	now := time.Now()
	cutoff := now.Add(-rl.window)

	times := rl.buckets[bk]
	pruned := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			pruned = append(pruned, t)
		}
	}
	if len(pruned) >= rl.limit {
		retry := pruned[0].Add(rl.window).Sub(now)
		if retry < 0 {
			retry = 0
		}
		rl.buckets[bk] = pruned
		return false, retry
	}
	pruned = append(pruned, now)
	rl.buckets[bk] = pruned
	return true, 0
}
