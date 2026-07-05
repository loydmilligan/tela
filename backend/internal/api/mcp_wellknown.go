package api

import (
	"encoding/json"
	"net/http"
)

// ServeMCPManifest serves the machine-discovery manifest at /.well-known/mcp.json
// (advertised in the README + the registry server.json). It mirrors the MCP
// registry server.schema shape but injects THIS instance's origin into the
// remote so a self-hoster's manifest points at their own /api/mcp, not the
// cloud. Public + static (on auth.IsPublicPath); a discovery client fetches it
// unauthenticated, cross-origin, so it sets a permissive CORS header like
// ServePRM. Must be routed through Caddy in prod (new top-level path).
func (s *Server) ServeMCPManifest(w http.ResponseWriter, r *http.Request) {
	origin := s.linkOrigin(r)
	manifest := map[string]any{
		"$schema":     "https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json",
		"name":        "io.github.zcag/tela",
		"description": "Self-hostable team wiki; agents read & write it via MCP; Atlas turns your repo into a cited wiki.",
		"repository": map[string]any{
			"url":       "https://github.com/zcag/tela",
			"source":    "github",
			"subfolder": "mcp",
		},
		"websiteUrl": origin,
		"packages": []any{
			map[string]any{
				"registryType":    "npm",
				"registryBaseUrl": "https://registry.npmjs.org",
				"identifier":      "tela-mcp",
				"runtimeHint":     "npx",
				"transport":       map[string]any{"type": "stdio"},
				"environmentVariables": []any{
					map[string]any{
						"name":        "TELA_BASE_URL",
						"description": "Origin of the tela instance the proxy connects to, e.g. " + origin,
						"isRequired":  true,
						"format":      "string",
					},
					map[string]any{
						"name":        "TELA_API_KEY",
						"description": "Personal access token (tela_pat_...), forwarded as Authorization: Bearer.",
						"isRequired":  true,
						"isSecret":    true,
						"format":      "string",
					},
				},
			},
		},
		"remotes": []any{
			map[string]any{"type": "streamable-http", "url": origin + "/api/mcp"},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_ = json.NewEncoder(w).Encode(manifest)
}
