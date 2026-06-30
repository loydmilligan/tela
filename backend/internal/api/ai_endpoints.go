package api

import (
	"net/http"
	"net/url"
	"os"
	"time"
)

// ai_endpoints.go — GET /api/admin/ai-endpoints. Instance-admin only. The
// in-app "AI endpoints & reliability" breakdown for Settings → Insights: each
// backing service's live reachability, latency, and time-in-state, plus the
// configured endpoint host/model and whether it's routed through a relief proxy.
//
// What lives HERE vs in Grafana: this is the at-a-glance, no-extra-infra view
// (always works, even on a bare self-host). The DEEP per-backend failover
// detail — which relief endpoint served, fallback counts, per-backend latency —
// is exported by the LiteLLM proxy's own /metrics and visualised in Grafana;
// grafana_url (TELA_GRAFANA_AI_URL) deep-links there when the operator sets it.

type aiEndpointHealth struct {
	Service    string `json:"service"`    // "embed" | "chat"
	Configured bool   `json:"configured"` // wired up at all
	Healthy    bool   `json:"healthy"`
	Reason     string `json:"reason,omitempty"` // when unhealthy
	Endpoint   string `json:"endpoint"`         // redacted scheme://host[:port] — never a path or secret
	Model      string `json:"model"`
	Proxied    bool   `json:"proxied"` // via an OpenAI /v1 proxy (a relief pool is possible)
	LatencyMs  int64  `json:"latency_ms"`
	LastOK     string `json:"last_ok,omitempty"` // sqlite-format UTC ts (FE relativeTimeFromSqlite)
	Since      string `json:"since,omitempty"`   // when the current up/down state began
}

type aiEndpointsOut struct {
	Enabled    bool               `json:"enabled"` // configured + not admin-disabled
	Probed     bool               `json:"probed"`  // the background prober has a verdict yet
	Healthy    bool               `json:"healthy"` // overall (every configured service up)
	Services   []aiEndpointHealth `json:"services"`
	GrafanaURL string             `json:"grafana_url,omitempty"`
}

// AdminAIEndpoints serves the per-service AI reliability breakdown.
func (s *Server) AdminAIEndpoints(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireInstanceAdmin(w, r); !ok {
		return
	}

	checked, embed, chat := s.aiHealthSnapshot()

	embedURL, embedModel, embedProxied := s.rag.EmbedEndpoint()
	chatURL, chatModel := s.llm.Endpoint()

	out := aiEndpointsOut{
		Enabled:    s.aiEnabled(),
		Probed:     checked,
		Healthy:    s.aiHealthy(),
		GrafanaURL: os.Getenv("TELA_GRAFANA_AI_URL"),
		Services: []aiEndpointHealth{
			aiEndpointRow("embed", embedURL != "", embed, embedURL, embedModel, embedProxied, checked),
			aiEndpointRow("chat", chatURL != "", chat, chatURL, chatModel, chatURL != "", checked),
		},
	}
	writeJSON(w, http.StatusOK, out)
}

// aiEndpointRow assembles one service row. Before the first probe (checked
// false) it reports the configured-but-unverified state optimistically, matching
// aiHealthy()'s boot posture.
func aiEndpointRow(service string, configured bool, h aiServiceHealth, endpoint, model string, proxied, checked bool) aiEndpointHealth {
	row := aiEndpointHealth{
		Service:    service,
		Configured: configured,
		Endpoint:   redactEndpoint(endpoint),
		Model:      model,
		Proxied:    proxied,
	}
	if !configured {
		return row
	}
	if !checked {
		row.Healthy = true // unverified at boot — optimistic, like aiHealthy()
		return row
	}
	row.Healthy = h.healthy
	row.Reason = h.reason
	row.LatencyMs = h.latency.Milliseconds()
	row.LastOK = sqliteTS(h.lastOK)
	row.Since = sqliteTS(h.since)
	return row
}

// redactEndpoint reduces a configured URL to scheme://host[:port], dropping any
// path (e.g. /v1, /api/cloud/ollama) and userinfo so the admin screen shows
// where AI points without leaking a token-bearing path. Falls back to "" on a
// parse failure rather than echoing a raw secret-bearing string.
func redactEndpoint(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// sqliteTS formats a UTC time in tela's TEXT datetime shape, or "" for the zero
// time, so the frontend's relativeTimeFromSqlite renders it.
func sqliteTS(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02 15:04:05")
}
