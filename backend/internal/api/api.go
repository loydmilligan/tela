package api

import (
	"context"
	"database/sql"

	"github.com/zcag/tela/backend/internal/auth"
)

// Server bundles dependencies shared across HTTP handlers.
type Server struct {
	DB           *sql.DB
	rooms        *roomRegistry
	shareSecret  []byte
	shareLimiter *shareRateLimiter

	// auditWriter buffers bearer-authed audit-log writes. M16.A.2: every
	// request that resolved to a valid api_keys row is recorded here
	// (method, path, status_code, ts). Lives on Server because the
	// /api/api_keys/{id}/audit handler queries the rows it produces, and
	// because tests need a stable handle to call Flush() between submit and
	// assert. Owned by New() so it always exists — never nil — which keeps
	// the dependency injection trivial.
	auditWriter *auth.AuditWriter
}

func New(db *sql.DB) *Server {
	s := &Server{
		DB:           db,
		rooms:        newRoomRegistry(),
		shareSecret:  loadOrGenerateShareSecret(),
		shareLimiter: newShareRateLimiter(),
		auditWriter:  auth.NewAuditWriter(db),
	}
	// Sweep stale share-rate-limit buckets every shareRateWindow so the
	// limiter map cannot grow unbounded under adversarial load. Tied to
	// context.Background() — the goroutine outlives non-graceful tests, which
	// is fine: each test process is short-lived.
	go s.shareLimiter.sweepLoop(context.Background())
	return s
}

// AuditWriter exposes the server's audit-log sink so tests can call Flush()
// between a bearer-authed request and a "did the audit row land?" assertion.
// Production code never needs this — Middleware Submits directly.
func (s *Server) AuditWriter() *auth.AuditWriter {
	return s.auditWriter
}
