package api

import (
	"database/sql"
	"net/http"
	"regexp"
	"strings"
)

// admin_client_errors.go — the instance-admin "Issues" view over browser error
// reports. client.error events are grouped by their server-computed fingerprint
// (see client_errors.go) so the screen shows one row per distinct error with a
// count, affected-user count, and first/last seen — not a 200-deep raw feed of
// the same crash. Drill-down lists recent occurrences for one fingerprint.

// firstLineKindMsg pulls the "[kind] message" header off a stored detail blob
// back into structured fields for display. The format is owned by
// CreateClientError, so this is a safe round-trip.
var clientErrTitleRe = regexp.MustCompile(`^\[([^\]]+)\] ?(.*)$`)

func splitClientErrDetail(detail string) (kind, message string) {
	firstLine := detail
	if i := strings.IndexByte(detail, '\n'); i >= 0 {
		firstLine = detail[:i]
	}
	if m := clientErrTitleRe.FindStringSubmatch(firstLine); m != nil {
		return m[1], m[2]
	}
	return "", firstLine
}

type clientErrorGroupDTO struct {
	Fingerprint string `json:"fingerprint"`
	Kind        string `json:"kind"`
	Message     string `json:"message"`
	Count       int64  `json:"count"`
	Users       int64  `json:"users"`
	FirstSeen   string `json:"first_seen"`
	LastSeen    string `json:"last_seen"`
	Sample      string `json:"sample"` // full detail of the latest occurrence (incl. stack)
}

// ListClientErrorGroups — GET /api/admin/client-errors. Instance-admin only.
// Groups by fingerprint, ordered by most-recently-seen, capped to a sane top-N.
func (s *Server) ListClientErrorGroups(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireInstanceAdmin(w, r); !ok {
		return
	}
	limit := clampLimit(r.URL.Query().Get("limit"), 100, 200)

	// Hide errors reported from instance admins' own browsers by default (dev/test
	// noise); ?include_admins brings them back. Both the group aggregate and the
	// drill-down occurrences apply the same filter so their counts stay consistent.
	adminCond := ""
	if f := adminActorFilter("actor_user_id", wantIncludeAdmins(r)); f != "" {
		adminCond = " AND " + f
	}

	rows, err := s.DB.QueryContext(r.Context(), `
		WITH agg AS (
			SELECT fingerprint,
			       COUNT(*)                    AS cnt,
			       COUNT(DISTINCT actor_user_id) AS users,
			       MIN(created_at)             AS first_seen,
			       MAX(created_at)             AS last_seen,
			       MAX(id)                     AS last_id
			  FROM events
			 WHERE type = $1 AND fingerprint IS NOT NULL`+adminCond+`
			 GROUP BY fingerprint
		)
		SELECT a.fingerprint, a.cnt, a.users, a.first_seen, a.last_seen, e.detail
		  FROM agg a JOIN events e ON e.id = a.last_id
		 ORDER BY a.last_seen DESC
		 LIMIT $2`, evtClientError, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list client errors failed")
		return
	}
	defer rows.Close()

	groups := []clientErrorGroupDTO{}
	for rows.Next() {
		var g clientErrorGroupDTO
		var sample string
		if err := rows.Scan(&g.Fingerprint, &g.Count, &g.Users, &g.FirstSeen, &g.LastSeen, &sample); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan client error row failed")
			return
		}
		g.Kind, g.Message = splitClientErrDetail(sample)
		g.Sample = sample
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate client errors failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
}

type clientErrorOccurrenceDTO struct {
	ID         int64  `json:"id"`
	ActorLabel string `json:"actor_label"`
	Detail     string `json:"detail"`
	IP         string `json:"ip"`
	CreatedAt  string `json:"created_at"`
}

// ListClientErrorOccurrences — GET /api/admin/client-errors/{fingerprint}.
// Instance-admin only. The recent individual reports behind one grouped issue
// (who hit it, when), newest first.
func (s *Server) ListClientErrorOccurrences(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireInstanceAdmin(w, r); !ok {
		return
	}
	fp := r.PathValue("fingerprint")
	if fp == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "fingerprint required")
		return
	}
	limit := clampLimit(r.URL.Query().Get("limit"), 50, 200)

	adminCond := ""
	if f := adminActorFilter("actor_user_id", wantIncludeAdmins(r)); f != "" {
		adminCond = " AND " + f
	}
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT id, actor_label, detail, ip, created_at
		  FROM events
		 WHERE type = $1 AND fingerprint = $2`+adminCond+`
		 ORDER BY id DESC
		 LIMIT $3`, evtClientError, fp, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list occurrences failed")
		return
	}
	defer rows.Close()

	out := []clientErrorOccurrenceDTO{}
	for rows.Next() {
		var o clientErrorOccurrenceDTO
		var ip sql.NullString
		if err := rows.Scan(&o.ID, &o.ActorLabel, &o.Detail, &ip, &o.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan occurrence failed")
			return
		}
		o.IP = ip.String
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate occurrences failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"occurrences": out})
}
