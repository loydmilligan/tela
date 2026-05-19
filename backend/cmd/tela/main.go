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

	srv := api.New(d)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", srv.Health)

	mux.HandleFunc("POST /api/auth/login", srv.Login)
	mux.HandleFunc("POST /api/auth/logout", srv.Logout)
	mux.HandleFunc("GET /api/auth/me", srv.Me)

	mux.HandleFunc("GET /api/spaces", srv.ListSpaces)
	mux.HandleFunc("POST /api/spaces", srv.CreateSpace)
	mux.HandleFunc("GET /api/spaces/{id}", srv.GetSpace)
	mux.HandleFunc("PATCH /api/spaces/{id}", srv.UpdateSpace)
	mux.HandleFunc("DELETE /api/spaces/{id}", srv.DeleteSpace)

	mux.HandleFunc("GET /api/pages", srv.ListPages)
	mux.HandleFunc("GET /api/pages/all", srv.ListAllPages)
	mux.HandleFunc("POST /api/pages", srv.CreatePage)
	mux.HandleFunc("GET /api/pages/{id}", srv.GetPage)
	mux.HandleFunc("PATCH /api/pages/{id}", srv.UpdatePage)
	mux.HandleFunc("DELETE /api/pages/{id}", srv.DeletePage)
	mux.HandleFunc("POST /api/pages/{id}/move", srv.MovePage)
	mux.HandleFunc("GET /api/pages/{id}/backlinks", srv.Backlinks)

	mux.HandleFunc("GET /api/search", srv.Search)

	handler := auth.Middleware(d)(mux)

	log.Printf("tela backend listening on %s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
