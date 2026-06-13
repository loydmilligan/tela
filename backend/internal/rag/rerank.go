package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Reranking — optional second-stage precision. Hybrid+RRF is a strong first
// stage but ranks on lexical overlap + embedding distance, neither of which
// reads the query and a passage TOGETHER. A cross-encoder reranker does exactly
// that, so re-scoring the top fused candidates lifts the genuinely-relevant ones
// above the merely-similar. Off by default; the retrieval order is identical to
// before until TELA_RAG_RERANK_URL is set. Gate any rollout on `tela rag-eval`.

// rerankCandidates is how many top fused hits get re-scored before the final
// trim. Deep enough to rescue a relevant chunk RRF buried — including a single
// on-topic doc that a large, vocabulary-overlapping corpus pushed well down the
// fused list (the cross-encoder ranks it #1 once it actually sees it) — bounded so
// the cross-encoder stays cheap.
const rerankCandidates = 50

// rerankTimeout bounds the cross-encoder call. Rerank is best-effort — on failure
// the caller falls back to the fused order — so this is kept short: a healthy
// reranker answers in well under a second, and a slow/cold one must NOT drag the
// whole /ask out (a stalled reranker once turned every ask into a ~2-minute hang
// before the browser gave up). Fail fast, degrade gracefully.
const rerankTimeout = 5 * time.Second

// RerankResult is one document's score from the reranker, by its index in the
// input list. Higher score = more relevant.
type RerankResult struct {
	Index int
	Score float64
}

// Reranker re-scores documents against a query. One method, so a hosted or local
// cross-encoder is a sibling implementation, not a refactor.
type Reranker interface {
	Rerank(ctx context.Context, query string, docs []string) ([]RerankResult, error)
}

// RerankEnabled reports whether a reranker is configured.
func (s *Service) RerankEnabled() bool { return s != nil && s.rr != nil }

// rerankIDs re-scores the given chunk ids against the query (best-effort) and
// returns them reordered most-relevant-first, writing the rerank score into the
// score map so hydrate surfaces it. On any failure it returns the input order
// unchanged (and a non-nil error for the caller to log) — reranking must never
// degrade availability of the underlying hybrid results.
func (s *Service) rerankIDs(ctx context.Context, userID int64, query string, ids []int64, spaceID *int64, score map[int64]float64) ([]int64, error) {
	if len(ids) < 2 {
		return ids, nil
	}
	contents, err := s.ChunkContents(ctx, userID, ids, spaceID)
	if err != nil {
		return ids, err
	}
	docs := make([]string, len(ids))
	for i, id := range ids {
		docs[i] = contents[id] // missing → "" ; reranker scores it low
	}
	res, err := s.rr.Rerank(ctx, query, docs)
	if err != nil {
		return ids, err
	}
	reordered := make([]int64, 0, len(ids))
	seen := make(map[int64]bool, len(ids))
	for _, r := range res {
		if r.Index < 0 || r.Index >= len(ids) {
			continue
		}
		id := ids[r.Index]
		if seen[id] {
			continue
		}
		seen[id] = true
		reordered = append(reordered, id)
		score[id] = r.Score
	}
	// Append any ids the reranker omitted (e.g. top_n cut), preserving fused order.
	for _, id := range ids {
		if !seen[id] {
			reordered = append(reordered, id)
		}
	}
	return reordered, nil
}

// HTTPReranker calls a Cohere/Jina-compatible /rerank endpoint:
//
//	POST {model?, query, documents:[...], top_n} -> {results:[{index, relevance_score}]}
//
// It also accepts a bare results array and a `score` field, so it works against
// TEI/Infinity/vLLM rerank servers too. base is the FULL endpoint URL.
type HTTPReranker struct {
	url    string
	model  string
	token  string
	client *http.Client
}

func NewHTTPReranker(url, model, token string) *HTTPReranker {
	return &HTTPReranker{
		url:    strings.TrimSpace(url),
		model:  model,
		token:  strings.TrimSpace(token),
		client: &http.Client{Timeout: rerankTimeout},
	}
}

func (h *HTTPReranker) Rerank(ctx context.Context, query string, docs []string) ([]RerankResult, error) {
	reqBody := map[string]any{"query": query, "documents": docs, "top_n": len(docs)}
	if h.model != "" {
		reqBody["model"] = h.model
	}
	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rerank: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rerank: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return parseRerank(raw)
}

// parseRerank tolerates the two common response shapes: {results:[...]} and a
// bare array, with the score under relevance_score or score. Results are sorted
// descending by score so the caller needn't trust server ordering.
func parseRerank(raw []byte) ([]RerankResult, error) {
	type item struct {
		Index     int      `json:"index"`
		Relevance *float64 `json:"relevance_score"`
		Score     *float64 `json:"score"`
	}
	var wrapped struct {
		Results []item `json:"results"`
	}
	items := []item(nil)
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.Results != nil {
		items = wrapped.Results
	} else if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("rerank: decode: %w", err)
	}
	out := make([]RerankResult, 0, len(items))
	for _, it := range items {
		sc := 0.0
		switch {
		case it.Relevance != nil:
			sc = *it.Relevance
		case it.Score != nil:
			sc = *it.Score
		}
		out = append(out, RerankResult{Index: it.Index, Score: sc})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out, nil
}
