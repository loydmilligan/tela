package engine

import (
	"context"
	"path/filepath"
	"time"

	"github.com/zcag/tela/backend/internal/atlas/core"
)

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
	snap, err := conn.Acquire(ctx, *rc.Source, rc.Workspace)
	if err != nil {
		return err
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
