package main

import (
	"log"
	"net/http"
	"os"

	"github.com/zcag/tela/backend/internal/api"
)

func main() {
	addr := ":8080"
	if v := os.Getenv("TELA_ADDR"); v != "" {
		addr = v
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", api.Health)

	log.Printf("tela backend listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
