package api

import (
	"context"
	"database/sql"

	"github.com/zcag/tela/backend/internal/auth"
	"github.com/zcag/tela/backend/internal/mailer"
	"github.com/zcag/tela/backend/internal/rag"
)

// Server bundles dependencies shared across HTTP handlers.
type Server struct {
	DB           *sql.DB
	rooms        *roomRegistry
	shareSecret  []byte
	shareLimiter *shareRateLimiter

	// Mailer sends transactional email (verify / reset). Defaults to the
	// env-selected driver (SMTP, or a log fallback when unconfigured) so it is
	// never nil; tests overwrite it with a capturing fake after New().
	Mailer mailer.Mailer

	// authLimiter throttles the unauthenticated, email-sending endpoints
	// (register / resend / forgot-password) per client IP so the relay can't
	// be turned into a mail bomb.
	authLimiter *authRateLimiter

	// auditWriter buffers bearer-authed audit-log writes. M16.A.2: every
	// request that resolved to a valid api_keys row is recorded here
	// (method, path, status_code, ts). Lives on Server because the
	// /api/api_keys/{id}/audit handler queries the rows it produces, and
	// because tests need a stable handle to call Flush() between submit and
	// assert. Owned by New() so it always exists — never nil — which keeps
	// the dependency injection trivial.
	auditWriter *auth.AuditWriter

	// rag is the semantic-retrieval service (chunk index + hybrid search).
	// Constructed from env (TELA_RAG_EMBED_URL); disabled — but never nil —
	// when unconfigured, so handlers can `if !s.rag.Enabled()` → 503. Tests
	// inject a fake embedder by overwriting this field.
	rag *rag.Service

	// oauth is the MCP endpoint's OAuth 2.1 Resource-Server config (WorkOS JWT
	// acceptance + Protected Resource Metadata). nil = disabled (PAT-only),
	// unless TELA_WORKOS_ISSUER is set. Tests inject a configured one.
	oauth *mcpOAuth
}

func New(db *sql.DB) *Server {
	s := &Server{
		DB:           db,
		rooms:        newRoomRegistry(),
		shareSecret:  loadOrGenerateShareSecret(),
		shareLimiter: newShareRateLimiter(),
		auditWriter:  auth.NewAuditWriter(db),
		Mailer:       mailer.FromEnv(),
		authLimiter:  newAuthRateLimiter(),
		rag:          rag.NewService(db, rag.ConfigFromEnv()),
		oauth:        loadMCPOAuth(context.Background()),
	}
	// Sweep stale share-rate-limit buckets every shareRateWindow so the
	// limiter map cannot grow unbounded under adversarial load. Tied to
	// context.Background() — the goroutine outlives non-graceful tests, which
	// is fine: each test process is short-lived.
	go s.shareLimiter.sweepLoop(context.Background())
	go s.authLimiter.sweepLoop(context.Background())
	// Background auto-reindex worker (no-op when the embedder is unconfigured).
	// Page writes call s.rag.QueueReindex; this drains the debounced queue.
	s.rag.StartAutoReindex(context.Background())
	return s
}

// AuditWriter exposes the server's audit-log sink so tests can call Flush()
// between a bearer-authed request and a "did the audit row land?" assertion.
// Production code never needs this — Middleware Submits directly.
func (s *Server) AuditWriter() *auth.AuditWriter {
	return s.auditWriter
}
