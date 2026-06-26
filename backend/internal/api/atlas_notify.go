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

// notifyAtlasRunFinish emits an in-app notification to the managers of an
// atlas-managed space when a generation run reaches a terminal state. Managers
// are exactly who can trigger a run (space owner + admins of the owning org, per
// atlasSpaceManageErr), so they're the people who care that it finished.
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
	spaceID := rc.Project.ID
	if spaceID == 0 {
		return
	}

	spaceName := m.s.spaceName(ctx, &spaceID)
	srcName := ""
	if rc.Source != nil {
		srcName = rc.Source.Name
	}
	prefix := atlasNotifPrefix(spaceName, srcName)

	data := map[string]any{
		"status":      string(status),
		"space_name":  spaceName,
		"source_name": srcName,
		// The console isn't a generic subject route, so carry the precise deep link
		// in the payload (subject_kind=space still navigates sensibly without it).
		"link": fmt.Sprintf("/spaces/%d/atlas", spaceID),
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

	recipients, err := m.atlasSpaceManagers(ctx, spaceID)
	if err != nil {
		slog.Error("atlas: notify resolve managers", "space", spaceID, "run", rc.Run.ID, "err", err)
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
			SubjectID:   spaceID,
			SpaceID:     &spaceID,
			Data:        data,
			// One-ever per (manager, run, terminal status) so a resume/double-fire
			// can't double-notify; a fresh run is a distinct id → a new row.
			DedupKey: "atlas_run:" + strconv.FormatInt(rc.Run.ID, 10) + ":" + string(status),
		})
	}
	m.s.emitNotifications(ctx, out...)
}

// atlasSpaceManagers returns the de-duplicated user ids that can manage atlas on
// a space: its owners (space_members role='owner') plus, for an org-owned space,
// the admins of that org. Mirrors atlasSpaceManageErr's notion of "manager".
// Instance admins are intentionally not blasted (they aren't per-space owners).
func (m *atlasManager) atlasSpaceManagers(ctx context.Context, spaceID int64) ([]int64, error) {
	rows, err := m.s.DB.QueryContext(ctx, `
		SELECT user_id FROM space_members WHERE space_id = $1 AND role = 'owner'
		UNION
		SELECT om.user_id
		  FROM org_members om
		  JOIN spaces sp ON sp.org_id = om.org_id
		 WHERE sp.id = $1 AND om.org_role = 'admin'`, spaceID)
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

// atlasNotifPrefix renders the "space · source" lead-in, collapsing to whichever
// part is present (so a single-source managed space reads cleanly).
func atlasNotifPrefix(spaceName, srcName string) string {
	switch {
	case spaceName != "" && srcName != "" && srcName != spaceName:
		return spaceName + " · " + srcName
	case srcName != "":
		return srcName
	case spaceName != "":
		return spaceName
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
