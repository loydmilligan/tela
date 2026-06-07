package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
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

func main() {
	addr := ":8080"
	if v := os.Getenv("TELA_ADDR"); v != "" {
		addr = v
	}

	dsn := os.Getenv("TELA_DATABASE_URL")
	if dsn == "" {
		log.Fatalf("TELA_DATABASE_URL is required (e.g. postgres://tela:pass@localhost:5432/tela?sslmode=disable)")
	}

	d, err := db.Open(dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer d.Close()

	if err := db.Migrate(context.Background(), d); err != nil {
		log.Fatalf("migrate db: %v", err)
	}
	log.Printf("db ready")

	// One-off maintenance subcommand: re-embed every page in every space, then
	// exit (no server). Used after changing the embedder model — the chunk hash
	// folds in the model name, so a reindex re-embeds everything with the new
	// model. Run in the container: `/tela reindex-all`.
	if len(os.Args) > 1 && os.Args[1] == "reindex-all" {
		runReindexAll(d)
		return
	}

	bs, err := auth.BootstrapFromEnv(context.Background(), d)
	if err != nil {
		log.Fatalf("bootstrap admin: %v", err)
	}
	if bs.Created {
		if bs.GeneratedPassword != "" {
			log.Println("==================================================================")
			log.Printf(">>> Tela bootstrap admin: %s / %s — change it in Settings.", bs.Username, bs.GeneratedPassword)
			log.Println("==================================================================")
		} else {
			log.Printf(">>> Tela bootstrap admin '%s' created from TELA_ADMIN_PASSWORD env.", bs.Username)
		}
	}

	// Assign TELA_ADMIN_EMAIL to a pre-email-auth bootstrap admin (no-op once
	// set, or when the env is unset).
	auth.BackfillAdminEmailFromEnv(context.Background(), d)

	// Every user gets a private, one-member personal space as their default
	// writing home (docs/visibility-model.md). Backfills the bootstrap admin
	// and any pre-existing users; non-fatal so a hiccup never blocks boot.
	if err := api.EnsurePersonalSpacesForAll(context.Background(), d); err != nil {
		log.Printf("personal space backfill: %v", err)
	}

	// rootCtx is cancelled on SIGINT/SIGTERM. Threaded into StartAuditGC so
	// the ticker loop exits cleanly on shutdown; used as the trigger for
	// HTTP and AuditWriter teardown below.
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	handler, apiSrv := api.HandlerWithServer(d)

	// M16.A.2 API-key audit retention GC. Background goroutine sweeps
	// api_key_audit rows older than TELA_API_KEY_AUDIT_DAYS (default 30) every
	// 6h. Cancels with rootCtx so the ticker exits before AuditWriter.Close
	// runs and the process exits.
	auth.StartAuditGC(rootCtx, d)

	httpSrv := &http.Server{Addr: addr, Handler: handler}
	serverErr := make(chan error, 1)
	go func() { serverErr <- httpSrv.ListenAndServe() }()

	log.Printf("tela backend listening on %s", addr)

	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server failed: %v", err)
		}
	case <-rootCtx.Done():
		log.Printf("shutdown signal received, draining for up to %s", shutdownGrace)
		// Order matters: HTTP shutdown FIRST so in-flight bearer requests
		// finish populating the audit buffer, THEN AuditWriter.Close so
		// those last rows make it to the DB before the worker stops.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			log.Printf("http shutdown: %v", err)
		}
		apiSrv.AuditWriter().Close()
	}
}

// runReindexAll re-embeds every page in every space against the currently
// configured embedder, logging per-space progress, then returns. The embed
// calls dominate wall-clock; it's synchronous and runs to completion.
func runReindexAll(d *sql.DB) {
	cfg := rag.ConfigFromEnv()
	if cfg.EmbedURL == "" {
		log.Fatalf("reindex-all: TELA_RAG_EMBED_URL is not set — nothing to embed against")
	}
	svc := rag.NewService(d, cfg)
	if !svc.Enabled() {
		log.Fatalf("reindex-all: embedder disabled")
	}
	ctx := context.Background()
	log.Printf("reindex-all: model=%q url=%q", cfg.EmbedModel, cfg.EmbedURL)

	type spaceRef struct {
		id   int64
		name string
	}
	rows, err := d.QueryContext(ctx, `SELECT id, name FROM spaces ORDER BY id`)
	if err != nil {
		log.Fatalf("reindex-all: list spaces: %v", err)
	}
	var spaces []spaceRef
	for rows.Next() {
		var s spaceRef
		if err := rows.Scan(&s.id, &s.name); err != nil {
			rows.Close()
			log.Fatalf("reindex-all: scan space: %v", err)
		}
		spaces = append(spaces, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Fatalf("reindex-all: iterate spaces: %v", err)
	}

	var totalPages, totalChunks int
	for i, s := range spaces {
		pages, chunks, err := svc.ReindexSpace(ctx, s.id)
		if err != nil {
			log.Fatalf("reindex-all: space %d %q: %v", s.id, s.name, err)
		}
		totalPages += pages
		totalChunks += chunks
		log.Printf("reindex-all: [%d/%d] space %d %q — %d pages, %d chunks", i+1, len(spaces), s.id, s.name, pages, chunks)
	}
	log.Printf("reindex-all: DONE — %d spaces, %d pages, %d chunks re-embedded with %q",
		len(spaces), totalPages, totalChunks, cfg.EmbedModel)
}
