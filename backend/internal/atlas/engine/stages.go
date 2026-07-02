package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zcag/tela/backend/internal/atlas/core"
	"github.com/zcag/tela/backend/internal/atlas/source"
)

// acquireTimeout bounds the source-materialization stage (git clone / jira
// fetch) so a wedged or unreachable source fails the run in minutes instead of
// hanging until the 4h run watchdog. Generous by default to accommodate large
// repos/projects on slow links; override with TELA_ATLAS_ACQUIRE_TIMEOUT (a Go
// duration, e.g. "90m") for giant monorepos.
func acquireTimeout() time.Duration {
	if v := os.Getenv("TELA_ATLAS_ACQUIRE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return 30 * time.Minute
}

// acquireStage is the first real stage: it materializes the source into the run
// workspace and pins the exact ref. The source-specific work (clone, rev pin)
// lives in the connector, selected by Source.Type; this wrapper keeps the
// RunContext/store/emit concerns — set Source.Ref, persist it, emit progress.
type acquireStage struct{}

func (acquireStage) Name() core.StageName { return core.StageAcquire }

func (acquireStage) Run(ctx context.Context, rc *RunContext) error {
	conn, err := connectorFor(rc.Source.Type)
	if err != nil {
		return err
	}
	// Auth (private repos / Jira) travels via the transient Source.SecretValue/
	// SecretMeta fields, which the executor pre-resolves before the run — keeping
	// auth out of the Connector interface. Phase 1 (public git) needs none; the
	// secret store lands in Phase 5.
	if rc.Source.Branch != "" {
		rc.Info("cloning %s (branch %s)", rc.Source.Location, rc.Source.Branch)
	} else {
		rc.Info("cloning %s", rc.Source.Location)
	}
	// Bound acquire with a generous deadline. We run it in a goroutine and select
	// so the stage fails on timeout even if the connector is stuck in a call that
	// ignores ctx cancellation (a hung DNS resolve / half-open TCP read — the
	// exact wedge we saw: idle goroutine, no socket, no progress). The wedged
	// goroutine may leak, but the run fails cleanly and frees its slot.
	acqCtx, cancel := context.WithTimeout(ctx, acquireTimeout())
	defer cancel()
	type acqResult struct {
		snap source.Snapshot
		err  error
	}
	done := make(chan acqResult, 1)
	go func() {
		s, e := conn.Acquire(acqCtx, *rc.Source, rc.Workspace)
		done <- acqResult{s, e}
	}()
	var snap source.Snapshot
	select {
	case <-acqCtx.Done():
		if ctx.Err() != nil {
			return ctx.Err() // whole run canceled/timed out — propagate as-is
		}
		return fmt.Errorf("acquire of %s exceeded %s (source unreachable, too large, or wedged; raise TELA_ATLAS_ACQUIRE_TIMEOUT)", rc.Source.Location, acquireTimeout())
	case r := <-done:
		if r.err != nil {
			return r.err
		}
		snap = r.snap
	}
	rc.Snapshot = snap
	rc.Source.Ref = snap.Ref
	_ = rc.Store.SetSourceRef(rc.Source.ID, snap.Ref)
	rc.Info("checked out %s at %s", filepath.Base(rc.Source.Location), snap.Ref[:min(12, len(snap.Ref))])
	return nil
}

// --- stub stages -----------------------------------------------------------
// Placeholders that emit a believable progress sweep so the pipeline runs
// end-to-end and the progress UX is exercised. Each is replaced by a real
// implementation as we build that stage.

type stubStage struct {
	name core.StageName
	done string
}

func stub(name core.StageName, done string) Stage { return &stubStage{name, done} }

func (s *stubStage) Name() core.StageName { return s.name }

func (s *stubStage) Run(ctx context.Context, rc *RunContext) error {
	const n = 5
	for i := 1; i <= n; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		rc.Step(i, n, "%s (stub)", s.name)
		time.Sleep(60 * time.Millisecond)
	}
	rc.Info("%s: %d %s (stub)", s.name, n, s.done)
	return nil
}
