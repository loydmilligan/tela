package api

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// TestAtlasCadenceLogic locks the pure freshness math: due-ness (interval elapsed
// since last; never-refreshed = due now; off/unknown = never) and the derived
// next_due string for the drift UI.
func TestAtlasCadenceLogic(t *testing.T) {
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)

	dueCases := []struct {
		name    string
		cadence string
		last    time.Time
		want    bool
	}{
		{"never refreshed is due", "daily", time.Time{}, true},
		{"daily not yet elapsed", "daily", now.Add(-3 * time.Hour), false},
		{"daily elapsed", "daily", now.Add(-25 * time.Hour), true},
		{"hourly elapsed", "hourly", now.Add(-90 * time.Minute), true},
		{"weekly not elapsed", "weekly", now.Add(-3 * 24 * time.Hour), false},
		{"off cadence never due", "", now.Add(-1000 * time.Hour), false},
		{"unknown cadence never due", "yearly", time.Time{}, false},
	}
	for _, c := range dueCases {
		if got := atlasIsDue(c.cadence, c.last, now); got != c.want {
			t.Errorf("atlasIsDue(%q, %v): got %v want %v", c.name, c.last, got, c.want)
		}
	}

	// next_due = last_refresh + interval, in tela TEXT form.
	if s := atlasNextDueStr(true, "daily", "2026-06-26 12:00:00"); s != "2026-06-27 12:00:00" {
		t.Errorf("next_due daily: got %q", s)
	}
	if s := atlasNextDueStr(true, "hourly", "2026-06-26 12:00:00"); s != "2026-06-26 13:00:00" {
		t.Errorf("next_due hourly: got %q", s)
	}
	// off / never-refreshed / auto-update-off → no next_due.
	if s := atlasNextDueStr(false, "daily", "2026-06-26 12:00:00"); s != "" {
		t.Errorf("next_due auto-off: got %q want empty", s)
	}
	if s := atlasNextDueStr(true, "", "2026-06-26 12:00:00"); s != "" {
		t.Errorf("next_due cadence-off: got %q want empty", s)
	}
	if s := atlasNextDueStr(true, "daily", ""); s != "" {
		t.Errorf("next_due never-refreshed: got %q want empty", s)
	}
}

// seedAtlasSource binds a minimal git source to a project and returns its id.
func seedAtlasSource(t *testing.T, d *sql.DB, projectID int64, location, ref string) int64 {
	t.Helper()
	var id int64
	err := d.QueryRow(
		`INSERT INTO atlas_sources (project_id, type, location, name, ref)
		 VALUES ($1,'git',$2,'repo',$3) RETURNING id`, projectID, location, ref).Scan(&id)
	if err != nil {
		t.Fatalf("seed source: %v", err)
	}
	return id
}

// seedAtlasRun inserts a run with a given status and returns its id.
func seedAtlasRun(t *testing.T, d *sql.DB, sourceID int64, status string) int64 {
	t.Helper()
	var id int64
	if err := d.QueryRow(
		`INSERT INTO atlas_runs (source_id, kind, status) VALUES ($1,'full',$2) RETURNING id`,
		sourceID, status).Scan(&id); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	return id
}

// TestAtlasLastDoneBaseline verifies the baseline/fromRef judgment: the baseline
// is the source's most recent done run and fromRef is its pinned ref; a source
// with no done run (or no pinned ref) has no baseline → a full run is owed.
func TestAtlasLastDoneBaseline(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	owner := seedUser(t, d, "alice", "alicepw12", false)
	space := seedSpace(t, d, "Repo Docs", "repo-docs", owner)
	pid := seedAtlasProject(t, d, "Repo Docs", accountUser, owner, space, 0)
	ctx := context.Background()

	src := seedAtlasSource(t, d, pid, "https://example.com/repo.git", "abc123")

	// No runs yet → no baseline.
	if id, ref := srv.atlas.lastDoneBaseline(ctx, src, "abc123"); id != 0 || ref != "" {
		t.Fatalf("no runs: want (0,\"\"), got (%d,%q)", id, ref)
	}

	// A failed + a running run still leave no usable baseline.
	seedAtlasRun(t, d, src, "failed")
	seedAtlasRun(t, d, src, "running")
	if id, _ := srv.atlas.lastDoneBaseline(ctx, src, "abc123"); id != 0 {
		t.Fatalf("non-done runs: want baseline 0, got %d", id)
	}

	// The most recent done run becomes the baseline; fromRef is the pinned ref.
	seedAtlasRun(t, d, src, "done")
	done2 := seedAtlasRun(t, d, src, "done")
	seedAtlasRun(t, d, src, "failed") // a later failed run must not override the baseline
	if id, ref := srv.atlas.lastDoneBaseline(ctx, src, "abc123"); id != done2 || ref != "abc123" {
		t.Fatalf("baseline: want (%d,\"abc123\"), got (%d,%q)", done2, id, ref)
	}

	// Empty pinned ref → no usable baseline even with a done run.
	if id, ref := srv.atlas.lastDoneBaseline(ctx, src, ""); id != 0 || ref != "" {
		t.Fatalf("empty ref: want (0,\"\"), got (%d,%q)", id, ref)
	}
}

// TestAtlasStartDelta_FallbackToFull checks that StartDelta with no prior done
// run starts a full run (StartRun semantics) rather than a delta. AI is enabled
// via env so the enablement guard passes; the source location is unreachable so
// the spawned background run fails fast — only the synchronous decision (a
// kind='full' run is created) is asserted.
func TestAtlasStartDelta_FallbackToFull(t *testing.T) {
	t.Setenv("TELA_LLM_URL", "http://127.0.0.1:1/v1")
	t.Setenv("TELA_RAG_EMBED_URL", "http://127.0.0.1:1")
	d := newAPITestDB(t)
	srv := New(d)
	owner := seedUser(t, d, "alice", "alicepw12", false)
	space := seedSpace(t, d, "Repo Docs", "repo-docs", owner)
	pid := seedAtlasProject(t, d, "Repo Docs", accountUser, owner, space, 0)
	// Unreachable local path → the background run fails immediately at acquire.
	src := seedAtlasSource(t, d, pid, "/nonexistent/atlas-test-repo", "")

	runID, changed, ae := srv.atlas.StartDelta(context.Background(), src)
	if ae != nil {
		t.Fatalf("StartDelta: unexpected apiErr %+v", ae)
	}
	if !changed || runID == 0 {
		t.Fatalf("StartDelta: want changed run, got changed=%v runID=%d", changed, runID)
	}
	var kind string
	var baseline int64
	if err := d.QueryRow(`SELECT kind, baseline_id FROM atlas_runs WHERE id=$1`, runID).Scan(&kind, &baseline); err != nil {
		t.Fatalf("load run: %v", err)
	}
	if kind != "full" || baseline != 0 {
		t.Fatalf("fallback run: want kind=full baseline=0, got kind=%q baseline=%d", kind, baseline)
	}
}

// TestAtlasStartDelta_AIUnavailable confirms the enablement guard fires (no LLM /
// embedder configured) before any source work.
func TestAtlasStartDelta_AIUnavailable(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	owner := seedUser(t, d, "alice", "alicepw12", false)
	space := seedSpace(t, d, "Repo Docs", "repo-docs", owner)
	pid := seedAtlasProject(t, d, "Repo Docs", accountUser, owner, space, 0)
	src := seedAtlasSource(t, d, pid, "https://example.com/repo.git", "")

	if _, _, ae := srv.atlas.StartDelta(context.Background(), src); ae == nil || ae.Code != "ai_unavailable" {
		t.Fatalf("want ai_unavailable apiErr, got %+v", ae)
	}
}
