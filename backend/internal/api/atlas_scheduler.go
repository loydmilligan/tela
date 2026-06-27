package api

import (
	"context"
	"log/slog"
	"time"

	"github.com/zcag/tela/backend/internal/atlas/engine"
)

// The scheduler runs two decoupled passes off a 1-minute base tick:
//   - detection: a cheap no-clone drift probe per source (git ls-remote / jira
//     count), at most every atlasDetectInterval, recording staleness for the UI
//     + the regen gate. No generation.
//   - regen: a delta for each STALE source, on the project's (slower) cadence.
const (
	atlasPollInterval   = time.Minute
	atlasDetectInterval = 15 * time.Minute
)

// startScheduler launches the freshness worker. paused is the admin AI
// kill-switch (shared with the other background workers); a paused or
// AI-unconfigured tick is a no-op. Idempotent per manager; call once from New().
func (m *atlasManager) startScheduler(ctx context.Context, paused func() bool) {
	m.paused = paused
	go func() {
		t := time.NewTicker(atlasPollInterval)
		defer t.Stop()
		m.tick(ctx) // run once on boot so an already-due project doesn't wait a minute
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.tick(ctx)
			}
		}
	}()
}

// tick runs detection then regeneration (detection first so a freshly-detected
// drift can regen in the same tick). No-op when AI is unconfigured or paused.
func (m *atlasManager) tick(ctx context.Context) {
	if !m.atlasEnabled() {
		return
	}
	if m.paused != nil && m.paused() {
		return
	}
	m.detectStaleness(ctx)
	m.pollRegen(ctx)
}

// detectStaleness runs the cheap no-clone drift probe for every source whose
// probe is due (never checked, or older than atlasDetectInterval), recording
// per-source staleness. This is pure drift-tracking — no clone, no LLM — and it
// runs for ALL sources, including those in Manual projects (so they can show
// "behind" and nudge a manual run).
func (m *atlasManager) detectStaleness(ctx context.Context) {
	now := time.Now()
	rows, err := m.s.DB.QueryContext(ctx, `SELECT id, upstream_checked_at FROM atlas_sources`)
	if err != nil {
		slog.Error("atlas: detection query", "err", err)
		return
	}
	var dueIDs []int64
	for rows.Next() {
		var id int64
		var checked string
		if err := rows.Scan(&id, &checked); err != nil {
			continue
		}
		var last time.Time
		if checked != "" {
			last, _ = time.Parse(atlasTSLayout, checked)
		}
		if last.IsZero() || !now.Before(last.Add(atlasDetectInterval)) {
			dueIDs = append(dueIDs, id)
		}
	}
	rows.Close()
	for _, id := range dueIDs {
		m.probeStaleness(ctx, id)
	}
}

// probeStaleness probes one source and records its drift: stale_since is set the
// first time a probe sees upstream past `ref` (preserved across probes until a
// regen clears it), cleared when a probe sees them back in sync. checked_at is
// always stamped so a broken source isn't re-hammered every minute.
func (m *atlasManager) probeStaleness(ctx context.Context, sourceID int64) {
	src, err := m.loadSource(ctx, sourceID)
	if err != nil {
		return
	}
	has, herr := engine.HasChanges(ctx, m.resolveCoreSource(ctx, src), src.Ref)
	if herr != nil {
		slog.Warn("atlas: detection probe failed", "source", sourceID, "err", herr)
		_, _ = m.s.DB.ExecContext(ctx, `UPDATE atlas_sources SET upstream_checked_at = tela_now() WHERE id = $1`, sourceID)
		return
	}
	if has {
		_, _ = m.s.DB.ExecContext(ctx,
			`UPDATE atlas_sources
			    SET upstream_checked_at = tela_now(),
			        stale_since = CASE WHEN stale_since = '' THEN tela_now() ELSE stale_since END
			  WHERE id = $1`, sourceID)
	} else {
		_, _ = m.s.DB.ExecContext(ctx,
			`UPDATE atlas_sources SET upstream_checked_at = tela_now(), stale_since = '' WHERE id = $1`, sourceID)
	}
}

// pollRegen regenerates, on each auto_update project's cadence, only the sources
// detection has flagged stale. last_refresh_at is stamped on the project whether
// or not anything regenerated, so the cadence measures from this poll.
func (m *atlasManager) pollRegen(ctx context.Context) {
	now := time.Now()
	type due struct {
		id      int64
		cadence string
		last    string
	}
	rows, err := m.s.DB.QueryContext(ctx,
		`SELECT id, cadence, last_refresh_at FROM atlas_projects WHERE auto_update = 1`)
	if err != nil {
		slog.Error("atlas: regen poll", "err", err)
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
		m.regenProject(ctx, d.id)
		if _, err := m.s.DB.ExecContext(ctx,
			`UPDATE atlas_projects SET last_refresh_at = tela_now() WHERE id = $1`, d.id); err != nil {
			slog.Error("atlas: stamp last_refresh_at", "project", d.id, "err", err)
		}
	}
}

// regenProject starts a delta for each stale source in the project. StartDelta
// clones HEAD (so it catches everything up to now) and, on success, advances
// `ref` + clears stale_since.
func (m *atlasManager) regenProject(ctx context.Context, projectID int64) {
	rows, err := m.s.DB.QueryContext(ctx,
		`SELECT id FROM atlas_sources WHERE project_id = $1 AND stale_since <> '' ORDER BY id`, projectID)
	if err != nil {
		slog.Error("atlas: regen project", "project", projectID, "err", err)
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
		if _, _, ae := m.StartDelta(ctx, id); ae != nil && ae.Code != "run_active" {
			slog.Warn("atlas: scheduled delta", "source", id, "code", ae.Code, "msg", ae.Message)
		}
	}
}
