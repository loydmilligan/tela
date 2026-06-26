package store

import (
	"database/sql"
	"testing"
	"time"

	"github.com/zcag/tela/backend/internal/atlas/core"
	"github.com/zcag/tela/backend/internal/testdb"
)

// fixture inserts a space + atlas_sources row + a pending atlas_runs row via raw
// SQL and returns (sourceID, runID).
func fixture(t *testing.T, d *sql.DB) (int64, int64) {
	t.Helper()
	var spaceID, sourceID, runID int64
	if err := d.QueryRow(
		`INSERT INTO spaces(name,slug) VALUES('Atlas Test','atlas-test') RETURNING id`).Scan(&spaceID); err != nil {
		t.Fatalf("insert space: %v", err)
	}
	if err := d.QueryRow(
		`INSERT INTO atlas_sources(space_id,type,location) VALUES($1,'git','https://example.com/repo.git') RETURNING id`,
		spaceID).Scan(&sourceID); err != nil {
		t.Fatalf("insert source: %v", err)
	}
	if err := d.QueryRow(
		`INSERT INTO atlas_runs(source_id,status) VALUES($1,'pending') RETURNING id`, sourceID).Scan(&runID); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	return sourceID, runID
}

// vec1024 builds a deterministic, distinctive 1024-d vector seeded by base so a
// round-trip can be asserted exactly.
func vec1024(base float32) []float32 {
	v := make([]float32, 1024)
	for i := range v {
		v[i] = base + float32(i)*0.001
	}
	return v
}

func TestStoreRoundTrip(t *testing.T) {
	d := testdb.New(t)
	st := New(d)
	sourceID, runID := fixture(t, d)

	// --- files ---
	files := []core.File{
		{Path: "main.go", Lang: core.LangGo, Size: 120, Lines: 10, Hash: "h1"},
		{Path: "util.go", Lang: core.LangGo, Size: 80, Lines: 6, Hash: "h2"},
	}
	if err := st.SaveFiles(runID, files); err != nil {
		t.Fatalf("SaveFiles: %v", err)
	}
	for i, f := range files {
		if f.ID == 0 {
			t.Fatalf("SaveFiles: file %d id not back-filled", i)
		}
	}
	gotFiles, err := st.RunFiles(runID)
	if err != nil {
		t.Fatalf("RunFiles: %v", err)
	}
	if len(gotFiles) != 2 || gotFiles[0].Path != "main.go" || gotFiles[0].Hash != "h1" || gotFiles[1].Lines != 6 {
		t.Fatalf("RunFiles mismatch: %+v", gotFiles)
	}

	// --- spine ---
	spine := []core.SpineItem{
		{Kind: core.KindRoute, Name: "GET /a", File: "main.go", Line: 3, Detail: "handler"},
		{Kind: core.KindEntrypoint, Name: "main", File: "main.go", Line: 1},
	}
	if err := st.SaveSpine(runID, spine); err != nil {
		t.Fatalf("SaveSpine: %v", err)
	}
	for i, it := range spine {
		if it.ID == 0 {
			t.Fatalf("SaveSpine: item %d id not back-filled", i)
		}
	}
	gotSpine, err := st.RunSpine(runID)
	if err != nil {
		t.Fatalf("RunSpine: %v", err)
	}
	// ordered by kind,name → entrypoint("main") before route("GET /a")
	if len(gotSpine) != 2 || gotSpine[0].Kind != core.KindEntrypoint || gotSpine[1].Kind != core.KindRoute {
		t.Fatalf("RunSpine order/contents wrong: %+v", gotSpine)
	}

	// --- chunks + vectors ---
	chunks := []core.Chunk{
		{File: "main.go", StartLine: 1, EndLine: 5, Kind: core.ChunkDecl, Symbol: "main", Text: "func main(){}"},
		{File: "util.go", StartLine: 1, EndLine: 3, Kind: core.ChunkFile, Text: "package util"},
	}
	if err := st.SaveChunks(runID, chunks); err != nil {
		t.Fatalf("SaveChunks: %v", err)
	}
	for i, c := range chunks {
		if c.ID == 0 {
			t.Fatalf("SaveChunks: chunk %d id not back-filled", i)
		}
	}
	// attach real 1024-d vectors and persist them by id
	chunks[0].Vector = vec1024(0.5)
	chunks[1].Vector = vec1024(-0.25)
	if err := st.SaveVectors(chunks); err != nil {
		t.Fatalf("SaveVectors: %v", err)
	}

	gotChunks, err := st.RunChunksWithVectors(runID)
	if err != nil {
		t.Fatalf("RunChunksWithVectors: %v", err)
	}
	if len(gotChunks) != 2 {
		t.Fatalf("RunChunksWithVectors: want 2, got %d", len(gotChunks))
	}
	// stable order by id → same order they were inserted
	if gotChunks[0].ID != chunks[0].ID || gotChunks[1].ID != chunks[1].ID {
		t.Fatalf("RunChunksWithVectors order: %+v", []int64{gotChunks[0].ID, gotChunks[1].ID})
	}
	assertVecEq(t, "chunk0", gotChunks[0].Vector, chunks[0].Vector)
	assertVecEq(t, "chunk1", gotChunks[1].Vector, chunks[1].Vector)
	if gotChunks[0].Symbol != "main" || gotChunks[1].Kind != core.ChunkFile {
		t.Fatalf("RunChunksWithVectors fields wrong: %+v", gotChunks)
	}

	// --- pages ---
	pages := []core.Page{
		{Order: 0, Kind: core.PageNarrative, Title: "Overview", Slug: "overview", Summary: "s0",
			Topics: []string{"intro", "arch"}},
		{Order: 1, Kind: core.PageReference, Title: "Routes", Slug: "routes", Summary: "s1",
			SpineKinds: []core.SpineKind{core.KindRoute, core.KindEntrypoint}},
	}
	if err := st.SavePages(runID, pages); err != nil {
		t.Fatalf("SavePages: %v", err)
	}
	for i, p := range pages {
		if p.ID == 0 {
			t.Fatalf("SavePages: page %d id not back-filled", i)
		}
	}
	if err := st.UpdatePageBody(pages[0].ID, "# Overview body"); err != nil {
		t.Fatalf("UpdatePageBody: %v", err)
	}
	gotPages, err := st.RunPagesFull(runID)
	if err != nil {
		t.Fatalf("RunPagesFull: %v", err)
	}
	if len(gotPages) != 2 {
		t.Fatalf("RunPagesFull: want 2, got %d", len(gotPages))
	}
	if gotPages[0].Body != "# Overview body" {
		t.Fatalf("UpdatePageBody not persisted: %q", gotPages[0].Body)
	}
	if len(gotPages[0].Topics) != 2 || gotPages[0].Topics[0] != "intro" {
		t.Fatalf("page topics round-trip wrong: %+v", gotPages[0].Topics)
	}
	if len(gotPages[1].SpineKinds) != 2 || gotPages[1].SpineKinds[0] != core.KindRoute {
		t.Fatalf("page spine-kinds round-trip wrong: %+v", gotPages[1].SpineKinds)
	}

	// --- coverage + stats → GetRun ---
	cov := core.Coverage{Total: 10, Covered: 8, MustTotal: 5, MustCovered: 5, Citations: 12}
	if err := st.SaveRunCoverage(runID, cov); err != nil {
		t.Fatalf("SaveRunCoverage: %v", err)
	}
	stats := core.RunStats{Files: 2, Chunks: 2, Pages: 2, ChatModel: "m", DurationSec: 1.5}
	if err := st.SaveRunStats(runID, stats); err != nil {
		t.Fatalf("SaveRunStats: %v", err)
	}

	// --- events + run lifecycle ---
	ev := core.Event{RunID: runID, Stage: core.StageChunk, Level: core.LevelInfo, Msg: "chunked", Cur: 2, Total: 2, At: time.Now()}
	if err := st.AppendEvent(ev); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	run, err := st.GetRun(runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.SourceID != sourceID || run.Status != core.RunPending || run.Kind != core.RunFull {
		t.Fatalf("GetRun base fields wrong: %+v", run)
	}
	if run.Coverage == nil || run.Coverage.Covered != 8 || run.Coverage.Citations != 12 {
		t.Fatalf("GetRun coverage decode wrong: %+v", run.Coverage)
	}
	if run.Stats == nil || run.Stats.Files != 2 || run.Stats.ChatModel != "m" {
		t.Fatalf("GetRun stats decode wrong: %+v", run.Stats)
	}
	if run.StartedAt.IsZero() {
		t.Fatalf("GetRun started_at should be set (default tela_now)")
	}

	// UpdateRun: transition to done with a finished_at
	run.Status = core.RunDone
	run.Stage = core.StagePublish
	run.FinishedAt = time.Now()
	if err := st.UpdateRun(run); err != nil {
		t.Fatalf("UpdateRun: %v", err)
	}
	again, err := st.GetRun(runID)
	if err != nil {
		t.Fatalf("GetRun after update: %v", err)
	}
	if again.Status != core.RunDone || again.Stage != core.StagePublish || again.FinishedAt.IsZero() {
		t.Fatalf("UpdateRun not persisted: %+v", again)
	}

	// --- SetSourceRef ---
	if err := st.SetSourceRef(sourceID, "deadbeef"); err != nil {
		t.Fatalf("SetSourceRef: %v", err)
	}
	var ref string
	if err := d.QueryRow(`SELECT ref FROM atlas_sources WHERE id=$1`, sourceID).Scan(&ref); err != nil {
		t.Fatalf("read ref: %v", err)
	}
	if ref != "deadbeef" {
		t.Fatalf("SetSourceRef not persisted: %q", ref)
	}

	// --- CopyChunksToRun: copy main.go's chunk (with vector) into a new run ---
	var toRun int64
	if err := d.QueryRow(
		`INSERT INTO atlas_runs(source_id,kind,baseline_id,status) VALUES($1,'delta',$2,'running') RETURNING id`,
		sourceID, runID).Scan(&toRun); err != nil {
		t.Fatalf("insert delta run: %v", err)
	}
	n, err := st.CopyChunksToRun(runID, toRun, []string{"main.go"})
	if err != nil {
		t.Fatalf("CopyChunksToRun: %v", err)
	}
	if n != 1 {
		t.Fatalf("CopyChunksToRun: want 1 copied, got %d", n)
	}
	copied, err := st.RunChunksWithVectors(toRun)
	if err != nil {
		t.Fatalf("RunChunksWithVectors(toRun): %v", err)
	}
	if len(copied) != 1 || copied[0].File != "main.go" || copied[0].Symbol != "main" {
		t.Fatalf("copied chunk wrong: %+v", copied)
	}
	if copied[0].ID == chunks[0].ID {
		t.Fatalf("copied chunk should be a new row, got same id %d", copied[0].ID)
	}
	assertVecEq(t, "copied", copied[0].Vector, chunks[0].Vector)

	// empty file list → no-op, zero copied
	if n0, err := st.CopyChunksToRun(runID, toRun, nil); err != nil || n0 != 0 {
		t.Fatalf("CopyChunksToRun(nil): n=%d err=%v", n0, err)
	}
}

func assertVecEq(t *testing.T, label string, got, want []float32) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: vector len %d != %d", label, len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: vector[%d] = %v, want %v", label, i, got[i], want[i])
		}
	}
}
