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
	if b := bearerSpace(r); b != nil {
		spaceID = b
	}

	// Retrieve grounding via the shared seam (also used by draft/answer-to-page).
	excerpts, hits, top, err := s.askContext(r.Context(), u.ID, req.Question, spaceID, req.Limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "retrieval failed")
		return
	}
	// Log every ask with its retrieval confidence (best-effort) — feeds the
	// knowledge-gaps view, including the zero-hit case (a clear gap).
	_ = s.rag.LogAsk(r.Context(), u.ID, spaceID, req.Question, len(hits), top)
	s.recordRequestEvent(r, eventInput{
		Type: evtAsk, ActorUserID: &u.ID, ActorLabel: u.Username,
		Detail: fmt.Sprintf("%q (%d hits)", req.Question, len(hits)),
	})
	if len(hits) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"answer":  "I couldn't find anything in your documents to answer that.",
			"sources": []rag.Hit{},
		})
		return
	}

	answer, ok := s.askComplete(w, r, u, "ask", askSystemPrompt,
		"Answer the question using only these document excerpts.\n\n"+excerpts+"Question: "+req.Question)
	if !ok {
		return
	}

	// Weak retrieval → still answer, but declare it. The caller (UI) gets a flag;
	// the answer text carries a deterministic callout so the warning survives even
	// when only the prose is shown.
	low := lowConfidence(s.rag.RerankEnabled(), top)
	if low {
		answer = lowConfidenceNote + answer
	}
	resp := map[string]any{"answer": answer, "sources": hits, "low_confidence": low}
	// Suggest follow-up questions so an answer becomes a thread to pull on
	// (ask-first navigation). Best-effort.
	if f := s.genFollowups(r.Context(), u, req.Question, answer); len(f) > 0 {
		resp["followups"] = f
	}
	writeJSON(w, http.StatusOK, resp)
}

// askSystemPrompt is the grounding instruction shared by the JSON and streaming
// ask handlers (single source so the two never drift).
const askSystemPrompt = "You are a helpful assistant answering questions strictly from the provided document excerpts. " +
	"Cite the relevant sources by their [n] number. If the excerpts don't contain the answer, say so — do not invent facts. " +
	"If the excerpts disagree — one excerpt's value, status, or claim conflicting with another's — surface the discrepancy explicitly rather than assuming they agree or silently picking one."

// RAGAskStream is the streaming (SSE) twin of RAGAsk: identical retrieval and
// prompt, but the answer is streamed token-by-token over text/event-stream so the
// UI renders it live AND the connection never idles — structurally killing the
// idle/proxy-timeout failure a slow blocking generation hit. Events:
//   - sources:   { sources: []Hit, low_confidence: bool }  (before generation)
//   - token:     { t: "…" }                                 (per delta)
//   - followups: { followups: []string }                    (after the answer)
//   - done:      {}
//   - error:     { code: "completion_failed" }              (mid-stream failure)
//
// The JSON /api/rag/ask is left untouched for MCP + non-web clients.
func (s *Server) RAGAskStream(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	if !s.askGuards(w) {
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
	if b := bearerSpace(r); b != nil {
		spaceID = b
	}

	excerpts, hits, top, err := s.askContext(r.Context(), u.ID, req.Question, spaceID, req.Limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "retrieval failed")
		return
	}
	_ = s.rag.LogAsk(r.Context(), u.ID, spaceID, req.Question, len(hits), top)
	s.recordRequestEvent(r, eventInput{
		Type: evtAsk, ActorUserID: &u.ID, ActorLabel: u.Username,
		Detail: fmt.Sprintf("%q (%d hits)", req.Question, len(hits)),
	})

	// Clear the compute guards BEFORE any SSE byte so a 429/cap stays a clean HTTP
	// status. No LLM call on the zero-hit path, so skip the guard there.
	if len(hits) > 0 && !s.askComputeOK(w, r, u, "ask") {
		return
	}

	sse, ok := newSSEWriter(w)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal", "streaming unsupported")
		return
	}

	low := lowConfidence(s.rag.RerankEnabled(), top)
	_ = sse.event("sources", map[string]any{"sources": hits, "low_confidence": low})

	if len(hits) == 0 {
		_ = sse.event("token", map[string]string{"t": "I couldn't find anything in your documents to answer that."})
		_ = sse.event("done", map[string]any{})
		return
	}

	// Low retrieval confidence → still answer, but lead with the deterministic
	// callout (streamed as the first tokens so it survives prose-only views).
	var answer strings.Builder
	if low {
		answer.WriteString(lowConfidenceNote)
		_ = sse.event("token", map[string]string{"t": lowConfidenceNote})
	}
	streamErr := s.llm.CompleteStream(r.Context(), askSystemPrompt,
		"Answer the question using only these document excerpts.\n\n"+excerpts+"Question: "+req.Question,
		func(tok string) error {
			answer.WriteString(tok)
			return sse.event("token", map[string]string{"t": tok})
		})
	if streamErr != nil {
		// Client gone (ctx canceled) → just stop; nothing to deliver. Otherwise the
		// generation upstream failed — tell the UI so it shows the retry message.
		if r.Context().Err() == nil {
			_ = sse.event("error", map[string]string{"code": "completion_failed"})
		}
		return
	}

	if f := s.genFollowups(r.Context(), u, req.Question, answer.String()); len(f) > 0 {
		_ = sse.event("followups", map[string]any{"followups": f})
	}
	_ = sse.event("done", map[string]any{})
}

// bearerSpace returns the space a space-pinned bearer key is locked to (else
// nil), the shared narrow used by every read endpoint below.
func bearerSpace(r *http.Request) *int64 {
	if k, isBearer := auth.APIKeyFromContext(r.Context()); isBearer && k.SpaceID != nil {
		return k.SpaceID
	}
	return nil
}

// RAGRelated handles GET /api/pages/{id}/related[?limit=]
// Semantically related pages ("see also") for a page, access-scoped. 404 when
// the page is out of scope; works without a live embedder (uses stored vectors).
func (s *Server) RAGRelated(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	k, _ := auth.APIKeyFromContext(r.Context())
	pid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || pid <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "page id must be a positive integer")
		return
	}
	// Verify read access to the source page (404s if out of scope).
	if _, ae := s.getPageCore(r.Context(), u, k, pid); ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	related, err := s.rag.RelatedPages(r.Context(), u.ID, pid, bearerSpace(r), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "related lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"related": related})
}

// suggestLinksRequest is the POST /api/rag/suggest-links body.
type suggestLinksRequest struct {
	Text    string `json:"text"`
	SpaceID *int64 `json:"space_id"`
	Limit   int    `json:"limit"`
}

// RAGSuggestLinks handles POST /api/rag/suggest-links {text, space_id?, limit?}
// Existing pages the draft text should link to (assisted authoring). Needs a
// live embedder (the draft isn't indexed). 503 when the embedder is off.
func (s *Server) RAGSuggestLinks(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	if !s.rag.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "rag_disabled", "semantic features are not configured")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, cloudMaxRequestBytes)
	var req suggestLinksRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	spaceID := req.SpaceID
	if b := bearerSpace(r); b != nil {
		spaceID = b
	}
	out, err := s.rag.SuggestLinks(r.Context(), u.ID, req.Text, spaceID, req.Limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "suggest-links failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"suggestions": out})
}

// RAGOverlaps handles GET /api/rag/overlaps[?space_id=&threshold=&limit=]
// Near-duplicate page pairs for wiki hygiene, access-scoped to the caller.
func (s *Server) RAGOverlaps(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
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
	if b := bearerSpace(r); b != nil {
		spaceID = b
	}
	threshold, _ := strconv.ParseFloat(q.Get("threshold"), 64)
	limit, _ := strconv.Atoi(q.Get("limit"))
	pairs, err := s.rag.FindOverlaps(r.Context(), u.ID, spaceID, threshold, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "overlap lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"overlaps": pairs})
}

// RAGGaps handles GET /api/rag/gaps[?since_days=&limit=]
// Knowledge gaps: the most-asked questions the corpus couldn't answer. Admin-only
// — it exposes users' questions.
func (s *Server) RAGGaps(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireInstanceAdmin(w, r); !ok {
		return
	}
	q := r.URL.Query()
	sinceDays, _ := strconv.Atoi(q.Get("since_days"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	gaps, err := s.rag.KnowledgeGaps(r.Context(), sinceDays, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "gaps query failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"gaps": gaps})
}
