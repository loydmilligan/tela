package api

import (
	"context"
	"os"
	"strconv"
	"sync"
	"time"
)

// dav_delete_guard.go — the mass-delete brake for the WebDAV sync surface (sync
// §6). A runaway or misconfigured client (e.g. a wiped local vault that syncs
// "everything is gone") must not be able to soft-delete a whole space in one go.
// This in-memory sliding window refuses a client's delete once an anomalous
// fraction of the space has already vanished in the window. Deletes are soft
// (recoverable), so this is a brake, not a lock — and it pairs with the per-page
// cursor gate (hasSyncBase) and the client-side rclone --max-delete.

const (
	davDeleteWindow          = 10 * time.Minute
	davDeleteFloorDefault    = 20  // always allow at least this many per window (small spaces unaffected)
	davDeleteFractionDefault = 0.5 // beyond the floor, trip at ~this share of the space gone
)

// davDeleteGuard is a process-local sliding-window counter keyed by
// (api_key, space). Mirrors authRateLimiter's sweep/prune machinery; a restart
// resets it (fine — a window is minutes and deletes are recoverable).
type davDeleteGuard struct {
	mu       sync.Mutex
	buckets  map[string][]time.Time
	floor    int64
	fraction float64
}

func newDavDeleteGuard() *davDeleteGuard {
	floor := int64(davDeleteFloorDefault)
	if v := os.Getenv("TELA_WEBDAV_DELETE_FLOOR"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			floor = n
		}
	}
	fraction := davDeleteFractionDefault
	if v := os.Getenv("TELA_WEBDAV_DELETE_FRACTION"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && f <= 1 {
			fraction = f
		}
	}
	return &davDeleteGuard{buckets: map[string][]time.Time{}, floor: floor, fraction: fraction}
}

func (g *davDeleteGuard) sweep() {
	g.mu.Lock()
	defer g.mu.Unlock()
	cutoff := time.Now().Add(-davDeleteWindow)
	for k, times := range g.buckets {
		kept := times[:0]
		for _, t := range times {
			if t.After(cutoff) {
				kept = append(kept, t)
			}
		}
		if len(kept) == 0 {
			delete(g.buckets, k)
		} else {
			g.buckets[k] = kept
		}
	}
}

func (g *davDeleteGuard) sweepLoop(ctx context.Context) {
	t := time.NewTicker(davDeleteWindow)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			g.sweep()
		}
	}
}

// allow reports whether one more delete is permitted for (apiKeyID, spaceID),
// given the space currently holds liveCount live pages, and records it when so.
// The limit is max(floor, fraction × baseline), where baseline ≈ the live count
// at the window's start (current live + already-deleted-this-window) — so a
// small space is never blocked (floor) while a large one trips at ~half gone.
func (g *davDeleteGuard) allow(apiKeyID, spaceID, liveCount int64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	key := strconv.FormatInt(apiKeyID, 10) + "\x00" + strconv.FormatInt(spaceID, 10)
	now := time.Now()
	cutoff := now.Add(-davDeleteWindow)

	pruned := g.buckets[key][:0]
	for _, t := range g.buckets[key] {
		if t.After(cutoff) {
			pruned = append(pruned, t)
		}
	}
	done := int64(len(pruned))
	baseline := liveCount + done
	limit := g.floor
	if f := int64(g.fraction * float64(baseline)); f > limit {
		limit = f
	}
	if done >= limit {
		g.buckets[key] = pruned
		return false
	}
	g.buckets[key] = append(pruned, now)
	return true
}
