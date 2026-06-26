package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	"github.com/zcag/tela/backend/internal/atlas/core"
	"github.com/zcag/tela/backend/internal/atlas/engine"
)

// notifRow is the slice of a notifications row this test inspects.
type notifRow struct {
	typ      string
	subjKind string
	subjID   int64
	spaceID  sql.NullInt64
	data     map[string]any
}

func atlasNotifsFor(t *testing.T, d *sql.DB, userID int64) []notifRow {
	t.Helper()
	rows, err := d.QueryContext(context.Background(),
		`SELECT type, subject_kind, subject_id, space_id, data::text
		   FROM notifications WHERE user_id = $1 AND type = 'atlas_run' ORDER BY id`, userID)
	if err != nil {
		t.Fatalf("query notifications: %v", err)
	}
	defer rows.Close()
	var out []notifRow
	for rows.Next() {
		var r notifRow
		var raw string
		if err := rows.Scan(&r.typ, &r.subjKind, &r.subjID, &r.spaceID, &raw); err != nil {
			t.Fatalf("scan notification: %v", err)
		}
		r.data = map[string]any{}
		_ = json.Unmarshal([]byte(raw), &r.data)
		out = append(out, r)
	}
	return out
}

// finishRC builds the minimal RunContext onFinish/notify needs: a space-bound
// project, a named source, and a run carrying stats + coverage.
func finishRC(spaceID, sourceID, runID int64, status core.RunStatus, runErr string) *engine.RunContext {
	return &engine.RunContext{
		Project: &core.Project{ID: spaceID},
		Source:  &core.Source{ID: sourceID, Name: "code"},
		Run: &core.Run{ID: runID, SourceID: sourceID, Status: status, Err: runErr,
			Stats: &core.RunStats{Pages: 13}},
		Coverage: core.Coverage{Total: 30, Covered: 29, MustTotal: 10, MustCovered: 10},
	}
}

// TestAtlasNotifyRunFinish locks the run-finish notification: a done run lands an
// in-app row for the space owner with the coverage/stats summary, and a failed
// run lands the failure message. Reuses tela's notification table + emit path.
func TestAtlasNotifyRunFinish(t *testing.T) {
	d := newAPITestDB(t)
	s := New(d)
	m := s.atlas

	owner := seedUser(t, d, "atlas-owner", "pw12345678", false)
	other := seedUser(t, d, "atlas-bystander", "pw12345678", false)
	spaceID := seedSpace(t, d, "compass", "compass", owner) // owner via space_members
	seedMember(t, d, spaceID, other, "editor")              // editor is NOT a manager
	var sourceID, doneRun, failRun int64
	if err := d.QueryRowContext(context.Background(),
		`INSERT INTO atlas_sources (space_id, type, location, name)
		 VALUES ($1, 'git', 'https://example.com/acme/code.git', 'code') RETURNING id`,
		spaceID).Scan(&sourceID); err != nil {
		t.Fatalf("insert source: %v", err)
	}
	if err := d.QueryRowContext(context.Background(),
		`INSERT INTO atlas_runs (source_id, status) VALUES ($1, 'done') RETURNING id`, sourceID).Scan(&doneRun); err != nil {
		t.Fatalf("insert done run: %v", err)
	}
	if err := d.QueryRowContext(context.Background(),
		`INSERT INTO atlas_runs (source_id, status) VALUES ($1, 'failed') RETURNING id`, sourceID).Scan(&failRun); err != nil {
		t.Fatalf("insert failed run: %v", err)
	}

	ctx := context.Background()

	// --- done run ---
	m.notifyAtlasRunFinish(ctx, finishRC(spaceID, sourceID, doneRun, core.RunDone, ""), core.RunDone, nil)

	got := atlasNotifsFor(t, d, owner)
	if len(got) != 1 {
		t.Fatalf("done: want 1 notification for owner, got %d", len(got))
	}
	n := got[0]
	if n.subjKind != "space" || n.subjID != spaceID || !n.spaceID.Valid || n.spaceID.Int64 != spaceID {
		t.Fatalf("done: wrong subject/space: kind=%s subj=%d space=%v", n.subjKind, n.subjID, n.spaceID)
	}
	if n.data["title"] != "compass · code regenerated" {
		t.Fatalf("done title = %q", n.data["title"])
	}
	if n.data["summary"] != "13 pages, must-cover 100%, surface 97%" {
		t.Fatalf("done summary = %q", n.data["summary"])
	}
	if n.data["link"] != ("/spaces/" + strconv.FormatInt(spaceID, 10) + "/atlas") {
		t.Fatalf("done link = %q", n.data["link"])
	}
	// The editor (not a manager) must NOT be notified.
	if other := atlasNotifsFor(t, d, other); len(other) != 0 {
		t.Fatalf("editor got %d notifications, want 0", len(other))
	}

	// Re-firing the same terminal run must not double-notify (dedup key).
	m.notifyAtlasRunFinish(ctx, finishRC(spaceID, sourceID, doneRun, core.RunDone, ""), core.RunDone, nil)
	if again := atlasNotifsFor(t, d, owner); len(again) != 1 {
		t.Fatalf("after re-fire: want 1 (deduped), got %d", len(again))
	}

	// --- failed run ---
	m.notifyAtlasRunFinish(ctx,
		finishRC(spaceID, sourceID, failRun, core.RunFailed, "clone failed: auth required"),
		core.RunFailed, errors.New("clone failed: auth required"))

	got = atlasNotifsFor(t, d, owner)
	if len(got) != 2 {
		t.Fatalf("failed: want 2 total notifications for owner, got %d", len(got))
	}
	f := got[1]
	if f.data["title"] != "compass · code generation failed" {
		t.Fatalf("failed title = %q", f.data["title"])
	}
	if f.data["summary"] != "clone failed: auth required" {
		t.Fatalf("failed summary = %q", f.data["summary"])
	}
	if f.data["status"] != "failed" {
		t.Fatalf("failed status = %q", f.data["status"])
	}
}
