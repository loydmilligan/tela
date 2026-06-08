package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/zcag/tela/backend/internal/auth"
)

// cloudMaxRequestBytes caps the managed-proxy request bodies (embed input / chat
// messages are text — 1 MiB is far more than any model context) so a client
// can't pin a huge buffer on the shared instance.
const cloudMaxRequestBytes = 1 << 20

// Cloud control plane — managed services the main instance HOSTS and a connected
// self-hoster reaches over HTTP. The whole point of this file is to add no
// parallel logic: it reuses the same embedder (s.rag), the same entitlement
// resolver (planFor / featureEnabled), and the same bearer validation
// (auth.LookupAPIKey) the instance already uses in-process. The "cloud token" is
// an ordinary tela PAT; the gate is the account's plan entitlement, not a new
// token type or scope. The main instance consumes these capabilities directly;
// self-hosters consume the identical code over /api/cloud/*.

// cloudAccount authenticates the bearer PAT and returns the owning user account,
// writing the error response itself (false = already responded). Same validation
// path as the rest of the system — no second auth scheme. Managed compute is an
// account-level, paid action, so a read-scoped or space-pinned PAT is rejected:
// a leaked low-privilege token must not be able to spend compute.
func (s *Server) cloudAccount(w http.ResponseWriter, r *http.Request) (account, bool) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, auth.BearerPrefix) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "valid bearer token required")
		return account{}, false
	}
	tok := strings.TrimSpace(strings.TrimPrefix(h, auth.BearerPrefix))
	key, err := auth.LookupAPIKey(r.Context(), s.DB, auth.LoadAPIKeySecret(), tok)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "valid bearer token required")
		return account{}, false
	}
	if key.Scope == auth.ScopeRead || key.SpaceID != nil {
		writeError(w, http.StatusForbidden, "forbidden", "an account-scoped, non-read token is required for cloud services")
		return account{}, false
	}
	return account{Kind: accountUser, ID: key.UserID}, true
}

// cloudRateOK enforces the per-account compute rate limit, writing 429 (with
// Retry-After) when exceeded. Keyed by account so abuse is bounded per
// subscriber, not per IP — the cheap, effective guard against a single entitled
// PAT hammering paid LLM/embedder compute.
func (s *Server) cloudRateOK(w http.ResponseWriter, purpose string, acct account) bool {
	ok, retry := s.cloudLimiter.allow(purpose, fmt.Sprintf("%s:%d", acct.Kind, acct.ID))
	if !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests; slow down")
		return false
	}
	return true
}

// publishingEntitlementRequired reports whether flipping a space to public must
// be gated on the owning account's `publishing` plan feature. Off by default —
// a self-host instance lets anyone publish their own spaces — and turned on for
// the cloud main instance via the instance setting. Single source of truth for
// the gate's posture; the check itself reuses featureEnabled (see spaces.go).
func (s *Server) publishingEntitlementRequired() bool {
	v, ok := s.settings.Get("require_publishing_entitlement")
	return ok && v == "true"
}

// CloudEntitlements returns the effective plan + feature flags for the token's
// account — the control-plane primitive a connected instance can read to learn
// what its subscription grants. Reuses planFor, so the answer is computed by the
// exact code the main instance uses in-process.
func (s *Server) CloudEntitlements(w http.ResponseWriter, r *http.Request) {
	acct, ok := s.cloudAccount(w, r)
	if !ok {
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
	acct, ok := s.cloudAccount(w, r)
	if !ok {
		return
	}
	if !s.featureEnabled(r.Context(), acct, "managed_rag") {
		writeError(w, http.StatusForbidden, "forbidden", "plan does not include managed semantic search")
		return
	}
	if !s.cloudRateOK(w, "embed", acct) {
		return
	}
	if !s.rag.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "embedder not configured on this instance")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, cloudMaxRequestBytes)
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

// chatProxyMessage / chatProxyRequest / chatProxyResponse mirror the
// OpenAI-compatible /v1/chat/completions shape so the existing llm.OpenAIClient
// works unchanged against this endpoint: a self-hoster just points TELA_LLM_URL
// at /api/cloud/llm/v1 and sets TELA_LLM_TOKEN. We extract system/user from the
// messages, run them through THIS instance's own llm.Complete, and return a
// minimal OpenAI-shaped {choices:[{message:{role,content}}]} so the client's
// decoder (and any other OpenAI client) reads it back natively.
type chatProxyMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatProxyRequest struct {
	Model    string             `json:"model"`
	Messages []chatProxyMessage `json:"messages"`
}

// CloudChat is the managed LLM proxy: authenticate the PAT, gate on the
// ask_docs entitlement, then run the prompt through THIS instance's own chat
// client and return the OpenAI-shaped completion. The single client serves both
// in-process /api/rag/ask and connected self-hosters — no second generation
// path. Routed at POST /api/cloud/llm/v1/chat/completions.
func (s *Server) CloudChat(w http.ResponseWriter, r *http.Request) {
	acct, ok := s.cloudAccount(w, r)
	if !ok {
		return
	}
	if !s.featureEnabled(r.Context(), acct, "ask_docs") {
		writeError(w, http.StatusForbidden, "forbidden", "plan does not include managed AI")
		return
	}
	if !s.cloudRateOK(w, "chat", acct) {
		return
	}
	if !s.llm.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "LLM not configured on this instance")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, cloudMaxRequestBytes)
	var req chatProxyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	var system, user string
	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			system = m.Content
		case "user":
			user = m.Content // last user message wins
		}
	}
	if strings.TrimSpace(user) == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "a user message is required")
		return
	}
	answer, err := s.llm.Complete(r.Context(), system, user)
	if err != nil {
		writeError(w, http.StatusBadGateway, "completion_failed", "completion failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"model": req.Model,
		"choices": []map[string]any{
			{"message": map[string]string{"role": "assistant", "content": answer}},
		},
	})
}
