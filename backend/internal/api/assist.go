package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/zcag/tela/backend/internal/auth"
	"github.com/zcag/tela/backend/internal/rag"
)

// Ask-first ("talk to your docs") — generative features layered on retrieval.
// They all share ONE seam so this doesn't fan out into N copies of the same
// retrieve→prompt→complete dance:
//
//   askContext  — retrieve the top chunks + render the cited excerpt block
//   askComplete — apply the per-account compute caps and run the LLM
//
// RAGAsk is refactored onto these too (so the seam is exercised, not bypassed).

// askContext retrieves the top chunks for query and renders the numbered, cited
// excerpt block that grounds every generative feature, plus the hits (for source
// citation) and the top fused score. An empty hits slice means "nothing
// retrieved" — callers decide whether to proceed.
func (s *Server) askContext(ctx context.Context, userID int64, query string, spaceID *int64, limit int) (string, []rag.Hit, float64, error) {
	if limit <= 0 || limit > askMaxChunks {
		limit = askMaxChunks
	}
	hits, err := s.rag.Search(ctx, userID, query, spaceID, limit, "hybrid")
	if err != nil {
		return "", nil, 0, err
	}
	if len(hits) == 0 {
		return "", nil, 0, nil
	}
	ids := make([]int64, len(hits))
	for i, h := range hits {
		ids[i] = h.ChunkID
	}
	contents, err := s.rag.ChunkContents(ctx, userID, ids, spaceID)
	if err != nil {
		return "", nil, 0, err
	}
	var b strings.Builder
	for i, h := range hits {
		fmt.Fprintf(&b, "[%d] %s", i+1, h.Title)
		if h.HeadingPath != "" {
			fmt.Fprintf(&b, " — %s", h.HeadingPath)
		}
		b.WriteString("\n")
		body := h.Snippet
		if full, ok := contents[h.ChunkID]; ok && full != "" {
			body = full
		}
		b.WriteString(body)
		b.WriteString("\n\n")
	}
	return b.String(), hits, hits[0].Score, nil
}

// askComplete applies the compute caps (rate + monthly) and runs the LLM,
// writing the HTTP error itself on any failure. Returns (answer, true) only on
// success. label categorises the rate bucket (e.g. "ask", "draft").
func (s *Server) askComplete(w http.ResponseWriter, r *http.Request, u *auth.User, label, system, user string) (string, bool) {
	acct := account{Kind: accountUser, ID: u.ID}
	if !s.cloudRateOK(w, label, acct) {
		return "", false
	}
	if ae := s.checkAndRecordLLMCall(r.Context(), acct); ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return "", false
	}
	answer, err := s.llm.Complete(r.Context(), system, user)
	if err != nil {
		writeError(w, http.StatusBadGateway, "completion_failed", "generation failed")
		return "", false
	}
	return answer, true
}

// askGuards is the shared precondition for every generative endpoint: embedder +
// LLM configured. Returns false (and writes the 503) when unavailable.
func (s *Server) askGuards(w http.ResponseWriter) bool {
	if !s.rag.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "rag_disabled", "semantic search is not configured")
		return false
	}
	if !s.llm.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "llm_disabled", "managed AI is not configured")
		return false
	}
	return true
}

// genFollowups returns up to 3 suggested follow-up questions for an answer.
// Best-effort: a cap hit or LLM error yields no follow-ups (never fails the ask).
func (s *Server) genFollowups(ctx context.Context, u *auth.User, question, answer string) []string {
	if ae := s.checkAndRecordLLMCall(ctx, account{Kind: accountUser, ID: u.ID}); ae != nil {
		return nil
	}
	const sys = "Suggest up to 3 concise follow-up questions a reader might ask next, " +
		"each answerable from a team wiki. Output one question per line, no numbering, no preamble."
	out, err := s.llm.Complete(ctx, sys, "Question: "+question+"\n\nAnswer: "+answer)
	if err != nil {
		return nil
	}
	return parseLines(out, 3)
}

// parseLines splits a model's line-per-item output into a clean, capped slice:
// strips bullets/numbering, drops blanks, dedupes, caps at max.
func parseLines(s string, max int) []string {
	var out []string
	seen := map[string]bool{}
	for _, ln := range strings.Split(s, "\n") {
		ln = strings.TrimSpace(ln)
		ln = strings.TrimLeft(ln, "-*•0123456789.) \t")
		ln = strings.TrimSpace(ln)
		if ln == "" || seen[strings.ToLower(ln)] {
			continue
		}
		seen[strings.ToLower(ln)] = true
		out = append(out, ln)
		if len(out) >= max {
			break
		}
	}
	return out
}

// sourcesBlock renders retrieved hits as a markdown "Sources" section with
// tela://page links (which the indexer records as backlinks — so a generated
// page wires itself back to its sources).
func sourcesBlock(hits []rag.Hit) string {
	if len(hits) == 0 {
		return ""
	}
	seen := map[int64]bool{}
	var b strings.Builder
	b.WriteString("\n\n---\n\n## Sources\n\n")
	for _, h := range hits {
		if seen[h.PageID] {
			continue
		}
		seen[h.PageID] = true
		fmt.Fprintf(&b, "- [%s](tela://page/%d)\n", h.Title, h.PageID)
	}
	return b.String()
}

// ── draft: ask-first authoring ──────────────────────────────────────────────

type draftRequest struct {
	Topic   string `json:"topic"`
	SpaceID *int64 `json:"space_id"`
	Limit   int    `json:"limit"`
}

// RAGDraft handles POST /api/rag/draft {topic, space_id?, limit?}
// Returns a grounded markdown DRAFT for a new page about `topic`, built from the
// most relevant existing pages (cited). Not saved — the caller edits then saves.
func (s *Server) RAGDraft(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	if !s.askGuards(w) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, cloudMaxRequestBytes)
	var req draftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Topic) == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "topic is required")
		return
	}
	spaceID := req.SpaceID
	if b := bearerSpace(r); b != nil {
		spaceID = b
	}
	excerpts, hits, _, err := s.askContext(r.Context(), u.ID, req.Topic, spaceID, req.Limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "retrieval failed")
		return
	}
	const sys = "You draft documentation pages for a team wiki in clean markdown (headings, lists, short paragraphs). " +
		"Ground the draft in the provided excerpts and cite them inline as [n] where used. " +
		"If the excerpts are thin, produce a sensible starting outline instead of inventing facts."
	user := "Excerpts:\n\n" + excerpts + "\nDraft a wiki page about: " + req.Topic
	draft, okc := s.askComplete(w, r, u, "draft", sys, user)
	if !okc {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"draft": draft, "sources": hits})
}

// ── answer-to-page: the loop-closer ─────────────────────────────────────────

type answerToPageRequest struct {
	Question string `json:"question"`
	SpaceID  int64  `json:"space_id"`
	ParentID *int64 `json:"parent_id"`
	Title    string `json:"title"`
}

// RAGAnswerToPage handles POST /api/rag/answer-to-page {question, space_id, parent_id?, title?}
// Answers the question from the docs AND saves the answer as a new page (cited),
// turning ephemeral Q&A into durable knowledge — closing the ask → gap → write
// loop. Requires editor access to the target space (createPageCore enforces it).
func (s *Server) RAGAnswerToPage(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	if !s.askGuards(w) {
		return
	}
	k, _ := auth.APIKeyFromContext(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, cloudMaxRequestBytes)
	var req answerToPageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Question) == "" || req.SpaceID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "question and space_id are required")
		return
	}
	excerpts, hits, top, err := s.askContext(r.Context(), u.ID, req.Question, &req.SpaceID, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "retrieval failed")
		return
	}
	_ = s.rag.LogAsk(r.Context(), u.ID, &req.SpaceID, req.Question, len(hits), top)
	if len(hits) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "no_answer", "couldn't find anything in the docs to answer that — nothing to save")
		return
	}
	const sys = "Answer the question strictly from the provided excerpts, in clean markdown suitable for a wiki page. " +
		"Cite sources inline as [n]. If the excerpts don't fully answer it, say what's known and what's missing."
	answer, okc := s.askComplete(w, r, u, "answer_to_page", sys, "Excerpts:\n\n"+excerpts+"\nQuestion: "+req.Question)
	if !okc {
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = strings.TrimSpace(req.Question)
	}
	body := answer + sourcesBlock(hits)
	page, ae := s.createPageCore(r.Context(), u, k, pageCreateRequest{
		SpaceID: req.SpaceID, ParentID: req.ParentID, Title: title, Body: body,
	})
	if ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"page": page, "answer": answer, "sources": hits})
}

// ── page questions: "what does this page answer?" ───────────────────────────

// RAGPageQuestions handles GET /api/pages/{id}/questions
// Suggested questions the given page answers — for a "people often ask…" affordance
// and as seeds for ask-first navigation. Read access to the page required.
func (s *Server) RAGPageQuestions(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	if !s.llm.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "llm_disabled", "managed AI is not configured")
		return
	}
	k, _ := auth.APIKeyFromContext(r.Context())
	pid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || pid <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "page id must be a positive integer")
		return
	}
	p, ae := s.getPageCore(r.Context(), u, k, pid)
	if ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	const sys = "List up to 5 distinct questions that THIS page directly answers. " +
		"One question per line, no numbering, no preamble."
	body, _ := mcpCapBody(p.Body)
	out, okc := s.askComplete(w, r, u, "page_questions", sys, "Title: "+p.Title+"\n\n"+body)
	if !okc {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"questions": parseLines(out, 5)})
}
