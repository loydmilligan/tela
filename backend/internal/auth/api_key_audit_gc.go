package auth

import (
	"context"
	"database/sql"
	"log"
	"os"
	"strconv"
	"time"
)

// DefaultAuditRetentionDays is the default lifespan of an api_key_audit row
// before the GC sweep deletes it. Overridable per-deploy via the
// TELA_API_KEY_AUDIT_DAYS env. 30 days matches the spec — long enough to
// debug a misbehaving agent, short enough that the table doesn't grow without
// bound on a noisy deploy.
const DefaultAuditRetentionDays = 30

// auditGCInterval is how often the GC goroutine wakes up to sweep. 6h is a
// compromise: noticeably less than the retention window so a freshly
// restarted instance with an overdue sweep catches up within one cycle, but
// long enough that the recurring DELETE never shows up as a hot query in
// the DB's stats.
const auditGCInterval = 6 * time.Hour

// StartAuditGC launches a background goroutine that periodically deletes
// api_key_audit rows older than the configured retention. Cancelling ctx
// stops the loop. Sweeps once on startup so an instance that's been off for
// longer than the cycle catches up immediately.
//
// Configurable via TELA_API_KEY_AUDIT_DAYS (positive integer; falls back to
// DefaultAuditRetentionDays on absent / unparseable / non-positive values).
func StartAuditGC(ctx context.Context, d *sql.DB) {
	days := DefaultAuditRetentionDays
	if v := os.Getenv("TELA_API_KEY_AUDIT_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			days = n
		} else {
			log.Printf("auth: ignoring invalid TELA_API_KEY_AUDIT_DAYS=%q, using default %d", v, days)
		}
	}
	log.Printf("auth: api_key audit GC retention=%d days, sweep interval=%s", days, auditGCInterval)
	go func() {
		if err := purgeAuditOlderThan(ctx, d, days); err != nil {
			log.Printf("auth: api_key audit GC initial sweep failed: %v", err)
		}
		t := time.NewTicker(auditGCInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := purgeAuditOlderThan(ctx, d, days); err != nil {
					log.Printf("auth: api_key audit GC sweep failed: %v", err)
				}
			}
		}
	}()
}

// purgeAuditOlderThan deletes api_key_audit rows whose ts is older than the
// retention cutoff. Single statement; no transaction needed.
//
// The cutoff is computed in SQL as (now - days) rendered into the same
// 'YYYY-MM-DD HH:MM:SS' UTC text format ts is stored in, so the comparison
// stays a lexicographic TEXT compare. days binds directly as $1 into
// make_interval(days => $1) (an int4 — the Go value is an int, fine).
func purgeAuditOlderThan(ctx context.Context, d *sql.DB, days int) error {
	if days <= 0 {
		return nil
	}
	_, err := d.ExecContext(ctx,
		`DELETE FROM api_key_audit WHERE ts < to_char((now() AT TIME ZONE 'UTC') - make_interval(days => $1), 'YYYY-MM-DD HH24:MI:SS')`,
		days)
	return err
}
