package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/zcag/tela/backend/internal/api"
	"github.com/zcag/tela/backend/internal/auth"
	"github.com/zcag/tela/backend/internal/db"
)

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

	handler := api.Handler(d)

	// M16.A.2 API-key audit retention GC. Background goroutine sweeps
	// api_key_audit rows older than TELA_API_KEY_AUDIT_DAYS (default 30) every
	// 6h. Tied to context.Background() — the process exits before the loop
	// cares about cancellation, and we accept the small leak (the goroutine
	// is bounded by process lifetime).
	auth.StartAuditGC(context.Background(), d)

	log.Printf("tela backend listening on %s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
