package api

import (
	"context"
	"database/sql"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zcag/tela/backend/internal/atlas/core"
)

// MCP tools for the in-tela "atlas" doc-generation feature: list the projects the
// caller can see, trigger a project's generation run, and read a run's coverage.
// Each tool reuses the SAME owner-scope gating the REST atlas handlers use and the
// SAME manager methods — no parallel gating, no duplicated executor logic. A run
// spends LLM budget and rewrites the generated subtree, so triggering is
// management-gated; reads are view-gated (owner + org members).

// ---- atlas_list_projects -------------------------------------------------

type atlasListProjectsIn struct{}

type atlasListProjectsOut struct {
	Projects []atlasProjectDTO `json:"projects"`
}

func (s *Server) mcpAtlasListProjects(ctx context.Context, req *mcp.CallToolRequest, in atlasListProjectsIn) (*mcp.CallToolResult, atlasListProjectsOut, error) {
	u, _ := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), atlasListProjectsOut{}, nil
	}
	projects, err := s.listAtlasProjectsFor(ctx, u)
	if err != nil {
		return mcpErr(&apiErr{500, "internal", "list projects failed"}), atlasListProjectsOut{}, nil
	}
	return nil, atlasListProjectsOut{Projects: projects}, nil
}

// ---- atlas_run -----------------------------------------------------------

type atlasRunIn struct {
	ProjectID int64 `json:"project_id" jsonschema:"id of the atlas project to generate docs for (from atlas_list_projects)"`
}

type atlasRunOut struct {
	RunIDs []int64 `json:"run_ids"` // one run per source; poll each with atlas_run_status
}

func (s *Server) mcpAtlasRun(ctx context.Context, req *mcp.CallToolRequest, in atlasRunIn) (*mcp.CallToolResult, atlasRunOut, error) {
	u, _ := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), atlasRunOut{}, nil
	}
	if ae := s.atlasProjectManageErr(ctx, u, in.ProjectID); ae != nil {
		return mcpErr(ae), atlasRunOut{}, nil
	}
	runIDs, ae := s.startProjectRuns(ctx, in.ProjectID)
	if ae != nil {
		return mcpErr(ae), atlasRunOut{}, nil
	}
	return nil, atlasRunOut{RunIDs: runIDs}, nil
}

// ---- atlas_run_status ----------------------------------------------------

type atlasRunStatusIn struct {
	RunID int64 `json:"run_id" jsonschema:"id of the run to read (from atlas_run)"`
}

// atlasCoverageView is the agent-facing coverage summary: the headline rates
// (computed from the raw counts via Coverage's own methods) plus the gap list,
// so an agent can judge completeness without re-deriving the math.
type atlasCoverageView struct {
	MustRate    float64    `json:"must_rate"`    // covered fraction of must-cover surface (0..1) — the headline number
	SurfaceRate float64    `json:"surface_rate"` // covered fraction of ALL surface items (0..1)
	MustCovered int        `json:"must_covered"`
	MustTotal   int        `json:"must_total"`
	Covered     int        `json:"covered"`
	Total       int        `json:"total"`
	GapCount    int        `json:"gap_count"`
	Gaps        []core.Gap `json:"gaps,omitempty"` // the uncovered surface items
	Citations   int        `json:"citations"`
}

type atlasRunView struct {
	ID         int64              `json:"id"`
	SourceID   int64              `json:"source_id"`
	Kind       core.RunKind       `json:"kind"`
	Status     core.RunStatus     `json:"status"`
	Stage      core.StageName     `json:"stage"`
	Err        string             `json:"err,omitempty"`
	StartedAt  string             `json:"started_at,omitempty"`
	FinishedAt string             `json:"finished_at,omitempty"`
	Coverage   *atlasCoverageView `json:"coverage,omitempty"` // set once validate/repair has run
	Stats      *core.RunStats     `json:"stats,omitempty"`    // set at publish (files/surface/chunks/pages)
}

type atlasRunStatusOut struct {
	Run atlasRunView `json:"run"`
}

func (s *Server) mcpAtlasRunStatus(ctx context.Context, req *mcp.CallToolRequest, in atlasRunStatusIn) (*mcp.CallToolResult, atlasRunStatusOut, error) {
	u, _ := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), atlasRunStatusOut{}, nil
	}
	projectID, err := s.atlasRunProject(ctx, in.RunID)
	if err == sql.ErrNoRows {
		return mcpErr(&apiErr{404, "not_found", "run not found"}), atlasRunStatusOut{}, nil
	} else if err != nil {
		return mcpErr(&apiErr{500, "internal", "lookup run failed"}), atlasRunStatusOut{}, nil
	}
	if ae := s.atlasProjectViewErr(ctx, u, projectID); ae != nil {
		return mcpErr(ae), atlasRunStatusOut{}, nil
	}
	run, err := s.atlas.store.GetRun(in.RunID)
	if err != nil || run == nil {
		return mcpErr(&apiErr{404, "not_found", "run not found"}), atlasRunStatusOut{}, nil
	}
	v := atlasRunView{
		ID: run.ID, SourceID: run.SourceID, Kind: run.Kind,
		Status: run.Status, Stage: run.Stage, Err: run.Err, Stats: run.Stats,
	}
	if !run.StartedAt.IsZero() {
		v.StartedAt = run.StartedAt.UTC().Format("2006-01-02 15:04:05")
	}
	if !run.FinishedAt.IsZero() {
		v.FinishedAt = run.FinishedAt.UTC().Format("2006-01-02 15:04:05")
	}
	if c := run.Coverage; c != nil {
		v.Coverage = &atlasCoverageView{
			MustRate: c.MustRate(), SurfaceRate: c.Rate(),
			MustCovered: c.MustCovered, MustTotal: c.MustTotal,
			Covered: c.Covered, Total: c.Total,
			GapCount: len(c.Gaps), Gaps: c.Gaps, Citations: c.Citations,
		}
	}
	return nil, atlasRunStatusOut{Run: v}, nil
}
