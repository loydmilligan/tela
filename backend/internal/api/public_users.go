package api

import (
	"database/sql"
	"errors"
	"net/http"
)

// Public user home page — GET /api/public/users/{username}. Powers /u/{handle}:
// a user's PUBLIC spaces and their top-level posts. Public (no auth), on
// auth.IsPublicPath via the /api/public/ prefix; read-only.
//
// Only **public** spaces the user is a **direct** member of are exposed — a
// private space, or one reached only via an org grant, never appears. If the
// user doesn't exist or has nothing public, the response is 404 (the profile
// simply doesn't exist publicly — we don't confirm an arbitrary username).

type publicUserSpaceDTO struct {
	ID    int64               `json:"id"`
	Name  string              `json:"name"`
	Slug  string              `json:"slug"`
	Pages []publicUserPageDTO `json:"pages"`
}

type publicUserPageDTO struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	UpdatedAt string `json:"updated_at"`
}

func (s *Server) GetPublicUser(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	if username == "" {
		writeError(w, http.StatusNotFound, "not_found", "no such user")
		return
	}

	var (
		userID    int64
		canonical string
	)
	err := s.DB.QueryRowContext(r.Context(),
		`SELECT id, username FROM users WHERE LOWER(username) = LOWER($1)`, username).
		Scan(&userID, &canonical)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "no such user")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup user failed")
		return
	}

	// One pass: the user's public spaces (direct membership) LEFT JOIN their
	// top-level pages, so a space with no posts still appears. Ordered by space
	// name, then the author's page arrangement.
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT s.id, s.name, s.slug, p.id, p.title, p.updated_at
		  FROM spaces s
		  JOIN space_members m ON m.space_id = s.id AND m.user_id = $1
		  LEFT JOIN pages p
		         ON p.space_id = s.id AND p.parent_id IS NULL AND p.deleted_at IS NULL
		 WHERE s.visibility = 'public'
		 ORDER BY s.name ASC, p.position ASC, p.id ASC`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "load user spaces failed")
		return
	}
	defer rows.Close()

	spaces := []publicUserSpaceDTO{}
	byID := map[int64]int{} // space id → index in spaces
	for rows.Next() {
		var (
			sID    int64
			sName  string
			sSlug  string
			pID    sql.NullInt64
			pTitle sql.NullString
			pTime  sql.NullString
		)
		if err := rows.Scan(&sID, &sName, &sSlug, &pID, &pTitle, &pTime); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan row failed")
			return
		}
		idx, ok := byID[sID]
		if !ok {
			spaces = append(spaces, publicUserSpaceDTO{
				ID: sID, Name: sName, Slug: sSlug, Pages: []publicUserPageDTO{},
			})
			idx = len(spaces) - 1
			byID[sID] = idx
		}
		if pID.Valid {
			spaces[idx].Pages = append(spaces[idx].Pages, publicUserPageDTO{
				ID:        pID.Int64,
				Title:     pTitle.String,
				UpdatedAt: pTime.String,
			})
		}
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate rows failed")
		return
	}

	// Nothing public → the profile doesn't exist publicly.
	if len(spaces) == 0 {
		writeError(w, http.StatusNotFound, "not_found", "no such user")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"user":   map[string]any{"username": canonical},
		"spaces": spaces,
	})
}
