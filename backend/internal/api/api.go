package api

import (
	"context"
	"database/sql"
	"log/slog"
	"os"

	"github.com/zcag/tela/backend/internal/agreement"
	"github.com/zcag/tela/backend/internal/auth"
	"github.com/zcag/tela/backend/internal/llm"
	"github.com/zcag/tela/backend/internal/mailer"
	"github.com/zcag/tela/backend/internal/rag"
	"github.com/zcag/tela/backend/internal/settings"
	"github.com/zcag/tela/backend/internal/summarize"
)

// Server bundles dependencies shared across HTTP handlers.
type Server struct {
	DB *sql.DB

	// settings is the instance-level runtime config store (instance_settings
	// table, cached). It backs the admin settings API, persisted secrets, and
	// (later) per-plan feature flags + the cloud-connect token. Never nil —
	// built in New().
	settings *settings.Store

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

	// cloudLimiter throttles the managed-compute proxies (cloud embed/chat,
	// ask-your-docs) per ACCOUNT so a single entitled PAT can't hammer paid
	// LLM/embedder compute into an unbounded bill or DoS the shared clients.
	cloudLimiter *authRateLimiter

	// davDeletes is the WebDAV mass-delete brake (sync §6): a per-(api_key,space)
	// sliding window that refuses a sync client's delete once an anomalous
	// fraction of the space has already vanished, so a runaway client can't wipe a
	// space. Pairs with the per-page cursor gate in the davFS RemoveAll path.
	davDeletes *davDeleteGuard

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

	// llm is the chat-completion service (OpenAI-compatible / Ollama). Sibling
	// to rag: constructed from env (TELA_LLM_URL), disabled — but never nil —
	// when unconfigured, so handlers can `if !s.llm.Enabled()` → 503. Consumed
	// in-process by /api/rag/ask and over HTTP via the managed cloud proxy.
	// Tests inject a fake completer by overwriting this field.
	llm *llm.Service

	// summarize is the auto-summary service (LLM-generated props.summary +
	// page_summaries bookkeeping). Built on llm, so it shares its enablement:
	// disabled — but never nil — when TELA_LLM_URL is unset. Page writes call
	// s.summarize.Queue alongside s.rag.QueueReindex. Tests inject a service
	// with a fake completer by overwriting this field.
	summarize *summarize.Service

	// agreement is the corroboration/contradiction service for the epistemic
	// trust strip (page_agreement bookkeeping). Built on llm + rag, so it shares
	// their enablement: disabled — but never nil — unless BOTH a chat model and
	// the embedder are configured. Page writes call s.agreement.Queue.
	agreement *agreement.Service

	// oauth is the MCP endpoint's OAuth 2.1 Resource-Server config (WorkOS JWT
	// acceptance + Protected Resource Metadata). nil = disabled (PAT-only),
	// unless TELA_WORKOS_ISSUER is set. Tests inject a configured one.
	oauth *mcpOAuth

	// sso is the federated-login registry (social providers from TELA_SSO_*).
	// Never nil — an unconfigured instance just has an empty provider map, so
	// the login screen shows no social buttons. Per-org OIDC connections aren't
	// held here; they're built per-request from the org_sso row.
	sso *ssoRegistry

	// deckWarm pre-builds a deck's interactive SPA after its source changes so the
	// next "Present" opens instantly. Never nil — built in New(). Best-effort and
	// correctness-independent (renders are content-keyed); see deck_warm.go.
	deckWarm *deckWarmer

	// seedWelcome controls whether POST /api/spaces seeds a starter "Welcome"
	// page into a freshly created space (so a new team doesn't land in an empty
	// void). On by default; the test package disables it via
	// TELA_DISABLE_WELCOME_SEED so space-creation tests keep asserting on exact
	// page sets.
	seedWelcome bool
}

func New(db *sql.DB) *Server {
	ctx := context.Background()
	st, err := settings.New(ctx, db)
	if err != nil {
		// Boot-fatal: can't run without instance settings.
		slog.Error("settings: load instance_settings", "err", err)
		os.Exit(1)
	}
	// Resolve the api-key HMAC secret through the store (env → persisted →
	// generated-and-persisted) so it survives restarts. Must run before any
	// bearer request; New() is called once at boot.
	if err := auth.InitAPIKeySecret(ctx, st); err != nil {
		// Boot-fatal: a stable api-key secret is required.
		slog.Error("settings: init api-key secret", "err", err)
		os.Exit(1)
	}
	s := &Server{
		DB:           db,
		settings:     st,
		rooms:        newRoomRegistry(),
		shareSecret:  resolveShareSecret(ctx, st),
		shareLimiter: newShareRateLimiter(),
		auditWriter:  auth.NewAuditWriter(db),
		Mailer:       mailer.FromEnv(),
		authLimiter:  newAuthRateLimiter(authRateWindow, authRateLimit),
		cloudLimiter: newAuthRateLimiter(cloudRateWindow, cloudRateLimit),
		davDeletes:   newDavDeleteGuard(),
		rag:          rag.NewService(db, rag.ConfigFromEnv()),
		llm:          llm.NewService(llm.ConfigFromEnv()),
		oauth:        loadMCPOAuth(context.Background()),
		sso:          loadSSOProviders(context.Background()),
		seedWelcome:  os.Getenv("TELA_DISABLE_WELCOME_SEED") == "",
	}
	// Built after the literal so it can share the llm handle (same enablement).
	s.summarize = summarize.NewService(db, s.llm)
	// Agreement shares llm + rag (needs both: a model to judge, embeddings to
	// find neighbours). Page writes call s.agreement.Queue alongside summarize.
	s.agreement = agreement.NewService(db, s.llm, s.rag)
	// Pre-warms a deck's Present build after any source change (all write paths).
	s.deckWarm = newDeckWarmer(s)
	// Sweep stale share-rate-limit buckets every shareRateWindow so the
	// limiter map cannot grow unbounded under adversarial load. Tied to
	// context.Background() — the goroutine outlives non-graceful tests, which
	// is fine: each test process is short-lived.
	go s.shareLimiter.sweepLoop(context.Background())
	go s.authLimiter.sweepLoop(context.Background())
	go s.cloudLimiter.sweepLoop(context.Background())
	go s.davDeletes.sweepLoop(context.Background())
	// Background auto-reindex worker (no-op when the embedder is unconfigured).
	// Page writes call s.rag.QueueReindex; this drains the debounced queue.
	s.rag.StartAutoReindex(context.Background())
	// Background auto-summarize worker, the generation sibling (no-op when the
	// LLM is unconfigured). Page writes call s.summarize.Queue.
	s.summarize.Start(context.Background())
	// Background agreement worker (no-op unless both llm + embedder are on).
	s.agreement.Start(context.Background())
	return s
}

// AuditWriter exposes the server's audit-log sink so tests can call Flush()
// between a bearer-authed request and a "did the audit row land?" assertion.
// Production code never needs this — Middleware Submits directly.
func (s *Server) AuditWriter() *auth.AuditWriter {
	return s.auditWriter
}
