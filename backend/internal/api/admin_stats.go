package api

import (
	"context"
	"database/sql"
	"net/http"
	"time"
)

// admin_stats.go — GET /api/admin/stats. The instance-analytics dashboard
// (Settings → Insights). Instance-admin only. Everything here is aggregated from
// existing tables — mostly `events` (the activity log) plus page_revisions,
// page_links, page_agreement, ask_log — so there's no new instrumentation.
//
// Read-only and a touch heavy (≈12 aggregations over a 30-day window); it's an
// admin screen, not a hot path. At larger volumes the events scans want a
// nightly rollup table — noted as the scale follow-up.

const (
	statsWindowDays = 30
	staleAfterDays  = 90 // a page not touched in this long is "stale"
)

type statsTopPage struct {
	PageID    int64  `json:"page_id"`
	SpaceID   int64  `json:"space_id"`
	Title     string `json:"title"`
	SpaceName string `json:"space_name"`
	Count     int64  `json:"count"`
}

type statsTopPerson struct {
	Label string `json:"label"`
	Count int64  `json:"count"`
}

type statsTopSpace struct {
	SpaceID int64  `json:"space_id"`
	Name    string `json:"name"`
	Count   int64  `json:"count"`
}

type statsKindCount struct {
	Kind  string `json:"kind"`
	Count int64  `json:"count"`
}

type adminStats struct {
	// Dense daily buckets (oldest→newest), len == statsWindowDays. `days` are the
	// 'YYYY-MM-DD' labels the others align to.
	Days   []string `json:"days"`
	Views  []int64  `json:"views"`
	Edits  []int64  `json:"edits"`
	Logins []int64  `json:"logins"`
	Asks   []int64  `json:"asks"`
	Errors []int64  `json:"errors"`
	// Cumulative growth over the same days.
	UsersCum []int64 `json:"users_cum"`
	PagesCum []int64 `json:"pages_cum"`

	// Active distinct users over rolling windows.
	DAU int64 `json:"dau"`
	WAU int64 `json:"wau"`
	MAU int64 `json:"mau"`

	// Current totals.
	Users  int64 `json:"users"`
	Spaces int64 `json:"spaces"`
	Pages  int64 `json:"pages"`

	// Leaderboards (30d).
	TopPages        []statsTopPage   `json:"top_pages"`
	TopContributors []statsTopPerson `json:"top_contributors"`
	TopSpaces       []statsTopSpace  `json:"top_spaces"`

	// AI (30d).
	Asks30         int64 `json:"asks_30"`
	AsksAnswered30 int64 `json:"asks_answered_30"`

	// Errors by kind (7d).
	ErrorsByKind []statsKindCount `json:"errors_by_kind"`

	// Knowledge health (current).
	StalePages     int64 `json:"stale_pages"`
	OrphanPages    int64 `json:"orphan_pages"`
	Contradictions int64 `json:"contradictions"`
}

func (s *Server) AdminStats(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireInstanceAdmin(w, r); !ok {
		return
	}
	ctx := r.Context()
	now := time.Now().UTC()

	// Dense day axis (oldest→newest) + a day→index map for scattering grouped rows.
	days := make([]string, statsWindowDays)
	idx := map[string]int{}
	for i := 0; i < statsWindowDays; i++ {
		d := now.AddDate(0, 0, -(statsWindowDays - 1 - i)).Format("2006-01-02")
		days[i] = d
		idx[d] = i
	}
	cut30 := days[0] + " 00:00:00"
	cut7 := now.AddDate(0, 0, -7).Format("2006-01-02 15:04:05")
	cut1 := now.AddDate(0, 0, -1).Format("2006-01-02 15:04:05")

	out := adminStats{
		Days:     days,
		Views:    make([]int64, statsWindowDays),
		Edits:    make([]int64, statsWindowDays),
		Logins:   make([]int64, statsWindowDays),
		Asks:     make([]int64, statsWindowDays),
		Errors:   make([]int64, statsWindowDays),
		UsersCum: make([]int64, statsWindowDays),
		PagesCum: make([]int64, statsWindowDays),
		// Non-nil slices so the JSON is [] not null when empty.
		TopPages:        []statsTopPage{},
		TopContributors: []statsTopPerson{},
		TopSpaces:       []statsTopSpace{},
		ErrorsByKind:    []statsKindCount{},
	}

	// --- Daily activity by type ---
	if rows, err := s.DB.QueryContext(ctx, `
		SELECT substr(created_at,1,10) AS day, type, COUNT(*)
		  FROM events
		 WHERE created_at >= $1
		   AND type IN ('page.view','page.edit','auth.login','ask','client.error')
		 GROUP BY day, type`, cut30); err == nil {
		defer rows.Close()
		for rows.Next() {
			var day, typ string
			var n int64
			if err := rows.Scan(&day, &typ, &n); err != nil {
				break
			}
			i, ok := idx[day]
			if !ok {
				continue
			}
			switch typ {
			case evtPageView:
				out.Views[i] = n
			case evtPageEdit:
				out.Edits[i] = n
			case evtAuthLogin:
				out.Logins[i] = n
			case evtAsk:
				out.Asks[i] = n
			case evtClientError:
				out.Errors[i] = n
			}
		}
		rows.Close()
	}

	// --- Active users (rolling DAU/WAU/MAU) ---
	_ = s.DB.QueryRowContext(ctx, `
		SELECT
		  COUNT(DISTINCT actor_user_id) FILTER (WHERE created_at >= $1),
		  COUNT(DISTINCT actor_user_id) FILTER (WHERE created_at >= $2),
		  COUNT(DISTINCT actor_user_id) FILTER (WHERE created_at >= $3)
		  FROM events
		 WHERE actor_user_id IS NOT NULL AND created_at >= $3`,
		cut1, cut7, cut30).Scan(&out.DAU, &out.WAU, &out.MAU)

	// --- Current totals ---
	_ = s.DB.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM users),
		       (SELECT COUNT(*) FROM spaces),
		       (SELECT COUNT(*) FROM pages WHERE deleted_at IS NULL)`).
		Scan(&out.Users, &out.Spaces, &out.Pages)

	// --- Cumulative growth (baseline before window + running daily new) ---
	fillCumulative(ctx, s.DB, out.UsersCum, days, idx,
		`SELECT COUNT(*) FROM users WHERE created_at < $1`,
		`SELECT substr(created_at,1,10), COUNT(*) FROM users WHERE created_at >= $1 GROUP BY 1`, cut30)
	fillCumulative(ctx, s.DB, out.PagesCum, days, idx,
		`SELECT COUNT(*) FROM pages WHERE deleted_at IS NULL AND created_at < $1`,
		`SELECT substr(created_at,1,10), COUNT(*) FROM pages WHERE deleted_at IS NULL AND created_at >= $1 GROUP BY 1`, cut30)

	// --- Top pages by views (30d) ---
	if rows, err := s.DB.QueryContext(ctx, `
		SELECT e.target_id, p.space_id, p.title, sp.name, COUNT(*) c
		  FROM events e
		  JOIN pages p  ON p.id = e.target_id AND p.deleted_at IS NULL
		  JOIN spaces sp ON sp.id = p.space_id
		 WHERE e.type = 'page.view' AND e.target_kind = 'page' AND e.created_at >= $1
		 GROUP BY e.target_id, p.space_id, p.title, sp.name
		 ORDER BY c DESC LIMIT 10`, cut30); err == nil {
		defer rows.Close()
		for rows.Next() {
			var tp statsTopPage
			if err := rows.Scan(&tp.PageID, &tp.SpaceID, &tp.Title, &tp.SpaceName, &tp.Count); err != nil {
				break
			}
			out.TopPages = append(out.TopPages, tp)
		}
		rows.Close()
	}

	// --- Top contributors by edits (30d) ---
	if rows, err := s.DB.QueryContext(ctx, `
		SELECT actor_label, COUNT(*) c
		  FROM events
		 WHERE type = 'page.edit' AND created_at >= $1 AND actor_label <> ''
		 GROUP BY actor_label ORDER BY c DESC LIMIT 10`, cut30); err == nil {
		defer rows.Close()
		for rows.Next() {
			var p statsTopPerson
			if err := rows.Scan(&p.Label, &p.Count); err != nil {
				break
			}
			out.TopContributors = append(out.TopContributors, p)
		}
		rows.Close()
	}

	// --- Most-active spaces (views+edits, 30d) ---
	if rows, err := s.DB.QueryContext(ctx, `
		SELECT sp.id, sp.name, COUNT(*) c
		  FROM events e
		  JOIN pages p  ON p.id = e.target_id AND p.deleted_at IS NULL
		  JOIN spaces sp ON sp.id = p.space_id
		 WHERE e.target_kind = 'page' AND e.type IN ('page.view','page.edit') AND e.created_at >= $1
		 GROUP BY sp.id, sp.name ORDER BY c DESC LIMIT 10`, cut30); err == nil {
		defer rows.Close()
		for rows.Next() {
			var ts statsTopSpace
			if err := rows.Scan(&ts.SpaceID, &ts.Name, &ts.Count); err != nil {
				break
			}
			out.TopSpaces = append(out.TopSpaces, ts)
		}
		rows.Close()
	}

	// --- Asks + answer rate (30d) ---
	_ = s.DB.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(answered),0) FROM ask_log WHERE created_at >= $1`, cut30).
		Scan(&out.Asks30, &out.AsksAnswered30)

	// --- Client errors by kind (7d), parsed from the "[kind] …" detail header ---
	if rows, err := s.DB.QueryContext(ctx, `
		SELECT COALESCE((regexp_match(detail, '^\[([^\]]+)\]'))[1], 'error') AS kind, COUNT(*) c
		  FROM events
		 WHERE type = 'client.error' AND created_at >= $1
		 GROUP BY kind ORDER BY c DESC`, cut7); err == nil {
		defer rows.Close()
		for rows.Next() {
			var kc statsKindCount
			if err := rows.Scan(&kc.Kind, &kc.Count); err != nil {
				break
			}
			out.ErrorsByKind = append(out.ErrorsByKind, kc)
		}
		rows.Close()
	}

	// --- Knowledge health ---
	staleCut := now.AddDate(0, 0, -staleAfterDays).Format("2006-01-02 15:04:05")
	_ = s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pages WHERE deleted_at IS NULL AND updated_at < $1`, staleCut).Scan(&out.StalePages)
	_ = s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pages p WHERE p.deleted_at IS NULL
		   AND NOT EXISTS (SELECT 1 FROM page_links l WHERE l.target_id = p.id)`).Scan(&out.OrphanPages)
	_ = s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM page_agreement WHERE dispute > 0`).Scan(&out.Contradictions)

	writeJSON(w, http.StatusOK, out)
}

// fillCumulative seeds dst[i] with a running cumulative total across `days`: the
// baseline count (rows that existed before the window) plus each day's new rows,
// carried forward. Best-effort — a query error just leaves zeros.
func fillCumulative(ctx context.Context, db *sql.DB, dst []int64, days []string, idx map[string]int, baselineQ, perDayQ, cut string) {
	var baseline int64
	_ = db.QueryRowContext(ctx, baselineQ, cut).Scan(&baseline)

	perDay := make([]int64, len(days))
	if rows, err := db.QueryContext(ctx, perDayQ, cut); err == nil {
		defer rows.Close()
		for rows.Next() {
			var day string
			var n int64
			if err := rows.Scan(&day, &n); err != nil {
				break
			}
			if i, ok := idx[day]; ok {
				perDay[i] = n
			}
		}
		rows.Close()
	}
	running := baseline
	for i := range days {
		running += perDay[i]
		dst[i] = running
	}
}
