package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/zcag/tela/backend/internal/auth"
	"github.com/zcag/tela/backend/internal/rag"
)

// RAG (semantic retrieval) handlers. Thin wrappers over the internal/rag
// Service: this file owns auth + HTTP shape, the rag package owns the logic.
// Both endpoints 503 when the feature is unconfigured (TELA_RAG_EMBED_URL
// unset), so the routes can be registered unconditionally.

// RAGSearch handles GET /api/rag/search?q=&space_id=&limit=&mode=
// Hybrid chunk search scoped to the caller's space_access. Returns ranked
// chunks with page id + heading path for citation.
func (s *Server) RAGSearch(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	if !s.rag.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "rag_disabled", "semantic search is not configured")
		return
	}

	q := r.URL.Query()
	var spaceID *int64
	if v := q.Get("space_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "space_id must be a positive integer")
			return
		}
		spaceID = &id
	}
	// A space-scoped bearer key may only ever see its one space — force the
	// narrow even if the caller passed a different (or no) space_id. Mirrors the
	// Search handler's bearer branch.
	if k, isBearer := auth.APIKeyFromContext(r.Context()); isBearer && k.SpaceID != nil {
		spaceID = k.SpaceID
	}

	limit, _ := strconv.Atoi(q.Get("limit"))
	hits, err := s.rag.Search(r.Context(), u.ID, q.Get("q"), spaceID, limit, q.Get("mode"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "semantic search failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": hits})
}

// RAGReadChunk handles GET /api/rag/chunk?chunk_id=
// Returns one chunk's full section text (the chunk-granularity read between a
// search snippet and the whole-page get_page). Scoped to the caller's
// space_access; 404 when the chunk doesn't exist or is out of scope.
func (s *Server) RAGReadChunk(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	if !s.rag.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "rag_disabled", "semantic search is not configured")
		return
	}

	chunkID, err := strconv.ParseInt(r.URL.Query().Get("chunk_id"), 10, 64)
	if err != nil || chunkID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "chunk_id must be a positive integer")
		return
	}
	var spaceID *int64
	if k, isBearer := auth.APIKeyFromContext(r.Context()); isBearer && k.SpaceID != nil {
		spaceID = k.SpaceID
	}

	chunk, err := s.rag.ReadChunk(r.Context(), u.ID, chunkID, spaceID)
	if errors.Is(err, rag.ErrChunkNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "chunk not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "read chunk failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"chunk": chunk})
}

// RAGFreshness handles GET /api/rag/freshness[?space_id=]
// Without space_id: per-space index-health summary across every space the caller
// can access. With space_id: per-page status within that space. Always 200 with
// an `enabled` flag (the counts are real even when the embedder is off, so the
// admin view can show what's indexed vs what would need an embedder).
func (s *Server) RAGFreshness(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	if v := r.URL.Query().Get("space_id"); v != "" {
		spaceID, err := strconv.ParseInt(v, 10, 64)
		if err != nil || spaceID <= 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "space_id must be a positive integer")
			return
		}
		pages, err := s.rag.SpacePageFreshness(r.Context(), u.ID, spaceID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "freshness query failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"enabled": s.rag.Enabled(), "space_id": spaceID, "pages": pages})
		return
	}

	spaces, err := s.rag.Freshness(r.Context(), u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "freshness query failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": s.rag.Enabled(), "spaces": spaces})
}

// RAGReindex handles POST /api/rag/reindex?space_id=
// Chunks + embeds every page in the space. Requires membership (the same gate
// as reading the space); synchronous — fine for a wiki-scale corpus.
func (s *Server) RAGReindex(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	if !s.rag.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "rag_disabled", "semantic search is not configured")
		return
	}

	v := r.URL.Query().Get("space_id")
	spaceID, err := strconv.ParseInt(v, 10, 64)
	if err != nil || spaceID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "space_id is required")
		return
	}
	if _, ok := s.requireMembership(w, r, spaceID); !ok {
		return
	}

	pages, chunks, err := s.rag.ReindexSpace(r.Context(), spaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "reindex failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"indexed_pages":  pages,
		"indexed_chunks": chunks,
	})
}

// askRequest is the POST /api/rag/ask body.
type askRequest struct {
	Question string `json:"question"`
	SpaceID  *int64 `json:"space_id"`
	Limit    int    `json:"limit"`
}

// askMaxChunks caps how many retrieved chunks ground the answer (and bounds the
// prompt size) regardless of what the caller asks for.
const askMaxChunks = 8

// RAGAsk handles POST /api/rag/ask {question, space_id?, limit?}
// "Ask your docs": retrieve the top chunks via the EXISTING hybrid search
// (scoped to the caller's space_access — same anti-leak path as RAGSearch),
// build a grounded prompt from the chunk texts + their page references, call the
// in-process LLM (s.llm.Complete — the SAME client a self-hoster reaches over
// the managed cloud proxy), and return the answer plus the cited source pages.
// 503 when the LLM is unconfigured, mirroring the rag handlers.
func (s *Server) RAGAsk(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	if !s.rag.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "rag_disabled", "semantic search is not configured")
		return
	}
	if !s.llm.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "llm_disabled", "managed AI is not configured")
		return
	}
	// Same per-account compute throttle as the cloud proxies — ask runs an LLM
	// call, so an authenticated user can't hammer it unbounded.
	if !s.cloudRateOK(w, "ask", account{Kind: accountUser, ID: u.ID}) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, cloudMaxRequestBytes)
	var req askRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	if strings.TrimSpace(req.Question) == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "question is required")
		return
	}

	spaceID := req.SpaceID
	// A space-scoped bearer key may only ever see its one space — force the
	// narrow. Mirrors RAGSearch.
	if k, isBearer := auth.APIKeyFromContext(r.Context()); isBearer && k.SpaceID != nil {
		spaceID = k.SpaceID
	}

	limit := req.Limit
	if limit <= 0 || limit > askMaxChunks {
		limit = askMaxChunks
	}
	hits, err := s.rag.Search(r.Context(), u.ID, req.Question, spaceID, limit, "hybrid")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "retrieval failed")
		return
	}
	if len(hits) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"answer":  "I couldn't find anything in your documents to answer that.",
			"sources": []rag.Hit{},
		})
		return
	}

	system := "You are a helpful assistant answering questions strictly from the provided document excerpts. " +
		"Cite the relevant sources by their [n] number. If the excerpts don't contain the answer, say so — do not invent facts."

	var b strings.Builder
	b.WriteString("Answer the question using only these document excerpts.\n\n")
	for i, h := range hits {
		fmt.Fprintf(&b, "[%d] %s", i+1, h.Title)
		if h.HeadingPath != "" {
			fmt.Fprintf(&b, " — %s", h.HeadingPath)
		}
		b.WriteString("\n")
		b.WriteString(h.Snippet)
		b.WriteString("\n\n")
	}
	fmt.Fprintf(&b, "Question: %s", req.Question)

	// Monthly compute cap (atomic) — only counts a real LLM call (we're past the
	// empty-retrieval short-circuit), against the asking user's account.
	if ae := s.checkAndRecordLLMCall(r.Context(), account{Kind: accountUser, ID: u.ID}); ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	answer, err := s.llm.Complete(r.Context(), system, b.String())
	if err != nil {
		writeError(w, http.StatusBadGateway, "completion_failed", "answer generation failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"answer":  answer,
		"sources": hits,
	})
}
