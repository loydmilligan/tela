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
)

// authRateLimiter is an in-memory sliding-window limiter keyed by
// (purpose, IP). Process-local; a restart resets it — fine for v0, mirrors
// shareRateLimiter so the two share the same sweep/normalize machinery.
type authRateLimiter struct {
	mu      sync.Mutex
	buckets map[string][]time.Time
}

func newAuthRateLimiter() *authRateLimiter {
	return &authRateLimiter{buckets: map[string][]time.Time{}}
}

func (rl *authRateLimiter) sweep() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := time.Now().Add(-authRateWindow)
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
	t := time.NewTicker(authRateWindow)
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

// allow returns (ok, retryAfter). purpose namespaces the bucket so a register
// and a forgot-password from the same IP don't share a budget.
func (rl *authRateLimiter) allow(purpose, ip string) (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	key := purpose + "\x00" + ip
	now := time.Now()
	cutoff := now.Add(-authRateWindow)

	times := rl.buckets[key]
	pruned := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			pruned = append(pruned, t)
		}
	}
	if len(pruned) >= authRateLimit {
		retry := pruned[0].Add(authRateWindow).Sub(now)
		if retry < 0 {
			retry = 0
		}
		rl.buckets[key] = pruned
		return false, retry
	}
	pruned = append(pruned, now)
	rl.buckets[key] = pruned
	return true, 0
}
