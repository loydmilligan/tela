package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/zcag/tela/backend/internal/api"
	"github.com/zcag/tela/backend/internal/auth"
	"github.com/zcag/tela/backend/internal/db"
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

	dbPath := "/data/tela.db"
	if v := os.Getenv("TELA_DB_PATH"); v != "" {
		dbPath = v
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		log.Fatalf("create db dir: %v", err)
	}

	d, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer d.Close()

	if err := db.Migrate(context.Background(), d); err != nil {
		log.Fatalf("migrate db: %v", err)
	}
	log.Printf("db ready at %s", dbPath)

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
