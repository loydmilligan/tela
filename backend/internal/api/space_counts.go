package api

import (
	"net/http"

	"github.com/zcag/tela/backend/internal/auth"
)

// Per-space sidebar counts (GET /api/spaces/counts): one batched read of total
// pages + disputed pages for every space the caller can see, so the sidebar
// renders badges (#26) without an N-per-space fan-out of /overview calls.
//
// Access: the same space_access gate every other read uses (docs/access-model.md
// invariant 4) — a space the caller can't read never appears, so a count can't
// leak the existence or size of a private space. An API-key-scoped caller is
// confined to its one space.

type spaceCounts struct {
	SpaceID  int64 `json:"space_id"`
	Total    int   `json:"total"`
	Disputed int   `json:"disputed"`
}

// SpaceCounts handles GET /api/spaces/counts. Registered before /spaces/{id} so
// the literal segment wins the Go 1.22 ServeMux precedence over the wildcard.
func (s *Server) SpaceCounts(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	k, _ := auth.APIKeyFromContext(r.Context())

	// LEFT JOINs from the readable-space set so a space with zero pages still
	// gets a row (total 0). page_agreement is one-row-per-page, so the join never
	// multiplies and count(p.id) is an exact page count. Disputed reuses the
	// overview's definition: dispute > 0 on a cleanly-computed agreement row.
	args := []any{u.ID}
	q := `
		SELECT sm.space_id,
		       count(p.id) AS total,
		       count(p.id) FILTER (WHERE a.dispute > 0 AND a.last_error = '') AS disputed
		  FROM (SELECT DISTINCT space_id FROM space_access WHERE user_id = $1) sm
		  LEFT JOIN pages p ON p.space_id = sm.space_id AND p.deleted_at IS NULL
		  LEFT JOIN page_agreement a ON a.page_id = p.id`
	if k != nil && k.SpaceID != nil {
		args = append(args, *k.SpaceID)
		q += " WHERE sm.space_id = $2"
	}
	q += " GROUP BY sm.space_id"

	rows, err := s.DB.QueryContext(r.Context(), q, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "count spaces failed")
		return
	}
	defer rows.Close()

	out := []spaceCounts{}
	for rows.Next() {
		var c spaceCounts
		if err := rows.Scan(&c.SpaceID, &c.Total, &c.Disputed); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan space counts failed")
			return
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate space counts failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"spaces": out})
}
