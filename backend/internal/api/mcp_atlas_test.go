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
// transport: atlas_list_projects + atlas_run_status are view-readable (owner and
// org members) and return the project/coverage envelopes, while atlas_run is
// management-gated (an org member who isn't an admin is rejected with a forbidden
// code before any run is attempted).
func TestMCP_AtlasTools(t *testing.T) {
	ts, d := newWiredServer(t)
	admin := seedUser(t, d, "admin", "adminpw12", false)   // org admin (manage)
	member := seedUser(t, d, "member", "memberpw1", false) // org member (view only)
	org := seedOrg(t, d, "Acme", "acme")
	seedOrgMember(t, d, org, admin, orgRoleAdmin)
	seedOrgMember(t, d, org, member, orgRoleMember)

	space := seedSpace(t, d, "Acme Docs", "acme-docs", admin)
	pid := seedAtlasProject(t, d, "Acme Docs", accountOrg, org, space, 0)

	// A source with a completed run that has coverage + stats — so list_projects
	// reports the last run and run_status surfaces the audit.
	var srcID int64
	if err := d.QueryRowContext(context.Background(),
		`INSERT INTO atlas_sources (project_id, type, location, name, ref)
		 VALUES ($1,'git','https://github.com/example/repo.git','repo','abc123') RETURNING id`,
		pid).Scan(&srcID); err != nil {
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
	// Admin with a write key; member with a read key.
	adminSess := mcpSession(t, ctx, ts, seedReadKey(t, d, admin, auth.ScopeWrite))
	memberSess := mcpSession(t, ctx, ts, seedReadKey(t, d, member, auth.ScopeRead))

	// atlas_list_projects (admin): the project, can_manage true, with the latest-run
	// status and last must-cover rate (4/5 = 0.8).
	var lp atlasListProjectsOut
	mcpCallJSON(t, ctx, adminSess, "atlas_list_projects", map[string]any{}, &lp)
	if len(lp.Projects) != 1 || lp.Projects[0].ID != pid {
		t.Fatalf("atlas_list_projects: %+v", lp.Projects)
	}
	p0 := lp.Projects[0]
	if !p0.CanManage || p0.Owner.Kind != "org" || p0.Owner.ID != org {
		t.Fatalf("atlas_list_projects flags: %+v", p0)
	}
	if p0.LastRun == nil || p0.LastRun.ID != runID || p0.LastRun.Status != "done" {
		t.Fatalf("atlas_list_projects last run: %+v", p0.LastRun)
	}
	if p0.LastRun.MustRate == nil || *p0.LastRun.MustRate != 0.8 {
		t.Fatalf("atlas_list_projects last_must_rate: %+v", p0.LastRun.MustRate)
	}

	// The member (org member, not admin) can still view — can_manage is false.
	var lpM atlasListProjectsOut
	mcpCallJSON(t, ctx, memberSess, "atlas_list_projects", map[string]any{}, &lpM)
	if len(lpM.Projects) != 1 || lpM.Projects[0].CanManage {
		t.Fatalf("member list flags: %+v", lpM.Projects)
	}

	// atlas_run_status (member, view): status/stage + computed coverage + stats.
	var rs atlasRunStatusOut
	mcpCallJSON(t, ctx, memberSess, "atlas_run_status", map[string]any{"run_id": runID}, &rs)
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

	// atlas_run manage-gate: the member is rejected with the forbidden code, BEFORE
	// any run is attempted (no ai_unavailable leak).
	res, err := memberSess.CallTool(ctx, &mcp.CallToolParams{Name: "atlas_run", Arguments: map[string]any{"project_id": pid}})
	if err != nil {
		t.Fatalf("member atlas_run call: %v", err)
	}
	if !res.IsError {
		t.Fatalf("member atlas_run: expected a tool error (management-gated)")
	}
	if txt := mcpErrText(res); !strings.Contains(txt, `"code":"forbidden"`) {
		t.Fatalf("member atlas_run: want forbidden, got %s", txt)
	}

	// The admin passes the manage gate; in tests the AI backends are unconfigured,
	// so the run trips the enablement guard (503 ai_unavailable) — proving the gate
	// let the admin through to StartRun.
	resAdmin, err := adminSess.CallTool(ctx, &mcp.CallToolParams{Name: "atlas_run", Arguments: map[string]any{"project_id": pid}})
	if err != nil {
		t.Fatalf("admin atlas_run call: %v", err)
	}
	if !resAdmin.IsError {
		t.Fatalf("admin atlas_run: expected ai_unavailable in tests")
	}
	if txt := mcpErrText(resAdmin); !strings.Contains(txt, `"code":"ai_unavailable"`) {
		t.Fatalf("admin atlas_run: want ai_unavailable (gate passed), got %s", txt)
	}

	// Unknown run → not_found.
	resNF, err := adminSess.CallTool(ctx, &mcp.CallToolParams{Name: "atlas_run_status", Arguments: map[string]any{"run_id": 999999}})
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
