package api

import (
	"context"
	"log/slog"
	"time"

	"github.com/zcag/tela/backend/internal/atlas/engine"
)

// atlasPollInterval is the freshness poll cadence (mirrors standalone atlas's
// 1-minute refresh tick). The per-source cadence (hourly/daily/…) gates which
// sources actually fire on a given tick.
const atlasPollInterval = time.Minute

// startScheduler launches the freshness poller: a background goroutine that, on a
// 1-minute tick, fires a change-gated delta for every auto_update source whose
// cadence has elapsed. paused is the admin AI kill-switch (shared with the other
// background workers); a paused or AI-unconfigured tick is a no-op. Idempotent
// per manager; call once from New() next to the other workers.
func (m *atlasManager) startScheduler(ctx context.Context, paused func() bool) {
	m.paused = paused
	go func() {
		t := time.NewTicker(atlasPollInterval)
		defer t.Stop()
		m.pollFreshness(ctx) // run once on boot so an already-due source doesn't wait a minute
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.pollFreshness(ctx)
			}
		}
	}()
}

// pollFreshness fires every PROJECT whose freshness poll is due as of now: for
// each it refreshes all of the project's sources (a cheap no-clone HasChanges
// probe per source, then a change-gated delta only when upstream moved).
// last_refresh_at is stamped on the project regardless of outcome so the cadence
// measures from this poll. No-op when AI is unconfigured or paused.
func (m *atlasManager) pollFreshness(ctx context.Context) {
	if !m.atlasEnabled() {
		return
	}
	if m.paused != nil && m.paused() {
		return
	}
	now := time.Now()

	type due struct {
		id      int64
		cadence string
		last    string
	}
	rows, err := m.s.DB.QueryContext(ctx,
		`SELECT id, cadence, last_refresh_at FROM atlas_projects WHERE auto_update = 1`)
	if err != nil {
		slog.Error("atlas: freshness poll", "err", err)
		return
	}
	var pending []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.cadence, &d.last); err != nil {
			continue
		}
		pending = append(pending, d)
	}
	rows.Close()

	for _, d := range pending {
		var last time.Time
		if d.last != "" {
			last, _ = time.Parse(atlasTSLayout, d.last)
		}
		if !atlasIsDue(d.cadence, last, now) {
			continue
		}
		m.refreshProject(ctx, d.id)
		// Stamp regardless so the cadence measures from this poll (whether or not a
		// run started). A failed run is owed again next cadence, not next minute.
		if _, err := m.s.DB.ExecContext(ctx,
			`UPDATE atlas_projects SET last_refresh_at = tela_now() WHERE id = $1`, d.id); err != nil {
			slog.Error("atlas: stamp last_refresh_at", "project", d.id, "err", err)
		}
	}
}

// refreshProject runs the change-gated refresh for every source in a due project.
func (m *atlasManager) refreshProject(ctx context.Context, projectID int64) {
	rows, err := m.s.DB.QueryContext(ctx, `SELECT id FROM atlas_sources WHERE project_id = $1 ORDER BY id`, projectID)
	if err != nil {
		slog.Error("atlas: refresh project", "project", projectID, "err", err)
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		m.refreshSource(ctx, id)
	}
}

// refreshSource runs the change-gated delta for one due source: a cheap
// HasChanges probe (git ls-remote / jira count — no clone), then StartDelta only
// when upstream has moved. A source that never ran (empty ref) probes as changed,
// so StartDelta's full-run fallback engages.
func (m *atlasManager) refreshSource(ctx context.Context, sourceID int64) {
	src, err := m.loadSource(ctx, sourceID)
	if err != nil {
		return
	}
	has, herr := engine.HasChanges(ctx, coreSourceFrom(src), src.Ref)
	if herr != nil {
		slog.Warn("atlas: freshness probe failed", "source", sourceID, "err", herr)
		return
	}
	if !has {
		return
	}
	if _, _, ae := m.StartDelta(ctx, sourceID); ae != nil && ae.Code != "run_active" {
		slog.Warn("atlas: scheduled delta", "source", sourceID, "code", ae.Code, "msg", ae.Message)
	}
}
