package api

import (
	"encoding/json"
	"net/http"
	"time"
)

// Version metadata injected at build time via:
//
//	-ldflags "-X github.com/zcag/tela/backend/internal/api.Version=...
//	          -X github.com/zcag/tela/backend/internal/api.Commit=...
//	          -X github.com/zcag/tela/backend/internal/api.BuildTime=..."
//
// Defaults preserve a usable dev/unknown signal when the binary is built
// without ldflags (e.g. `go run ./cmd/tela`, `go test`).
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = ""
)

// processStart is the fallback for BuildTime when no ldflag was supplied.
// Captured once at process start so /api/version returns a stable value
// across requests rather than `time.Now()` on every call.
var processStart = time.Now().UTC().Format(time.RFC3339)

// VersionInfo is the wire shape of GET /api/version. Used by the MCP server's
// startup compat check (M16.B.1) to decide whether to print a "backend newer
// than built-against" warning to stderr.
type VersionInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	BuiltAt string `json:"built_at"`
}

// Version handles GET /api/version. Public (see auth.IsPublicPath) — mirrors
// /api/health: no session, no bearer, no membership.
func (s *Server) Version(w http.ResponseWriter, r *http.Request) {
	bt := BuildTime
	if bt == "" {
		bt = processStart
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(VersionInfo{
		Version: Version,
		Commit:  Commit,
		BuiltAt: bt,
	})
}
