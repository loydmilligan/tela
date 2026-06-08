package api

import (
	"net/http"
	"os"
	"strings"
)

// origin.go centralises "which origin does a browser-facing URL live on?".
//
// canonicalBaseURL is the single env-backed origin (TELA_PUBLIC_BASE_URL) — the
// one true tela host that owns the marketing/docs/MCP/sync/SEO surface. Once
// orgs can bring custom domains, a few browser-facing surfaces (auth emails,
// SSO callback, share URLs) follow the custom domain instead; that request-
// scoped resolution lives alongside this in originFor (see custom_domains.go).

// devBaseURL is the fallback origin so dev (no TELA_PUBLIC_BASE_URL) still
// produces a complete, clickable URL for surfaces that need one (auth emails,
// the share-create response). OG/permalink surfaces deliberately tolerate an
// empty base (path-only URLs), so they use canonicalBaseURL directly.
const devBaseURL = "http://localhost:8780"

// canonicalBaseURL returns the env-configured canonical origin with a single
// trailing slash trimmed. Empty when TELA_PUBLIC_BASE_URL is unset, producing
// path-only og:url / og:image — Slack and Twitter handle that fine in dev.
func canonicalBaseURL() string {
	return strings.TrimRight(os.Getenv("TELA_PUBLIC_BASE_URL"), "/")
}

// requestScheme reports the scheme the client used, trusting Caddy's
// X-Forwarded-Proto (Caddy is the only trusted upstream — see the XFF note in
// CLAUDE.md). Falls back to the TLS state, then http for plain dev.
func requestScheme(r *http.Request) string {
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		return strings.ToLower(p)
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// hostnameOnly lowercases a Host header and strips any :port (dev serves on
// host:port; Caddy passes the bare host in prod). Returns "" for an empty host.
func hostnameOnly(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	return host
}
