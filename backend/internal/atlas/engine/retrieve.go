package engine

import (
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/zcag/tela/backend/internal/atlas/core"
)

// Retriever does hybrid retrieval over a run's chunks: dense (cosine over
// embeddings) fused with sparse (BM25 over tokens). In-memory and dependency-
// free — fine for repo-scale corpora; swap for an ANN index behind this type if
// it ever needs to scale past a single box.
type Retriever struct {
	chunks []core.Chunk
	norms  []float64   // |vector| per chunk
	docs   [][]string  // tokenized chunk text
	dl     []float64   // doc lengths
	avgdl  float64
	df     map[string]int
	n      int
}

const (
	bm25K1     = 1.2
	bm25B      = 0.75
	denseWeight = 0.6 // dense:sparse fusion
)

func BuildRetriever(chunks []core.Chunk) *Retriever {
	r := &Retriever{chunks: chunks, df: map[string]int{}, n: len(chunks)}
	r.norms = make([]float64, len(chunks))
	r.docs = make([][]string, len(chunks))
	r.dl = make([]float64, len(chunks))
	var total float64
	for i, c := range chunks {
		r.norms[i] = norm(c.Vector)
		toks := tokenize(c.Symbol + " " + c.File + " " + c.Text)
		r.docs[i] = toks
		r.dl[i] = float64(len(toks))
		total += r.dl[i]
		seen := map[string]bool{}
		for _, t := range toks {
			if !seen[t] {
				seen[t] = true
				r.df[t]++
			}
		}
	}
	if len(chunks) > 0 {
		r.avgdl = total / float64(len(chunks))
	}
	return r
}

// Search returns the top-k chunks for a query, fusing dense + sparse scores.
func (r *Retriever) Search(queryVec []float32, queryText string, k int) []core.Chunk {
	if r.n == 0 {
		return nil
	}
	qn := norm(queryVec)
	qToks := tokenize(queryText)
	dense := make([]float64, r.n)
	sparse := make([]float64, r.n)
	for i := range r.chunks {
		dense[i] = cosine(queryVec, r.chunks[i].Vector, qn, r.norms[i])
		sparse[i] = r.bm25(qToks, i)
	}
	normalizeInPlace(dense)
	normalizeInPlace(sparse)

	idx := make([]int, r.n)
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		sa := denseWeight*dense[idx[a]] + (1-denseWeight)*sparse[idx[a]]
		sb := denseWeight*dense[idx[b]] + (1-denseWeight)*sparse[idx[b]]
		return sa > sb
	})
	if k > r.n {
		k = r.n
	}
	out := make([]core.Chunk, k)
	for i := 0; i < k; i++ {
		out[i] = r.chunks[idx[i]]
	}
	return out
}

func (r *Retriever) bm25(qToks []string, doc int) float64 {
	if len(r.docs[doc]) == 0 {
		return 0
	}
	tf := map[string]int{}
	for _, t := range r.docs[doc] {
		tf[t]++
	}
	var score float64
	for _, qt := range qToks {
		f := float64(tf[qt])
		if f == 0 {
			continue
		}
		idf := math.Log(1 + (float64(r.n)-float64(r.df[qt])+0.5)/(float64(r.df[qt])+0.5))
		denom := f + bm25K1*(1-bm25B+bm25B*r.dl[doc]/r.avgdl)
		score += idf * (f * (bm25K1 + 1)) / denom
	}
	return score
}

// --- math / tokenization ---

func norm(v []float32) float64 {
	var s float64
	for _, x := range v {
		s += float64(x) * float64(x)
	}
	return math.Sqrt(s)
}

func cosine(a, b []float32, na, nb float64) float64 {
	if na == 0 || nb == 0 || len(a) != len(b) {
		return 0
	}
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	return dot / (na * nb)
}

func normalizeInPlace(s []float64) {
	mn, mx := math.Inf(1), math.Inf(-1)
	for _, v := range s {
		mn, mx = math.Min(mn, v), math.Max(mx, v)
	}
	if mx-mn < 1e-12 {
		for i := range s {
			s[i] = 0
		}
		return
	}
	for i := range s {
		s[i] = (s[i] - mn) / (mx - mn)
	}
}

var tokRe = regexp.MustCompile(`[A-Za-z0-9]+`)
var camelRe = regexp.MustCompile(`([a-z0-9])([A-Z])`)

func tokenize(s string) []string {
	s = camelRe.ReplaceAllString(s, "$1 $2") // split camelCase
	s = strings.ReplaceAll(s, "_", " ")       // and snake_case
	raw := tokRe.FindAllString(strings.ToLower(s), -1)
	out := raw[:0]
	for _, t := range raw {
		if len(t) > 1 { // drop single chars
			out = append(out, t)
		}
	}
	return out
}
