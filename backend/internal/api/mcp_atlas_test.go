package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zcag/tela/backend/internal/auth"
)

// TestMCP_AtlasTools exercises the atlas MCP surface end-to-end over the
// transport: atlas_list_sources + atlas_run_status are member-readable and
// return the source/coverage envelopes, while atlas_run is management-gated (an
// editor is rejected with a forbidden code before any run is attempted).
func TestMCP_AtlasTools(t *testing.T) {
	ts, d := newWiredServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)      // space owner (manage)
	charlie := seedUser(t, d, "charlie", "charliepw1", false) // editor member (view only)
	space := seedSpace(t, d, "Repo Docs", "repo-docs", alice)
	seedMember(t, d, space, charlie, "editor")

	// A bound source (makes the space atlas-managed) with a completed run that has
	// coverage + stats — so list_sources reports the last run and run_status
	// surfaces the audit.
	var srcID int64
	if err := d.QueryRowContext(context.Background(),
		`INSERT INTO atlas_sources (space_id, type, location, name, ref, cadence, auto_update)
		 VALUES ($1,'git','https://github.com/example/repo.git','repo','abc123','daily',1) RETURNING id`,
		space).Scan(&srcID); err != nil {
		t.Fatalf("insert source: %v", err)
	}
	cov := `{"total":10,"covered":8,"must_total":5,"must_covered":4,"citations":12,` +
		`"gaps":[{"kind":"route","name":"GET /x","file":"a.go","line":3}]}`
	stats := `{"files":64,"surface":77,"chunks":433,"pages":13}`
	var runID int64
	if err := d.QueryRowContext(context.Background(),
		`INSERT INTO atlas_runs (source_id, kind, status, stage, coverage_json, stats_json)
		 VALUES ($1,'full','done','publish',$2,$3) RETURNING id`,
		srcID, cov, stats).Scan(&runID); err != nil {
		t.Fatalf("insert run: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// Owner with a write key; editor with a read key.
	ownerSess := mcpSession(t, ctx, ts, seedReadKey(t, d, alice, auth.ScopeWrite))
	editorSess := mcpSession(t, ctx, ts, seedReadKey(t, d, charlie, auth.ScopeRead))

	// atlas_list_sources (member): the bound source, managed + can_manage true for
	// the owner, with the latest-run status and last must-cover rate (4/5 = 0.8).
	var ls atlasListSourcesOut
	mcpCallJSON(t, ctx, ownerSess, "atlas_list_sources", map[string]any{"space_id": space}, &ls)
	if len(ls.Sources) != 1 || ls.Sources[0].ID != srcID {
		t.Fatalf("atlas_list_sources: %+v", ls.Sources)
	}
	if !ls.Managed || !ls.CanManage {
		t.Fatalf("atlas_list_sources flags: managed=%v can_manage=%v", ls.Managed, ls.CanManage)
	}
	s0 := ls.Sources[0]
	if s0.LastRunID == nil || *s0.LastRunID != runID || s0.LastRunStatus != "done" {
		t.Fatalf("atlas_list_sources last run: %+v", s0)
	}
	if s0.LastMustRate == nil || *s0.LastMustRate != 0.8 {
		t.Fatalf("atlas_list_sources last_must_rate: %+v", s0.LastMustRate)
	}

	// The editor (member, not manager) can still view — can_manage is false.
	var lsEd atlasListSourcesOut
	mcpCallJSON(t, ctx, editorSess, "atlas_list_sources", map[string]any{"space_id": space}, &lsEd)
	if !lsEd.Managed || lsEd.CanManage {
		t.Fatalf("editor list flags: managed=%v can_manage=%v", lsEd.Managed, lsEd.CanManage)
	}

	// atlas_run_status (member): status/stage + computed coverage + stats.
	var rs atlasRunStatusOut
	mcpCallJSON(t, ctx, editorSess, "atlas_run_status", map[string]any{"run_id": runID}, &rs)
	if rs.Run.Status != "done" || rs.Run.Stage != "publish" || rs.Run.Kind != "full" {
		t.Fatalf("atlas_run_status run: %+v", rs.Run)
	}
	if rs.Run.Coverage == nil || rs.Run.Coverage.MustRate != 0.8 || rs.Run.Coverage.SurfaceRate != 0.8 {
		t.Fatalf("atlas_run_status coverage rates: %+v", rs.Run.Coverage)
	}
	if rs.Run.Coverage.GapCount != 1 || len(rs.Run.Coverage.Gaps) != 1 || rs.Run.Coverage.Gaps[0].Name != "GET /x" {
		t.Fatalf("atlas_run_status gaps: %+v", rs.Run.Coverage)
	}
	if rs.Run.Stats == nil || rs.Run.Stats.Files != 64 || rs.Run.Stats.Pages != 13 {
		t.Fatalf("atlas_run_status stats: %+v", rs.Run.Stats)
	}

	// atlas_run manage-gate: the editor is rejected with the forbidden code,
	// BEFORE any run is attempted (no ai_unavailable leak).
	res, err := editorSess.CallTool(ctx, &mcp.CallToolParams{Name: "atlas_run", Arguments: map[string]any{"source_id": srcID}})
	if err != nil {
		t.Fatalf("editor atlas_run call: %v", err)
	}
	if !res.IsError {
		t.Fatalf("editor atlas_run: expected a tool error (management-gated)")
	}
	if txt := mcpErrText(res); !strings.Contains(txt, `"code":"forbidden"`) {
		t.Fatalf("editor atlas_run: want forbidden, got %s", txt)
	}

	// The owner passes the manage gate; in tests the AI backends are unconfigured,
	// so the run trips the enablement guard (503 ai_unavailable) — proving the gate
	// let the owner through to StartRun.
	resOwner, err := ownerSess.CallTool(ctx, &mcp.CallToolParams{Name: "atlas_run", Arguments: map[string]any{"source_id": srcID}})
	if err != nil {
		t.Fatalf("owner atlas_run call: %v", err)
	}
	if !resOwner.IsError {
		t.Fatalf("owner atlas_run: expected ai_unavailable in tests")
	}
	if txt := mcpErrText(resOwner); !strings.Contains(txt, `"code":"ai_unavailable"`) {
		t.Fatalf("owner atlas_run: want ai_unavailable (gate passed), got %s", txt)
	}

	// Unknown run → not_found.
	resNF, err := ownerSess.CallTool(ctx, &mcp.CallToolParams{Name: "atlas_run_status", Arguments: map[string]any{"run_id": 999999}})
	if err != nil {
		t.Fatalf("missing run call: %v", err)
	}
	if !resNF.IsError || !strings.Contains(mcpErrText(resNF), `"code":"not_found"`) {
		t.Fatalf("missing run: want not_found tool error, got %+v", resNF)
	}
}

// mcpErrText returns the text payload of a tool-error result (the {error,code,
// status} envelope mcpErr emits), for asserting on the error `code`.
func mcpErrText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
