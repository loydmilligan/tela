package api

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zcag/tela/backend/internal/atlas/core"
)

// MCP tools for the in-tela "atlas" doc-generation feature: list a managed
// space's sources, trigger a generation run, read a run's coverage, and (delta)
// re-sync on change. Each tool reuses the SAME identity + gating the REST atlas
// handlers use (atlas_http.go) and the SAME manager methods (StartRun/StartDelta)
// — no parallel gating, no duplicated executor logic. A run spends LLM budget and
// rewrites the generated subtree, so triggering is management-gated
// (atlasSpaceManageErr); reads are member-gated (membershipCore).

// ---- atlas_list_sources --------------------------------------------------

type atlasListSourcesIn struct {
	SpaceID int64 `json:"space_id" jsonschema:"id of the space whose atlas sources to list"`
}

type atlasListSourcesOut struct {
	Sources   []atlasSourceDTO `json:"sources"`
	Managed   bool             `json:"managed"`    // the space has ≥1 bound source (it's atlas-managed)
	CanManage bool             `json:"can_manage"` // the caller may trigger runs / edit sources
}

func (s *Server) mcpAtlasListSources(ctx context.Context, req *mcp.CallToolRequest, in atlasListSourcesIn) (*mcp.CallToolResult, atlasListSourcesOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), atlasListSourcesOut{}, nil
	}
	if _, ae := s.membershipCore(ctx, u, k, in.SpaceID); ae != nil {
		return mcpErr(ae), atlasListSourcesOut{}, nil
	}
	canManage := s.atlasSpaceManageErr(ctx, u, k, in.SpaceID) == nil
	// Mirror ListAtlasSources (atlas_http.go): each source with its latest-run
	// status + last must-cover rate. Kept in lockstep with that query.
	rows, err := s.DB.QueryContext(ctx, `
		SELECT s.id, s.space_id, s.parent_page_id, s.type, s.location, s.name, s.ref, s.branch, s.subpath,
		       s.include, s.exclude, s.cadence, s.auto_update, s.last_refresh_at, s.created_at,
		       lr.id, lr.status, lr.coverage_json
		  FROM atlas_sources s
		  LEFT JOIN LATERAL (
		        SELECT id, status, coverage_json FROM atlas_runs WHERE source_id = s.id ORDER BY id DESC LIMIT 1
		  ) lr ON true
		 WHERE s.space_id = $1
		 ORDER BY s.id`, in.SpaceID)
	if err != nil {
		return mcpErr(&apiErr{500, "internal", "list sources failed"}), atlasListSourcesOut{}, nil
	}
	defer rows.Close()
	out := atlasListSourcesOut{Sources: []atlasSourceDTO{}}
	for rows.Next() {
		var d atlasSourceDTO
		var parent, lastRunID sql.NullInt64
		var autoUpd int
		var lastStatus, covJSON sql.NullString
		if err := rows.Scan(&d.ID, &d.SpaceID, &parent, &d.Type, &d.Location, &d.Name, &d.Ref, &d.Branch,
			&d.Subpath, &d.Include, &d.Exclude, &d.Cadence, &autoUpd, &d.LastRefreshAt, &d.CreatedAt,
			&lastRunID, &lastStatus, &covJSON); err != nil {
			return mcpErr(&apiErr{500, "internal", "scan source failed"}), atlasListSourcesOut{}, nil
		}
		if parent.Valid {
			d.ParentPageID = &parent.Int64
		}
		d.AutoUpdate = autoUpd != 0
		d.NextDue = atlasNextDueStr(d.AutoUpdate, d.Cadence, d.LastRefreshAt)
		if lastRunID.Valid {
			d.LastRunID = &lastRunID.Int64
			d.LastRunStatus = lastStatus.String
			if covJSON.Valid && covJSON.String != "" {
				var cov core.Coverage
				if json.Unmarshal([]byte(covJSON.String), &cov) == nil {
					mr := cov.MustRate()
					d.LastMustRate = &mr
				}
			}
		}
		out.Sources = append(out.Sources, d)
	}
	out.Managed = len(out.Sources) > 0
	out.CanManage = canManage
	return nil, out, nil
}

// ---- atlas_run -----------------------------------------------------------

type atlasRunIn struct {
	SourceID int64 `json:"source_id" jsonschema:"id of the atlas source to generate docs for (from atlas_list_sources)"`
}

type atlasRunOut struct {
	RunID int64 `json:"run_id"` // poll with atlas_run_status / open the live SSE stream
}

func (s *Server) mcpAtlasRun(ctx context.Context, req *mcp.CallToolRequest, in atlasRunIn) (*mcp.CallToolResult, atlasRunOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), atlasRunOut{}, nil
	}
	spaceID, err := s.atlasSourceSpace(ctx, in.SourceID)
	if err == sql.ErrNoRows {
		return mcpErr(&apiErr{404, "not_found", "source not found"}), atlasRunOut{}, nil
	} else if err != nil {
		return mcpErr(&apiErr{500, "internal", "lookup source failed"}), atlasRunOut{}, nil
	}
	if ae := s.atlasSpaceManageErr(ctx, u, k, spaceID); ae != nil {
		return mcpErr(ae), atlasRunOut{}, nil
	}
	runID, ae := s.atlas.StartRun(ctx, in.SourceID)
	if ae != nil {
		return mcpErr(ae), atlasRunOut{}, nil
	}
	return nil, atlasRunOut{RunID: runID}, nil
}

// ---- atlas_sync (change-gated delta) -------------------------------------

type atlasSyncIn struct {
	SourceID int64 `json:"source_id" jsonschema:"id of the atlas source to refresh on change (from atlas_list_sources)"`
}

type atlasSyncOut struct {
	Changed bool  `json:"changed"`          // false = nothing changed upstream, no run started
	RunID   int64 `json:"run_id,omitempty"` // the started delta/full run when changed
}

func (s *Server) mcpAtlasSync(ctx context.Context, req *mcp.CallToolRequest, in atlasSyncIn) (*mcp.CallToolResult, atlasSyncOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), atlasSyncOut{}, nil
	}
	spaceID, err := s.atlasSourceSpace(ctx, in.SourceID)
	if err == sql.ErrNoRows {
		return mcpErr(&apiErr{404, "not_found", "source not found"}), atlasSyncOut{}, nil
	} else if err != nil {
		return mcpErr(&apiErr{500, "internal", "lookup source failed"}), atlasSyncOut{}, nil
	}
	if ae := s.atlasSpaceManageErr(ctx, u, k, spaceID); ae != nil {
		return mcpErr(ae), atlasSyncOut{}, nil
	}
	runID, changed, ae := s.atlas.StartDelta(ctx, in.SourceID)
	if ae != nil {
		return mcpErr(ae), atlasSyncOut{}, nil
	}
	return nil, atlasSyncOut{Changed: changed, RunID: runID}, nil
}

// ---- atlas_run_status ----------------------------------------------------

type atlasRunStatusIn struct {
	RunID int64 `json:"run_id" jsonschema:"id of the run to read (from atlas_run / atlas_list_sources)"`
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
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), atlasRunStatusOut{}, nil
	}
	spaceID, err := s.atlasRunSpace(ctx, in.RunID)
	if err == sql.ErrNoRows {
		return mcpErr(&apiErr{404, "not_found", "run not found"}), atlasRunStatusOut{}, nil
	} else if err != nil {
		return mcpErr(&apiErr{500, "internal", "lookup run failed"}), atlasRunStatusOut{}, nil
	}
	if _, ae := s.membershipCore(ctx, u, k, spaceID); ae != nil {
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
