package api

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"

	"github.com/zcag/tela/backend/internal/atlas/core"
	"github.com/zcag/tela/backend/internal/atlas/engine"
)

// notifAtlasRun is the notification type for a finished atlas generation run.
// Text-coded like the other notification kinds (notifications.go) — additive: a
// new type is a constant + an emit site (+ a frontend render case, when added).
const notifAtlasRun = "atlas_run"

// notifyAtlasRunFinish emits an in-app notification to a project's managers when
// a generation run reaches a terminal state. Managers are exactly who can trigger
// a run (the owner user, or the admins of the owning org, per atlasOwnerManageErr),
// so they're the people who care that it finished.
//
// Best-effort and non-fatal: a notify failure must never affect the run, so the
// recipient lookup logs-and-returns and the actual write goes through tela's
// emitNotifications, which already swallows per-row errors. Reuses the existing
// notification path verbatim — same table, same helper, same input shape.
func (m *atlasManager) notifyAtlasRunFinish(ctx context.Context, rc *engine.RunContext, status core.RunStatus, runErr error) {
	if rc == nil || rc.Run == nil || rc.Project == nil {
		return
	}
	if status != core.RunDone && status != core.RunFailed {
		return // only done/failed carry copy; pending/running/canceled are silent
	}
	projectID := rc.Project.ID
	if projectID == 0 {
		return
	}
	proj, err := loadAtlasProject(ctx, m.s.DB, projectID)
	if err != nil {
		slog.Error("atlas: notify load project", "project", projectID, "run", rc.Run.ID, "err", err)
		return
	}

	projectName := proj.Name
	srcName := ""
	if rc.Source != nil {
		srcName = rc.Source.Name
	}
	prefix := atlasNotifPrefix(projectName, srcName)

	// Subject + in-app space_id point at the output space (when materialized) so the
	// notification navigates to the generated docs; the link targets the project.
	var spaceID *int64
	var subjectID int64
	if proj.OutputSpaceID != nil {
		spaceID = proj.OutputSpaceID
		subjectID = *proj.OutputSpaceID
	}

	data := map[string]any{
		"status":       string(status),
		"project_name": projectName,
		"source_name":  srcName,
		"link":         fmt.Sprintf("/atlas/projects/%d", projectID),
	}
	var title, summary string
	if status == core.RunDone {
		cov := rc.Coverage // computed by validate/repair, present on a done run
		pages := 0
		if rc.Run.Stats != nil {
			pages = rc.Run.Stats.Pages
		}
		must, surface := atlasPct(cov.MustRate()), atlasPct(cov.Rate())
		title = prefix + " regenerated"
		summary = fmt.Sprintf("%d pages, must-cover %d%%, surface %d%%", pages, must, surface)
		data["pages"], data["must_rate"], data["surface_rate"] = pages, must, surface
	} else {
		title = prefix + " generation failed"
		summary = atlasErrText(rc.Run.Err, runErr)
		data["err"] = summary
	}
	data["title"], data["summary"] = title, summary

	recipients, err := m.atlasProjectManagers(ctx, proj.OwnerKind, proj.OwnerID)
	if err != nil {
		slog.Error("atlas: notify resolve managers", "project", projectID, "run", rc.Run.ID, "err", err)
		return
	}
	if len(recipients) == 0 {
		return
	}
	out := make([]notificationInput, 0, len(recipients))
	for _, uid := range recipients {
		out = append(out, notificationInput{
			UserID:      uid,
			Type:        notifAtlasRun,
			SubjectKind: "space",
			SubjectID:   subjectID,
			SpaceID:     spaceID,
			Data:        data,
			// One-ever per (manager, run, terminal status) so a resume/double-fire
			// can't double-notify; a fresh run is a distinct id → a new row.
			DedupKey: "atlas_run:" + strconv.FormatInt(rc.Run.ID, 10) + ":" + string(status),
		})
	}
	m.s.emitNotifications(ctx, out...)
}

// atlasProjectManagers returns the de-duplicated user ids that can manage a
// project: the owner user (personal project) or every admin of the owning org.
// Mirrors atlasOwnerManageErr's notion of "manager"; instance admins are not
// blasted (they aren't the project's principals).
func (m *atlasManager) atlasProjectManagers(ctx context.Context, ownerKind string, ownerID int64) ([]int64, error) {
	switch ownerKind {
	case accountUser:
		return []int64{ownerID}, nil
	case accountOrg:
		rows, err := m.s.DB.QueryContext(ctx,
			`SELECT user_id FROM org_members WHERE org_id = $1 AND org_role = 'admin'`, ownerID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var ids []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			ids = append(ids, id)
		}
		return ids, rows.Err()
	}
	return nil, nil
}

// atlasNotifPrefix renders the "project · source" lead-in, collapsing to
// whichever part is present (so a single-source project reads cleanly).
func atlasNotifPrefix(projectName, srcName string) string {
	switch {
	case projectName != "" && srcName != "" && srcName != projectName:
		return projectName + " · " + srcName
	case srcName != "":
		return srcName
	case projectName != "":
		return projectName
	default:
		return "atlas"
	}
}

// atlasErrText picks the most specific failure message (the persisted run err, or
// the returned error) and clips it to one concise line.
func atlasErrText(runErr string, err error) string {
	msg := strings.TrimSpace(runErr)
	if msg == "" && err != nil {
		msg = strings.TrimSpace(err.Error())
	}
	if msg == "" {
		msg = "generation failed"
	}
	return clip(msg, 160)
}

// atlasPct rounds a 0..1 rate to a whole-number percentage.
func atlasPct(rate float64) int { return int(math.Round(rate * 100)) }
