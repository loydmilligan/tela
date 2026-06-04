package api

import "net/http"

// mentionUserDTO is the minimal user projection for the @-mention picker —
// just enough to render and resolve a mention. No email-verification / admin /
// activity fields (that's the admin-only /api/admin/users list).
type mentionUserDTO struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email,omitempty"`
}

// ListUsers handles GET /api/users — the mention directory. Session-authed
// (any member); returns active users sorted by username. tela is a single-org
// team wiki, so listing usernames to authenticated members is expected.
func (s *Server) ListUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT id, username, COALESCE(email, '')
		  FROM users
		 WHERE is_active = 1
		 ORDER BY username ASC`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list users failed")
		return
	}
	defer rows.Close()

	users := []mentionUserDTO{}
	for rows.Next() {
		var u mentionUserDTO
		if err := rows.Scan(&u.ID, &u.Username, &u.Email); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan user row failed")
			return
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate users failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}
