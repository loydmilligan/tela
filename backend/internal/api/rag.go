package api

import (
	"net/http"
	"strconv"

	"github.com/zcag/tela/backend/internal/auth"
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
