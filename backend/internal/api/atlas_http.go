package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/zcag/tela/backend/internal/atlas/core"
	"github.com/zcag/tela/backend/internal/auth"
)

// ── request / response shapes ───────────────────────────────────────────────

type atlasSourceCreateReq struct {
	Type         string `json:"type"` // "git" (jira lands in Phase 5)
	Location     string `json:"location"`
	Name         string `json:"name"`
	Branch       string `json:"branch"`
	Subpath      string `json:"subpath"`
	Include      string `json:"include"`
	Exclude      string `json:"exclude"`
	ParentPageID *int64 `json:"parent_page_id"`
	Cadence      string `json:"cadence"`     // "hourly"|"daily"|"weekly"|"monthly"|"" = off
	AutoUpdate   *bool  `json:"auto_update"` // default true
}

type atlasSourceDTO struct {
	ID            int64  `json:"id"`
	SpaceID       int64  `json:"space_id"`
	ParentPageID  *int64 `json:"parent_page_id,omitempty"`
	Type          string `json:"type"`
	Location      string `json:"location"`
	Name          string `json:"name"`
	Ref           string `json:"ref"`
	Branch        string `json:"branch,omitempty"`
	Subpath       string `json:"subpath,omitempty"`
	Include       string `json:"include,omitempty"`
	Exclude       string `json:"exclude,omitempty"`
	Cadence       string `json:"cadence"`
	AutoUpdate    bool   `json:"auto_update"`
	LastRefreshAt string `json:"last_refresh_at,omitempty"`
	// NextDue is the next scheduled refresh (last_refresh_at + cadence), for the
	// drift UI ("auto-updates daily · next due in 3h"). Empty when auto-update is
	// off, the cadence is off, or the source has never refreshed (due now).
	NextDue   string `json:"next_due,omitempty"`
	CreatedAt string `json:"created_at"`
	// Latest-run summary (nil when never run), for the sources list.
	LastRunID     *int64   `json:"last_run_id,omitempty"`
	LastRunStatus string   `json:"last_run_status,omitempty"`
	LastMustRate  *float64 `json:"last_must_rate,omitempty"`
}

var atlasCadences = map[string]bool{"": true, "hourly": true, "daily": true, "weekly": true, "monthly": true}

// ── space-scoped: source CRUD ───────────────────────────────────────────────

// CreateAtlasSource binds a source (git repo) to a space, making it atlas-managed.
// POST /api/spaces/{id}/atlas/sources — management-gated.
func (s *Server) CreateAtlasSource(w http.ResponseWriter, r *http.Request) {
	spaceID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	k, _ := auth.APIKeyFromContext(r.Context())
	if ae := s.atlasSpaceManageErr(r.Context(), u, k, spaceID); ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	var req atlasSourceCreateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	if req.Type == "" {
		req.Type = string(core.SourceGit)
	}
	if req.Type != string(core.SourceGit) {
		writeError(w, http.StatusBadRequest, "unsupported_type", "only 'git' sources are supported yet")
		return
	}
	if req.Location == "" {
		writeError(w, http.StatusBadRequest, "invalid_source", "location is required")
		return
	}
	if !atlasCadences[req.Cadence] {
		writeError(w, http.StatusBadRequest, "invalid_cadence", "cadence must be hourly|daily|weekly|monthly or empty")
		return
	}
	autoUpdate := req.AutoUpdate == nil || *req.AutoUpdate
	cadence := req.Cadence
	if cadence == "" && autoUpdate {
		cadence = "daily"
	}
	// parent_page_id, when given, must belong to this space.
	if req.ParentPageID != nil {
		var inSpace bool
		err := s.DB.QueryRowContext(r.Context(),
			`SELECT EXISTS(SELECT 1 FROM pages WHERE id=$1 AND space_id=$2 AND deleted_at IS NULL)`,
			*req.ParentPageID, spaceID).Scan(&inSpace)
		if err != nil || !inSpace {
			writeError(w, http.StatusBadRequest, "invalid_parent", "parent_page_id must be a page in this space")
			return
		}
	}
	var id int64
	var createdAt string
	err := s.DB.QueryRowContext(r.Context(), `
		INSERT INTO atlas_sources (space_id, parent_page_id, type, location, name, branch, subpath, include, exclude, cadence, auto_update)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id, created_at`,
		spaceID, nullableInt64(req.ParentPageID), req.Type, req.Location, req.Name, req.Branch, req.Subpath,
		req.Include, req.Exclude, cadence, boolToInt(autoUpdate)).Scan(&id, &createdAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "create source failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"source": atlasSourceDTO{
		ID: id, SpaceID: spaceID, ParentPageID: req.ParentPageID, Type: req.Type, Location: req.Location,
		Name: req.Name, Branch: req.Branch, Subpath: req.Subpath, Include: req.Include, Exclude: req.Exclude,
		Cadence: cadence, AutoUpdate: autoUpdate, CreatedAt: createdAt,
	}})
}

// ListAtlasSources lists a space's sources (+ latest-run summary) and whether the
// space is atlas-managed. GET /api/spaces/{id}/atlas/sources — member-gated.
func (s *Server) ListAtlasSources(w http.ResponseWriter, r *http.Request) {
	spaceID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	k, _ := auth.APIKeyFromContext(r.Context())
	if _, ae := s.membershipCore(r.Context(), u, k, spaceID); ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	canManage := s.atlasSpaceManageErr(r.Context(), u, k, spaceID) == nil
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT s.id, s.space_id, s.parent_page_id, s.type, s.location, s.name, s.ref, s.branch, s.subpath,
		       s.include, s.exclude, s.cadence, s.auto_update, s.last_refresh_at, s.created_at,
		       lr.id, lr.status, lr.coverage_json
		  FROM atlas_sources s
		  LEFT JOIN LATERAL (
		        SELECT id, status, coverage_json FROM atlas_runs WHERE source_id = s.id ORDER BY id DESC LIMIT 1
		  ) lr ON true
		 WHERE s.space_id = $1
		 ORDER BY s.id`, spaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list sources failed")
		return
	}
	defer rows.Close()
	out := []atlasSourceDTO{}
	for rows.Next() {
		var d atlasSourceDTO
		var parent, lastRunID sql.NullInt64
		var autoUpd int
		var lastStatus, covJSON sql.NullString
		if err := rows.Scan(&d.ID, &d.SpaceID, &parent, &d.Type, &d.Location, &d.Name, &d.Ref, &d.Branch,
			&d.Subpath, &d.Include, &d.Exclude, &d.Cadence, &autoUpd, &d.LastRefreshAt, &d.CreatedAt,
			&lastRunID, &lastStatus, &covJSON); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan source failed")
			return
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
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": out, "managed": len(out) > 0, "can_manage": canManage})
}

// DeleteAtlasSource unbinds a source (CASCADEs its runs + ingestion artifacts;
// generated pages are left in place). DELETE /api/atlas/sources/{id} — management.
func (s *Server) DeleteAtlasSource(w http.ResponseWriter, r *http.Request) {
	sourceID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	if !s.atlasManageSource(w, r, sourceID) {
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), `DELETE FROM atlas_sources WHERE id=$1`, sourceID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "delete source failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RunAtlasSource triggers a full run for a source. POST /api/atlas/sources/{id}/run — management.
func (s *Server) RunAtlasSource(w http.ResponseWriter, r *http.Request) {
	sourceID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	if !s.atlasManageSource(w, r, sourceID) {
		return
	}
	runID, ae := s.atlas.StartRun(r.Context(), sourceID)
	if ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"run_id": runID})
}

// ListAtlasSourceRuns lists a source's runs. GET /api/atlas/sources/{id}/runs — member.
func (s *Server) ListAtlasSourceRuns(w http.ResponseWriter, r *http.Request) {
	sourceID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	spaceID, err := s.atlasSourceSpace(r.Context(), sourceID)
	if err != nil {
		s.atlasResolveErr(w, err)
		return
	}
	if _, ok := s.requireMembership(w, r, spaceID); !ok {
		return
	}
	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT id, kind, status, stage, err, coverage_json, stats_json, started_at, finished_at
		   FROM atlas_runs WHERE source_id=$1 ORDER BY id DESC LIMIT 50`, sourceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list runs failed")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var kind, status, stage, errStr, covJSON, statsJSON, started, finished string
		if err := rows.Scan(&id, &kind, &status, &stage, &errStr, &covJSON, &statsJSON, &started, &finished); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan run failed")
			return
		}
		m := map[string]any{"id": id, "kind": kind, "status": status, "stage": stage, "started_at": started}
		if errStr != "" {
			m["err"] = errStr
		}
		if finished != "" {
			m["finished_at"] = finished
		}
		if covJSON != "" {
			var cov core.Coverage
			if json.Unmarshal([]byte(covJSON), &cov) == nil {
				m["coverage"] = cov
			}
		}
		out = append(out, m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": out})
}

// GetAtlasRun returns a run's status + coverage + stats. GET /api/atlas/runs/{id} — member.
func (s *Server) GetAtlasRun(w http.ResponseWriter, r *http.Request) {
	runID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	spaceID, err := s.atlasRunSpace(r.Context(), runID)
	if err != nil {
		s.atlasResolveErr(w, err)
		return
	}
	if _, ok := s.requireMembership(w, r, spaceID); !ok {
		return
	}
	run, err := s.atlas.store.GetRun(runID)
	if err != nil || run == nil {
		writeError(w, http.StatusNotFound, "not_found", "run not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": run})
}

// StreamAtlasRun streams a run's live progress over SSE: persisted events replay
// first, then live tail. GET /api/atlas/runs/{id}/stream — member.
func (s *Server) StreamAtlasRun(w http.ResponseWriter, r *http.Request) {
	runID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	spaceID, err := s.atlasRunSpace(r.Context(), runID)
	if err != nil {
		s.atlasResolveErr(w, err)
		return
	}
	if _, ok := s.requireMembership(w, r, spaceID); !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal", "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// Subscribe BEFORE replay so an event fired during replay isn't lost.
	ch, unsub := s.atlas.hub.subscribe(runID)
	defer unsub()

	send := func(e core.Event) {
		b, _ := json.Marshal(e)
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}

	// Replay persisted events.
	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT stage, level, msg, cur, total, at FROM atlas_run_events WHERE run_id=$1 ORDER BY id`, runID)
	if err == nil {
		for rows.Next() {
			var e core.Event
			var stage, level, at string
			if rows.Scan(&stage, &level, &e.Msg, &e.Cur, &e.Total, &at) == nil {
				e.RunID, e.Stage, e.Level = runID, core.StageName(stage), core.EventLevel(level)
				if t, perr := time.Parse("2006-01-02 15:04:05", at); perr == nil {
					e.At = t
				}
				send(e)
			}
		}
		rows.Close()
	}

	// If the run already finished, send a terminal marker and stop — don't hang.
	var status string
	_ = s.DB.QueryRowContext(r.Context(), `SELECT status FROM atlas_runs WHERE id=$1`, runID).Scan(&status)
	if status == string(core.RunDone) || status == string(core.RunFailed) || status == string(core.RunCanceled) {
		send(core.Event{RunID: runID, Stage: atlasEndStage, Level: core.LevelInfo, Msg: status})
		return
	}

	// Live tail until the run ends, the client disconnects, or we go idle.
	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case e, open := <-ch:
			if !open {
				return
			}
			send(e)
			if e.Stage == atlasEndStage {
				return
			}
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// atlasEndStage is the synthetic terminal marker published to the hub when a run
// reaches a terminal state, so live SSE subscribers know to close.
const atlasEndStage core.StageName = "__end__"

// ── helpers: resolve the governing space + gate ─────────────────────────────

// atlasManageSource resolves a source's space and enforces management rights,
// writing the error envelope on failure. Returns true when the caller may manage.
func (s *Server) atlasManageSource(w http.ResponseWriter, r *http.Request, sourceID int64) bool {
	spaceID, err := s.atlasSourceSpace(r.Context(), sourceID)
	if err != nil {
		s.atlasResolveErr(w, err)
		return false
	}
	u, ok := requireUser(w, r)
	if !ok {
		return false
	}
	k, _ := auth.APIKeyFromContext(r.Context())
	if ae := s.atlasSpaceManageErr(r.Context(), u, k, spaceID); ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return false
	}
	return true
}

func (s *Server) atlasSourceSpace(ctx context.Context, sourceID int64) (int64, error) {
	var spaceID int64
	err := s.DB.QueryRowContext(ctx, `SELECT space_id FROM atlas_sources WHERE id=$1`, sourceID).Scan(&spaceID)
	return spaceID, err
}

func (s *Server) atlasRunSpace(ctx context.Context, runID int64) (int64, error) {
	var spaceID int64
	err := s.DB.QueryRowContext(ctx,
		`SELECT s.space_id FROM atlas_runs r JOIN atlas_sources s ON s.id=r.source_id WHERE r.id=$1`, runID).Scan(&spaceID)
	return spaceID, err
}

func (s *Server) atlasResolveErr(w http.ResponseWriter, err error) {
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	writeError(w, http.StatusInternalServerError, "internal", "lookup failed")
}
