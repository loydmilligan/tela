package api

import (
	"net/http"
	"sort"
)

// The effective-access panel: the one place that answers "who can access this
// space, how, and at what role" (docs/access-model.md). Resolves every user
// with access, the source(s) they reach it through (direct / via org), and their
// single effective role (max by precedence). When groups land, add a third
// UNION leg (principal_kind='group') + source kind 'group' — the DTO and the
// aggregation already handle arbitrary sources.

type accessSource struct {
	Kind string  `json:"kind"` // "direct" | "org" | "group"
	Role string  `json:"role"`
	Name *string `json:"name,omitempty"` // org/group name; nil for direct
}

type spaceAccessEntry struct {
	UserID        int64          `json:"user_id"`
	Username      string         `json:"username"`
	Email         *string        `json:"email"`
	EffectiveRole string         `json:"effective_role"`
	Sources       []accessSource `json:"sources"`
}

// roleRank orders the space roles by precedence for max-role resolution.
func roleRank(role string) int {
	switch role {
	case roleOwner:
		return 3
	case roleEditor:
		return 2
	default:
		return 1
	}
}

// GetSpaceAccess returns the resolved access list for a space. Any member may
// read (same gate as ListSpaceMembers).
func (s *Server) GetSpaceAccess(w http.ResponseWriter, r *http.Request) {
	spaceID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	if _, ok := s.requireMembership(w, r, spaceID); !ok {
		return
	}

	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT u.id, u.username, u.email, sm.role AS role,
		       'direct' AS source_kind, NULL AS source_name
		  FROM space_members sm
		  JOIN users u ON u.id = sm.user_id
		 WHERE sm.space_id = ?
		UNION ALL
		SELECT u.id, u.username, u.email, sg.role,
		       'org' AS source_kind, o.name AS source_name
		  FROM space_grants sg
		  JOIN org_members om ON om.org_id = sg.principal_id
		  JOIN orgs o ON o.id = sg.principal_id
		  JOIN users u ON u.id = om.user_id
		 WHERE sg.space_id = ? AND sg.principal_kind = 'org'
		UNION ALL
		SELECT u.id, u.username, u.email, sg.role,
		       'group' AS source_kind, g.name AS source_name
		  FROM space_grants sg
		  JOIN group_members gm ON gm.group_id = sg.principal_id
		  JOIN groups g ON g.id = sg.principal_id
		  JOIN users u ON u.id = gm.user_id
		 WHERE sg.space_id = ? AND sg.principal_kind = 'group'`, spaceID, spaceID, spaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "resolve access failed")
		return
	}
	defer rows.Close()

	// Aggregate rows into one entry per user, collecting sources and the max
	// effective role.
	byUser := map[int64]*spaceAccessEntry{}
	order := []int64{}
	for rows.Next() {
		var (
			uid              int64
			username         string
			email            *string
			role, sourceKind string
			sourceName       *string
		)
		if err := rows.Scan(&uid, &username, &email, &role, &sourceKind, &sourceName); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan access row failed")
			return
		}
		e, seen := byUser[uid]
		if !seen {
			e = &spaceAccessEntry{UserID: uid, Username: username, Email: email, EffectiveRole: role}
			byUser[uid] = e
			order = append(order, uid)
		}
		if roleRank(role) > roleRank(e.EffectiveRole) {
			e.EffectiveRole = role
		}
		e.Sources = append(e.Sources, accessSource{Kind: sourceKind, Role: role, Name: sourceName})
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate access failed")
		return
	}

	entries := make([]spaceAccessEntry, 0, len(order))
	for _, uid := range order {
		entries = append(entries, *byUser[uid])
	}
	// Sort owner → editor → viewer, then username, for a stable, readable panel.
	sort.SliceStable(entries, func(i, j int) bool {
		ri, rj := roleRank(entries[i].EffectiveRole), roleRank(entries[j].EffectiveRole)
		if ri != rj {
			return ri > rj
		}
		return entries[i].Username < entries[j].Username
	})
	writeJSON(w, http.StatusOK, map[string]any{"access": entries})
}
