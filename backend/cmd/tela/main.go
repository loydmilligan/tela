package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zcag/tela/backend/internal/api"
	"github.com/zcag/tela/backend/internal/auth"
	"github.com/zcag/tela/backend/internal/db"
	"github.com/zcag/tela/backend/internal/rag"
)

// shutdownGrace is how long http.Server.Shutdown waits for in-flight requests
// to finish before forcing close. Generous enough to let a slow import or
// search response complete; short enough that `docker stop` doesn't escalate
// to SIGKILL (default 10s grace).
const shutdownGrace = 10 * time.Second

// initLogger installs the process-wide slog default: a text handler for
// human-readable dev logs, or JSON when TELA_LOG_FORMAT=json so prod logs flow
// into Loki/Grafana as structured records. Called first thing in main().
func initLogger() {
	var h slog.Handler
	if os.Getenv("TELA_LOG_FORMAT") == "json" {
		h = slog.NewJSONHandler(os.Stderr, nil)
	} else {
		h = slog.NewTextHandler(os.Stderr, nil)
	}
	slog.SetDefault(slog.New(h))
}

// fatal logs at Error level then exits non-zero. Replaces log.Fatalf while
// keeping its boot-fatal semantics (no panic/Goexit, so deferred Close calls
// in callers are intentionally skipped — same as the old log.Fatalf).
func fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}

func main() {
	initLogger()

	addr := ":8080"
	if v := os.Getenv("TELA_ADDR"); v != "" {
		addr = v
	}

	dsn := os.Getenv("TELA_DATABASE_URL")
	if dsn == "" {
		fatal("TELA_DATABASE_URL is required (e.g. postgres://tela:pass@localhost:5432/tela?sslmode=disable)")
	}

	d, err := db.Open(dsn)
	if err != nil {
		fatal("open db", "err", err)
	}
	defer d.Close()

	if err := db.Migrate(context.Background(), d); err != nil {
		fatal("migrate db", "err", err)
	}
	slog.Info("db ready")

	// One-off CLI subcommands run after migrations, then exit (no server).
	// Headless parity for the ops runbook (docs/operations.md).
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "reindex-all":
			// Re-embed every page after an embedder model change (the chunk
			// hash folds in the model name). Add --force for a full re-embed
			// when the model name is unchanged but the embedder setup moved.
			runReindexAll(d, os.Args[2:])
			return
		case "create-admin":
			runCreateAdmin(d, os.Args[2:])
			return
		case "set-plan":
			runSetPlan(d, os.Args[2:])
			return
		case "list-users":
			runListUsers(d)
			return
		default:
			fatal("unknown subcommand (known: reindex-all, create-admin, set-plan, list-users)", "subcommand", os.Args[1])
		}
	}

	bs, err := auth.BootstrapFromEnv(context.Background(), d)
	if err != nil {
		fatal("bootstrap admin", "err", err)
	}
	if bs.Created {
		slog.Info("bootstrap admin created from TELA_ADMIN_PASSWORD env", "username", bs.Username)
	}

	// Assign TELA_ADMIN_EMAIL to a pre-email-auth bootstrap admin (no-op once
	// set, or when the env is unset).
	auth.BackfillAdminEmailFromEnv(context.Background(), d)

	// Every user gets a private, one-member personal space as their default
	// writing home (docs/visibility-model.md). Backfills the bootstrap admin
	// and any pre-existing users; non-fatal so a hiccup never blocks boot.
	if err := api.EnsurePersonalSpacesForAll(context.Background(), d); err != nil {
		slog.Error("personal space backfill", "err", err)
	}

	// rootCtx is cancelled on SIGINT/SIGTERM. Threaded into StartAuditGC so
	// the ticker loop exits cleanly on shutdown; used as the trigger for
	// HTTP and AuditWriter teardown below.
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logStartupConfig()

	handler, apiSrv := api.HandlerWithServer(d)

	// M16.A.2 API-key audit retention GC. Background goroutine sweeps
	// api_key_audit rows older than TELA_API_KEY_AUDIT_DAYS (default 30) every
	// 6h. Cancels with rootCtx so the ticker exits before AuditWriter.Close
	// runs and the process exits.
	auth.StartAuditGC(rootCtx, d)

	httpSrv := &http.Server{Addr: addr, Handler: handler}
	serverErr := make(chan error, 1)
	go func() { serverErr <- httpSrv.ListenAndServe() }()

	slog.Info("tela backend listening", "addr", addr)

	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fatal("server failed", "err", err)
		}
	case <-rootCtx.Done():
		slog.Info("shutdown signal received, draining", "grace", shutdownGrace)
		// Order matters: HTTP shutdown FIRST so in-flight bearer requests
		// finish populating the audit buffer, THEN AuditWriter.Close so
		// those last rows make it to the DB before the worker stops.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			slog.Error("http shutdown", "err", err)
		}
		apiSrv.AuditWriter().Close()
	}
}

// logStartupConfig prints the effective operational config at boot so silent
// misconfigurations become visible: the cookie-Secure ↔ TELA_PUBLIC_BASE_URL
// coupling (https base URL behind plain HTTP drops the login cookie), the SMTP
// log-fallback (registration emails go to stdout, blocking multi-user signup),
// and the RAG embedder target.
func logStartupConfig() {
	base := os.Getenv("TELA_PUBLIC_BASE_URL")
	if base == "" {
		base = "(unset; defaulting to http://localhost:8780)"
	}
	slog.Info("config", "public_base_url", base, "cookie_secure", auth.CookieSecure())

	if os.Getenv("TELA_SMTP_HOST") == "" {
		slog.Warn("config: SMTP unset — verify/reset emails are LOGGED, not sent. " +
			"Open self-registration needs SMTP to be usable; set TELA_SMTP_* for multi-user.")
	} else {
		slog.Info("config: SMTP enabled", "host", os.Getenv("TELA_SMTP_HOST"))
	}

	if rcfg := rag.ConfigFromEnv(); rcfg.EmbedURL == "" {
		slog.Warn("config: RAG/semantic search DISABLED (TELA_RAG_EMBED_URL unset). " +
			"Full-text search still works; set the URL to enable embeddings.")
	} else {
		slog.Info("config: RAG embedder", "url", rcfg.EmbedURL, "model", rcfg.EmbedModel, "dim", rcfg.Dim)
	}
}

// runReindexAll re-embeds every page in every space against the currently
// configured embedder, logging per-space progress (in the rag package), then
// returns. Synchronous; the embed calls dominate wall-clock. Pass --force to
// bypass the per-chunk vector cache and re-embed everything from scratch — the
// clean replacement for a manual TRUNCATE when the model name is unchanged but
// the embedder setup moved.
func runReindexAll(d *sql.DB, args []string) {
	force := false
	for _, a := range args {
		switch a {
		case "--force", "-f":
			force = true
		default:
			fatal("reindex-all: unknown flag (known: --force)", "flag", a)
		}
	}

	cfg := rag.ConfigFromEnv()
	if cfg.EmbedURL == "" {
		fatal("reindex-all: TELA_RAG_EMBED_URL is not set — nothing to embed against")
	}
	svc := rag.NewService(d, cfg)
	if !svc.Enabled() {
		fatal("reindex-all: embedder disabled")
	}

	sum, err := svc.ReindexAll(context.Background(), force)
	if err != nil {
		fatal("reindex-all", "err", err)
	}
	if sum.Failed > 0 {
		slog.Warn("reindex-all: completed with failures", "failed_pages", sum.Failed)
	}
}
