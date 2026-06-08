package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/zcag/tela/backend/internal/auth"
)

// Cloud control plane — managed services the main instance HOSTS and a connected
// self-hoster reaches over HTTP. The whole point of this file is to add no
// parallel logic: it reuses the same embedder (s.rag), the same entitlement
// resolver (planFor / featureEnabled), and the same bearer validation
// (auth.LookupAPIKey) the instance already uses in-process. The "cloud token" is
// an ordinary tela PAT; the gate is the account's plan entitlement, not a new
// token type or scope. The main instance consumes these capabilities directly;
// self-hosters consume the identical code over /api/cloud/*.

// cloudAccount authenticates the bearer PAT and returns the owning user account.
// Same validation path as the rest of the system — no second auth scheme.
func (s *Server) cloudAccount(r *http.Request) (account, bool) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, auth.BearerPrefix) {
		return account{}, false
	}
	tok := strings.TrimSpace(strings.TrimPrefix(h, auth.BearerPrefix))
	key, err := auth.LookupAPIKey(r.Context(), s.DB, auth.LoadAPIKeySecret(), tok)
	if err != nil {
		return account{}, false
	}
	return account{Kind: accountUser, ID: key.UserID}, true
}

// CloudEntitlements returns the effective plan + feature flags for the token's
// account — the control-plane primitive a connected instance can read to learn
// what its subscription grants. Reuses planFor, so the answer is computed by the
// exact code the main instance uses in-process.
func (s *Server) CloudEntitlements(w http.ResponseWriter, r *http.Request) {
	acct, ok := s.cloudAccount(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "valid bearer token required")
		return
	}
	p, err := planFor(r.Context(), s.DB, acct)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "resolve plan failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"plan_key":  p.Key,
		"plan_name": p.Name,
		"features":  p.Features,
	})
}

// ollamaEmbedRequest mirrors the Ollama /api/embed shape so the existing
// rag.OllamaEmbedder works unchanged against this endpoint: a self-hoster just
// points TELA_RAG_EMBED_URL at /api/cloud/ollama and sets TELA_RAG_EMBED_TOKEN.
type ollamaEmbedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

// CloudEmbed is the managed RAG embed proxy: authenticate the PAT, gate on the
// managed_rag entitlement, then run the input through THIS instance's own
// embedder and return the Ollama-shaped {embeddings:[[...]]}. The single
// embedder serves both in-process search and connected self-hosters — there is
// no second embedding implementation.
func (s *Server) CloudEmbed(w http.ResponseWriter, r *http.Request) {
	acct, ok := s.cloudAccount(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "valid bearer token required")
		return
	}
	if !s.featureEnabled(r.Context(), acct, "managed_rag") {
		writeError(w, http.StatusForbidden, "forbidden", "plan does not include managed semantic search")
		return
	}
	if !s.rag.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "embedder not configured on this instance")
		return
	}
	var req ollamaEmbedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	if strings.TrimSpace(req.Input) == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "input is required")
		return
	}
	vec, err := s.rag.Embed(r.Context(), req.Input)
	if err != nil {
		writeError(w, http.StatusBadGateway, "embed_failed", "embedding failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"embeddings": [][]float32{vec}})
}
