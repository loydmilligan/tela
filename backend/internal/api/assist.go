package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/zcag/tela/backend/internal/agreement"
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

// Parent-document retrieval knobs for the ask path. Chunk retrieval finds the
// right neighbourhood, but an answer that spans a whole page — most painfully a
// "services using X" registry TABLE the chunker had to split — can't be
// reconstructed from one ~1400-char fragment. So the ask path pulls a deeper
// chunk pool, dedups to pages, and feeds the LLM the WHOLE body of the pages that
// matter, falling back to chunk text for the long tail.
const (
	// askRetrieveDepth: how many chunks to pull for the ask path (vs askMaxChunks
	// for plain search). Deeper so a topical hub page surfaces even when a
	// cross-encoder buried its terse table low.
	askRetrieveDepth = 24
	// askExpandTopRank: always expand the top-N pages by rank (precision — the
	// clearly-relevant pages get read in full).
	askExpandTopRank = 4
	// askDenseChunks: ...AND expand any page contributing at least this many
	// chunks to the pool. A page the corpus discusses densely for this query is
	// its hub (the Kafka registry page for "which projects use kafka"); read it
	// whole even if its best chunk ranked low. This is what rescues split tables.
	askDenseChunks = 3
	// askPageBodyCap / askExpandBudget bound the cost: per-page and cumulative
	// rune caps on expanded full bodies. Past the budget, pages degrade to their
	// chunk text. ~28k chars ≈ 7k tokens — full answers, bounded context.
	askPageBodyCap  = 12000
	askExpandBudget = 30000
	// askMaxPages caps how many distinct pages we render at all (expanded or not).
	askMaxPages = 12
	// askHubProbe: how many top-by-density pages the rerank-independent hub probe
	// returns (whole-page answers the reranker would bury).
	askHubProbe = 8
	// askLowConfidenceScore: when reranking is on, a top hit scoring below this
	// cross-encoder logit means retrieval found nothing strongly relevant. The
	// answer is still produced (best effort) but flagged low-confidence so the
	// reader knows to verify. Calibrated on the live corpus: a strong query tops
	// ~+3, an answerable aggregate ~-0.2, a genuinely out-of-scope question ~-6.6 —
	// so -4 fires only on the last kind. The reranker score scale is the only one
	// this threshold is valid for; with reranking off there's no comparable signal.
	askLowConfidenceScore = -4.0
)

// lowConfidenceNote prefixes an answer the system isn't confident in — rendered
// as a tela CAUTION callout. Deterministic (not left to the model) so the
// declaration is reliable.
const lowConfidenceNote = "> [!CAUTION]\n" +
	"> Low confidence — I didn't find strongly relevant material in your docs for this. " +
	"The answer below is a best effort from loosely related excerpts; verify it before relying on it.\n\n"

// lowConfidence reports whether an answer should be flagged low-confidence: only
// meaningful when reranking is on (the score is then a cross-encoder logit
// comparable to askLowConfidenceScore); the RRF-only scale has no equivalent.
func lowConfidence(rerankOn bool, topScore float64) bool {
	return rerankOn && topScore < askLowConfidenceScore
}

// askContext retrieves grounding for query and renders the numbered, cited
// excerpt block that feeds every generative feature, plus the per-page hits (for
// source citation, aligned to the [n] numbering) and the top fused score. It
// dedups chunks to pages and expands topically-central pages to their full body
// (see the knobs above). An empty hits slice means "nothing retrieved".
func (s *Server) askContext(ctx context.Context, userID int64, query string, spaceID *int64, limit int) (string, []rag.Hit, float64, error) {
	depth := askRetrieveDepth
	if limit > depth {
		depth = limit
	}
	hits, err := s.rag.Search(ctx, userID, query, spaceID, depth, "hybrid")
	if err != nil {
		return "", nil, 0, err
	}
	if len(hits) == 0 {
		return "", nil, 0, nil
	}
	// Best hit + chunk count per page, in rank order of first appearance.
	pageIDs := make([]int64, 0, len(hits))
	count := map[int64]int{}
	best := map[int64]rag.Hit{}
	chunkIDs := make([]int64, 0, len(hits))
	for _, h := range hits {
		if _, seen := best[h.PageID]; !seen {
			best[h.PageID] = h
			pageIDs = append(pageIDs, h.PageID)
			chunkIDs = append(chunkIDs, h.ChunkID)
		}
		count[h.PageID]++
	}
	// Topical-hub signal (rerank-INDEPENDENT). A precision reranker demotes terse
	// tables, so a page whose WHOLE body answers an aggregate question (a "services
	// using X" registry) can have every chunk ranked low — present but never
	// expanded. HubPages finds the page TITLED for the topic. Pull such hubs to the
	// FRONT so they expand before the per-page budget is spent on lower-value pages
	// (the registry page ranking ~8th was getting the budget-exhausted fallback —
	// the bug that kept the table out of the prompt). Best-effort.
	var hubFront []int64
	if hubs, herr := s.rag.HubPages(ctx, userID, query, spaceID, askHubProbe); herr == nil {
		for _, hp := range hubs {
			if hp.Count < askDenseChunks {
				continue
			}
			count[hp.PageID] += hp.Count
			if _, seen := best[hp.PageID]; !seen {
				best[hp.PageID] = rag.Hit{PageID: hp.PageID, Title: hp.Title, ChunkID: hp.ChunkID}
				chunkIDs = append(chunkIDs, hp.ChunkID)
			}
			hubFront = append(hubFront, hp.PageID)
		}
	}
	if len(hubFront) > 0 {
		hub := map[int64]bool{}
		for _, id := range hubFront {
			hub[id] = true
		}
		reordered := append([]int64{}, hubFront...)
		for _, id := range pageIDs {
			if !hub[id] {
				reordered = append(reordered, id)
			}
		}
		pageIDs = reordered
	}
	bodies, err := s.rag.PageBodies(ctx, userID, pageIDs, spaceID)
	if err != nil {
		return "", nil, 0, err
	}
	contents, err := s.rag.ChunkContents(ctx, userID, chunkIDs, spaceID)
	if err != nil {
		return "", nil, 0, err
	}
	block, pageHits := buildAskContext(pageIDs, best, count, bodies, contents)
	return block, pageHits, hits[0].Score, nil
}

// buildAskContext renders the numbered excerpt block from ranked pages, expanding
// topically-central pages (top-by-rank or dense hubs) to their full body and
// falling back to chunk text otherwise. Pure (no I/O) so it's unit-testable.
// Returns the block and the per-page hits aligned to the [n] numbering.
func buildAskContext(pageIDs []int64, best map[int64]rag.Hit, count map[int64]int, bodies, contents map[int64]string) (string, []rag.Hit) {
	var b strings.Builder
	pageHits := make([]rag.Hit, 0, len(pageIDs))
	spent, n := 0, 0
	for rank, pid := range pageIDs {
		if n >= askMaxPages {
			break
		}
		h := best[pid]
		n++
		full, hasBody := bodies[pid]
		expand := hasBody && full != "" && spent < askExpandBudget &&
			(rank < askExpandTopRank || count[pid] >= askDenseChunks)

		fmt.Fprintf(&b, "[%d] %s", n, h.Title)
		if !expand && h.HeadingPath != "" {
			fmt.Fprintf(&b, " — %s", h.HeadingPath) // heading path only adds context to a fragment
		}
		b.WriteString("\n")

		body := h.Snippet
		if expand {
			body = clampRunes(full, askPageBodyCap)
			spent += len(body)
		} else if c, ok := contents[h.ChunkID]; ok && c != "" {
			body = c
		}
		b.WriteString(body)
		b.WriteString("\n\n")
		pageHits = append(pageHits, h)
	}
	return b.String(), pageHits
}

// askMaxConflicts caps how many known-disagreement lines we hand the model — a
// noise/cost bound; the LLM filters these to the question-relevant ones anyway.
const askMaxConflicts = 6

// askConflictNote builds the "known disagreements" block for the cited sources:
// precomputed contradictions (the agreement worker) keyed to the [n] excerpt
// numbers, INCLUDING ones whose other side wasn't retrieved. Returns "" when none.
// The system prompt tells the model to raise only the question-relevant ones. The
// caller must pass the access-scoped cited hits (same order as the [n] numbering).
func (s *Server) askConflictNote(ctx context.Context, pageHits []rag.Hit) string {
	if s.agreement == nil || len(pageHits) == 0 {
		return ""
	}
	ids := make([]int64, len(pageHits))
	for i, h := range pageHits {
		ids[i] = h.PageID
	}
	byPage, err := s.agreement.DisputesFor(ctx, ids)
	if err != nil || len(byPage) == 0 {
		return ""
	}
	return formatConflicts(pageHits, byPage)
}

// formatConflicts renders the disagreement block (pure, so it's unit-testable).
// It walks pageHits in [n] order, dedups symmetric pairs (a conflict recorded on
// both pages' rows), skips reasonless entries, and caps the list.
func formatConflicts(pageHits []rag.Hit, byPage map[int64][]agreement.Dispute) string {
	numOf := make(map[int64]int, len(pageHits))
	for i, h := range pageHits {
		numOf[h.PageID] = i + 1
	}
	seen := map[[2]int64]bool{}
	var b strings.Builder
	lines := 0
	for _, h := range pageHits {
		for _, d := range byPage[h.PageID] {
			if lines >= askMaxConflicts {
				break
			}
			reason := strings.TrimSpace(d.Reason)
			if reason == "" {
				continue
			}
			key := [2]int64{h.PageID, d.PageID}
			if key[0] > key[1] {
				key[0], key[1] = key[1], key[0]
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			fmt.Fprintf(&b, "- [%d] %q may conflict with %q — %s\n", numOf[h.PageID], h.Title, d.Title, reason)
			lines++
		}
	}
	if lines == 0 {
		return ""
	}
	return "\nKnown disagreements among these sources (raise only if relevant to the question, and give both values):\n" + b.String() + "\n"
}

// clampRunes truncates s to at most n runes (never splitting a rune).
func clampRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// askComplete applies the compute caps (rate + monthly) and runs the LLM,
// writing the HTTP error itself on any failure. Returns (answer, true) only on
// success. label categorises the rate bucket (e.g. "ask", "draft").
func (s *Server) askComplete(w http.ResponseWriter, r *http.Request, u *auth.User, label, system, user string) (string, bool) {
	if !s.askComputeOK(w, r, u, label) {
		return "", false
	}
	answer, err := s.llm.Complete(r.Context(), system, user)
	if err != nil {
		writeError(w, http.StatusBadGateway, "completion_failed", "generation failed")
		return "", false
	}
	return answer, true
}

// askComputeOK runs the compute caps (per-request rate + monthly cap), writing
// the HTTP error itself on failure. Shared by askComplete (JSON) and the
// streaming ask path, which must clear the guards BEFORE it starts the SSE body
// so a 429/cap stays a clean HTTP status rather than a mid-stream surprise.
func (s *Server) askComputeOK(w http.ResponseWriter, r *http.Request, u *auth.User, label string) bool {
	acct := account{Kind: accountUser, ID: u.ID}
	if !s.cloudRateOK(w, label, acct) {
		return false
	}
	if ae := s.checkAndRecordLLMCall(r.Context(), acct); ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return false
	}
	return true
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
