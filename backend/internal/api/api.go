package api

import (
	"context"
	"database/sql"
)

// Server bundles dependencies shared across HTTP handlers.
type Server struct {
	DB           *sql.DB
	rooms        *roomRegistry
	shareSecret  []byte
	shareLimiter *shareRateLimiter
}

func New(db *sql.DB) *Server {
	s := &Server{
		DB:           db,
		rooms:        newRoomRegistry(),
		shareSecret:  loadOrGenerateShareSecret(),
		shareLimiter: newShareRateLimiter(),
	}
	// Sweep stale share-rate-limit buckets every shareRateWindow so the
	// limiter map cannot grow unbounded under adversarial load. Tied to
	// context.Background() — the goroutine outlives non-graceful tests, which
	// is fine: each test process is short-lived.
	go s.shareLimiter.sweepLoop(context.Background())
	return s
}
