package api

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/zcag/tela/backend/internal/atlas/core"
	"github.com/zcag/tela/backend/internal/atlas/engine"
)

// atlasSourceRow is the persisted binding the executor needs to drive a run: the
// source's scope + the project it belongs to (which resolves the output space)
// and the optional reusable credential it authenticates with.
type atlasSourceRow struct {
	ID        int64
	ProjectID int64
	Type      string
	Location  string
	Name      string
	Ref       string
	Branch    string
	Subpath   string
	Include   string
	Exclude   string
	CredID    *int64
}

const atlasSourceCols = `id, project_id, type, location, name, ref, branch, subpath, include, exclude, cred_id`

func scanAtlasSource(sc interface{ Scan(...any) error }) (atlasSourceRow, error) {
	var r atlasSourceRow
	var cred sql.NullInt64
	err := sc.Scan(&r.ID, &r.ProjectID, &r.Type, &r.Location, &r.Name, &r.Ref, &r.Branch, &r.Subpath, &r.Include, &r.Exclude, &cred)
	if cred.Valid {
		r.CredID = &cred.Int64
	}
	return r, err
}

func (m *atlasManager) loadSource(ctx context.Context, sourceID int64) (atlasSourceRow, error) {
	row := m.s.DB.QueryRowContext(ctx, `SELECT `+atlasSourceCols+` FROM atlas_sources WHERE id = $1`, sourceID)
	return scanAtlasSource(row)
}

// StartRun creates a full run for a source and drives it in the background.
// Authorization is the caller's responsibility (gate the source's project with
// atlasProjectManageErr before calling). One active run per source: a second
// start is rejected.
func (m *atlasManager) StartRun(ctx context.Context, sourceID int64) (int64, *apiErr) {
	if !m.atlasEnabled() {
		return 0, &apiErr{http.StatusServiceUnavailable, "ai_unavailable", "atlas needs both an embedder (TELA_RAG_EMBED_URL) and a chat model (TELA_LLM_URL)"}
	}
	m.mu.Lock()
	if _, busy := m.active[sourceID]; busy {
		m.mu.Unlock()
		return 0, &apiErr{http.StatusConflict, "run_active", "a run is already in progress for this source"}
	}
	m.mu.Unlock()

	src, err := m.loadSource(ctx, sourceID)
	if err == sql.ErrNoRows {
		return 0, &apiErr{http.StatusNotFound, "not_found", "source not found"}
	} else if err != nil {
		return 0, &apiErr{http.StatusInternalServerError, "internal", "lookup source failed"}
	}

	var runID int64
	if err := m.s.DB.QueryRowContext(ctx,
		`INSERT INTO atlas_runs (source_id, kind, status) VALUES ($1, 'full', 'pending') RETURNING id`,
		sourceID).Scan(&runID); err != nil {
		return 0, &apiErr{http.StatusInternalServerError, "internal", "create run failed"}
	}

	m.spawn(src, runID, "")
	return runID, nil
}

// buildRunContext assembles the engine inputs for a run. It resolves the
// source's project → output destination (creating the output space on the first
// run if it doesn't exist yet), binds the publisher to that space + top-dir, and
// pins the instance chat/embed model. core.Project.ID carries the tela project id
// (the notify path resolves the project's owner/managers from it).
func (m *atlasManager) buildRunContext(ctx context.Context, src atlasSourceRow, run *core.Run, workspace string) (*engine.RunContext, error) {
	proj, err := m.loadProject(ctx, src.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("load project %d: %w", src.ProjectID, err)
	}
	spaceID, parentPageID, ae := m.s.ensureOutputSpace(ctx, proj)
	if ae != nil {
		return nil, fmt.Errorf("ensure output space: %s", ae.Message)
	}
	client := m.newLLMClient()
	coreProj := &core.Project{ID: proj.ID, Name: proj.Name, Model: atlasModelCfg(m.s.rag.EmbedModel())}
	// Resolve the bound credential into the transient secret fields (Location stays
	// clean; the git connector injects auth at command time). jira reads
	// SecretValue + SecretMeta["email"]; git reads SecretValue + SecretMeta["username"].
	coreSrc := m.resolveCoreSource(ctx, src)
	return &engine.RunContext{
		Project:   coreProj,
		Source:    &coreSrc,
		Run:       run,
		Workspace: workspace,
		Store:     m.store,
		LLM:       client,
		Publisher: newAtlasPublisher(m.s.DB, m.s.rag.QueueReindex, spaceID, parentPageID),
		OnFinish:  m.onFinish,
	}, nil
}

// spawn drives one run to completion in its own goroutine. fromStage == "" runs
// the full pipeline; a non-empty fromStage resumes (re-acquire + rehydrate +
// RunFrom), used by ResumeDangling after a restart.
func (m *atlasManager) spawn(src atlasSourceRow, runID int64, fromStage core.StageName) {
	runCtx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.active[src.ID] = cancel
	m.mu.Unlock()

	go func() {
		defer func() {
			m.mu.Lock()
			delete(m.active, src.ID)
			m.mu.Unlock()
			cancel()
			if r := recover(); r != nil {
				slog.Error("atlas: run panicked", "run", runID, "panic", r)
				_, _ = m.s.DB.Exec(`UPDATE atlas_runs SET status='failed', err=$2, finished_at=tela_now() WHERE id=$1`,
					runID, fmt.Sprintf("panic: %v", r))
			}
		}()

		// Queue gate: block for a global run slot so at most TELA_ATLAS_MAX_CONCURRENT_RUNS
		// execute at once. While waiting the run stays status='pending' (UI: "Queued").
		// Cancellable mid-queue (a user cancel, or process shutdown via runCtx).
		select {
		case m.runSlots <- struct{}{}:
			defer func() { <-m.runSlots }()
		case <-runCtx.Done():
			_, _ = m.s.DB.Exec(`UPDATE atlas_runs SET status='canceled', finished_at=tela_now() WHERE id=$1 AND status='pending'`, runID)
			return
		}
		// Slot granted → flip out of the Queued state.
		_, _ = m.s.DB.Exec(`UPDATE atlas_runs SET status='running' WHERE id=$1 AND status='pending'`, runID)

		workspace, err := os.MkdirTemp(atlasWorkRoot(), fmt.Sprintf("atlas-run-%d-", runID))
		if err != nil {
			slog.Error("atlas: workspace", "run", runID, "err", err)
			_, _ = m.s.DB.Exec(`UPDATE atlas_runs SET status='failed', err=$2, finished_at=tela_now() WHERE id=$1`, runID, err.Error())
			return
		}
		defer os.RemoveAll(workspace)

		run, err := m.store.GetRun(runID)
		if err != nil || run == nil {
			slog.Error("atlas: load run", "run", runID, "err", err)
			return
		}
		rc, err := m.buildRunContext(runCtx, src, run, workspace)
		if err != nil {
			slog.Error("atlas: build run context", "run", runID, "err", err)
			_, _ = m.s.DB.Exec(`UPDATE atlas_runs SET status='failed', err=$2, finished_at=tela_now() WHERE id=$1`, runID, err.Error())
			return
		}
		emit := func(e core.Event) { m.hub.publish(e) }
		// Publish a terminal marker so live SSE subscribers close cleanly when the
		// run ends (the engine emits no terminal event of its own).
		defer m.hub.publish(core.Event{RunID: runID, Stage: atlasEndStage, Level: core.LevelInfo, Msg: "finished"})

		if fromStage == "" {
			_ = engine.Default().Run(runCtx, rc, emit)
			return
		}
		// Resume: re-materialize the source on disk, rehydrate persisted artifacts
		// + the retriever, then continue from the persisted stage.
		if err := engine.Acquire(runCtx, rc); err != nil {
			slog.Error("atlas: resume acquire", "run", runID, "err", err)
			return
		}
		if err := engine.Rehydrate(runCtx, rc); err != nil {
			slog.Error("atlas: resume rehydrate", "run", runID, "err", err)
			return
		}
		_ = engine.Default().RunFrom(runCtx, rc, fromStage, emit)
	}()
}

// onFinish meters the run's chat token usage into tela's AI-usage ledger and
// notifies the space's managers that the run finished. Embeddings are already
// metered per-call by tela's rag recorder (s.rag.Embed), so only chat is
// recorded here to avoid double counting. Both steps are best-effort: a metering
// or notify failure must never affect the run (it's already terminal).
func (m *atlasManager) onFinish(rc *engine.RunContext, status core.RunStatus, runErr error) {
	if rc.Run != nil && rc.Run.Stats != nil {
		u := rc.Run.Stats.Usage
		m.s.recordAIUsage("chat", rc.Project.Model.ChatModel, int(u.PromptTokens), int(u.CompletionTokens), 0)
	}
	m.notifyAtlasRunFinish(context.Background(), rc, status, runErr)
}

// ResumeDangling picks up runs left 'running' by a previous process and continues
// them from their persisted stage. Called once at boot (mirrors atlas's
// ResumeDangling). Best-effort: a run that can't be resumed is left as-is.
func (m *atlasManager) ResumeDangling(ctx context.Context) {
	rows, err := m.s.DB.QueryContext(ctx,
		`SELECT r.id, r.stage, `+atlasSourceColsPrefixed("s")+`
		   FROM atlas_runs r JOIN atlas_sources s ON s.id = r.source_id
		  WHERE r.status = 'running'`)
	if err != nil {
		slog.Error("atlas: resume scan", "err", err)
		return
	}
	defer rows.Close()
	type pending struct {
		src   atlasSourceRow
		runID int64
		stage core.StageName
	}
	var todo []pending
	for rows.Next() {
		var runID int64
		var stage string
		var src atlasSourceRow
		var cred sql.NullInt64
		if err := rows.Scan(&runID, &stage, &src.ID, &src.ProjectID, &src.Type, &src.Location, &src.Name, &src.Ref, &src.Branch, &src.Subpath, &src.Include, &src.Exclude, &cred); err != nil {
			slog.Error("atlas: resume row", "err", err)
			continue
		}
		if cred.Valid {
			src.CredID = &cred.Int64
		}
		todo = append(todo, pending{src, runID, core.StageName(stage)})
	}
	for _, p := range todo {
		if !m.atlasEnabled() {
			slog.Warn("atlas: cannot resume run (AI unconfigured)", "run", p.runID)
			continue
		}
		slog.Info("atlas: resuming dangling run", "run", p.runID, "from", p.stage)
		m.spawn(p.src, p.runID, p.stage)
	}
}

// atlasSourceColsPrefixed returns the source columns qualified with a table
// alias, for the resume join (keeps column order in sync with scanAtlasSource).
func atlasSourceColsPrefixed(alias string) string {
	return alias + ".id, " + alias + ".project_id, " + alias + ".type, " +
		alias + ".location, " + alias + ".name, " + alias + ".ref, " + alias + ".branch, " +
		alias + ".subpath, " + alias + ".include, " + alias + ".exclude, " + alias + ".cred_id"
}

// atlasWorkRoot is where run workspaces (repo clones) are materialized. Override
// with TELA_ATLAS_WORKDIR; defaults to the OS temp dir.
func atlasWorkRoot() string {
	if v := os.Getenv("TELA_ATLAS_WORKDIR"); v != "" {
		return v
	}
	return os.TempDir()
}
