package engine

import (
	"context"

	"golang.org/x/sync/errgroup"
)

// maxFanout caps the engine-side worker pool. The real limiter is the LLM
// client's global concurrency gate (every chat/embed acquires a slot), so this
// just keeps goroutine/memory growth sane on huge page/chunk-batch sets; it can
// be generous without actually swamping the provider.
const maxFanout = 32

// parallelN runs fn over each item index with a bounded worker pool, returning
// the first error (and canceling the rest via the derived context). n<=0 falls
// back to maxFanout; the effective worker count is capped at both maxFanout and
// the number of items. fn must only touch item-local state (its own index) —
// the callers here write distinct, pre-sized slice slots and persist their own
// row, so no shared mutation crosses workers.
func parallelN(ctx context.Context, n, items int, fn func(ctx context.Context, i int) error) error {
	if items == 0 {
		return nil
	}
	if n <= 0 || n > maxFanout {
		n = maxFanout
	}
	if n > items {
		n = items
	}
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(n)
	for i := 0; i < items; i++ {
		i := i
		g.Go(func() error {
			if err := ctx.Err(); err != nil { // a sibling already failed / ctx canceled
				return err
			}
			return fn(ctx, i)
		})
	}
	return g.Wait()
}
