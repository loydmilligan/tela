package engine

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/zcag/tela/backend/internal/atlas/core"
)

const (
	embedBatch  = 16
	embedFanout = 8 // batches in flight; the LLM gate is the real limiter
)

// embedStage turns every chunk into a vector. Batched for throughput; the client
// transparently retries and splits oversized inputs, so every chunk gets a
// vector (none silently dropped). Vectors are persisted for durability and kept
// in-memory for the index stage.
type embedStage struct{}

func (embedStage) Name() core.StageName { return core.StageEmbed }

func (embedStage) Run(ctx context.Context, rc *RunContext) error {
	chunks := rc.Art.Chunks

	// Partition: only chunks without a vector need embedding. On a full run that's
	// all of them; on a delta run the copied-forward chunks already carry their
	// baseline vectors, so this skips them automatically. *todo[i] indexes back
	// into chunks so the embedded vector lands on the right chunk.
	var todo []*core.Chunk
	for i := range chunks {
		if len(chunks[i].Vector) == 0 {
			todo = append(todo, &chunks[i])
		}
	}
	if len(todo) == 0 {
		rc.Info("embed skipped: all %d chunks reused baseline vectors", len(chunks))
		return nil
	}

	// Partition into fixed-size batches and embed them concurrently. Each batch
	// writes only its own [start,end) slots of todo (distinct, pre-sized chunk
	// pointers), so workers never race on shared state; fallbacks is atomic and
	// progress goes through the atomic StepDone counter.
	type batch struct{ start, end int }
	var batches []batch
	for start := 0; start < len(todo); start += embedBatch {
		batches = append(batches, batch{start, min(start+embedBatch, len(todo))})
	}
	var fallbacks atomic.Int64
	rc.resetProgress()
	err := parallelN(ctx, embedFanout, len(batches), func(ctx context.Context, b int) error {
		start, end := batches[b].start, batches[b].end
		inputs := make([]string, end-start)
		for i := start; i < end; i++ {
			inputs[i-start] = embedText(*todo[i])
		}
		vecs, zeroed, err := rc.LLM.Embed(ctx, inputs)
		if err != nil {
			return err
		}
		for i := start; i < end; i++ {
			todo[i].Vector = vecs[i-start]
		}
		fallbacks.Add(int64(zeroed))
		rc.StepDone(len(batches), "embedding chunks")
		return nil
	})
	if err != nil {
		return err
	}

	// Persist only the newly-embedded chunks (copied ones are already stored).
	newly := make([]core.Chunk, len(todo))
	for i, c := range todo {
		newly[i] = *c
	}
	if err := rc.Store.SaveVectors(newly); err != nil {
		return err
	}
	dim := len(todo[0].Vector)
	if fb := fallbacks.Load(); fb > 0 {
		rc.Warn("%d oversized chunk(s) used a zero vector (still keyword-retrievable)", fb)
	}
	rc.Info("embedded %d chunks (%d-d)%s", len(todo), dim, reuseNote(len(chunks)-len(todo)))
	return nil
}

// reuseNote annotates the embed summary when some chunks reused baseline vectors.
func reuseNote(reused int) string {
	if reused == 0 {
		return ""
	}
	return fmt.Sprintf(" · reused %d", reused)
}

// embedText prefixes the chunk with its location so the embedding captures where
// it lives, not just the code — improves retrieval for "where is X" queries.
func embedText(c core.Chunk) string {
	loc := c.File
	if c.Symbol != "" {
		loc += " · " + c.Symbol
	}
	return loc + "\n" + c.Text
}
