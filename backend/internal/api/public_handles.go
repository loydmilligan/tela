package api

import (
	"database/sql"
	"errors"
	"net/http"
)

// Unified GitHub-style handle URLs. ONE namespace where {handle} is a user OR
// org public home and {handle}/{space-slug} is a public space. The backend for
// the FE routes /{handle} and /{handle}/{space-slug}.
//
// Both endpoints live under /api/public/ (auth.IsPublicPath) and are GET/read-
// only. They self-authenticate the only way the rest of /api/public/ does: by
// selecting ONLY visibility='public' rows. A private space can never surface —
// the WHERE clause is the gate. A handle with zero public presence is reported
// as 404, identical to an unknown handle, so we never confirm a private
// account/space exists.
//
// Handle resolution spans BOTH namespaces (users.username + orgs.slug). On the
// rare collision a USER wins (the reserved-words + cross-namespace guard in
// handle_guard.go keeps new signups from colliding, but legacy rows are
// grandfathered, so the resolver still has to pick).

const handleKindUser = "user"
const handleKindOrg = "org"

// handleSpaceDTO is one public-space card on a handle home — the projection
// shared with /api/public/discover (id/name/slug/description + the page_count
// and updated_at activity signals).
type handleSpaceDTO struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	PageCount   int64  `json:"page_count"`
	UpdatedAt   string `json:"updated_at"`
}

// GetPublicByHandle — GET /api/public/by-handle/{handle}. Resolves the handle
// across both namespaces (user precedence) and returns the account's PUBLIC
// spaces. 404 when the handle matches nothing OR matches but has no public
// space (no public presence).
func (s *Server) GetPublicByHandle(w http.ResponseWriter, r *http.Request) {
	handle := r.PathValue("handle")
	if handle == "" {
		writeError(w, http.StatusNotFound, "not_found", "no such handle")
		return
	}

	kind, ownerID, name, ok := s.resolveHandle(r, handle)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no such handle")
		return
	}

	spaces, err := s.publicSpacesForHandle(r, kind, ownerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "load handle spaces failed")
		return
	}
	// Public presence is having ≥1 public space. No public space → the home
	// doesn't exist publicly (don't confirm the account).
	if len(spaces) == 0 {
		writeError(w, http.StatusNotFound, "not_found", "no such handle")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"kind":   kind,
		"handle": handle,
		"name":   name,
		"spaces": spaces,
	})
}

// GetPublicByHandleSpace — GET /api/public/by-handle/{handle}/spaces/{slug}.
// Resolves handle → owner account, then that owner's PUBLIC space with the given
// slug, and returns the SAME envelope as GetPublicSpace ({"space": …}) so the
// reader can consume it unchanged. 404 on any miss (unknown handle, no such
// public space, private space).
func (s *Server) GetPublicByHandleSpace(w http.ResponseWriter, r *http.Request) {
	handle := r.PathValue("handle")
	slug := r.PathValue("slug")
	if handle == "" || slug == "" {
		writeError(w, http.StatusNotFound, "not_found", "no such public space")
		return
	}
	kind, ownerID, _, ok := s.resolveHandle(r, handle)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no such public space")
		return
	}

	id, owner, err := s.publicSpaceIDForHandle(r, kind, ownerID, slug)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "no such public space")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup space failed")
		return
	}
	sp, ok := s.requirePublicSpace(w, r, id)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"space": publicSpaceDTO{
			ID:          sp.ID,
			Name:        sp.Name,
			Slug:        sp.Slug,
			Visibility:  sp.Visibility,
			Description: sp.Description,
			OwnerHandle: owner,
		},
	})
}

// resolveHandle maps a handle to (kind, ownerID, displayName). User namespace
// wins on a collision. ok=false when the handle matches no user and no org.
func (s *Server) resolveHandle(r *http.Request, handle string) (kind string, ownerID int64, name string, ok bool) {
	var (
		uid         int64
		username    string
		displayName string
	)
	err := s.DB.QueryRowContext(r.Context(),
		`SELECT id, username, display_name FROM users WHERE LOWER(username) = LOWER($1)`, handle).
		Scan(&uid, &username, &displayName)
	if err == nil {
		n := displayName
		if n == "" {
			n = username
		}
		return handleKindUser, uid, n, true
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", 0, "", false
	}

	var (
		oid     int64
		orgName string
	)
	err = s.DB.QueryRowContext(r.Context(),
		`SELECT id, name FROM orgs WHERE LOWER(slug) = LOWER($1)`, handle).
		Scan(&oid, &orgName)
	if err == nil {
		return handleKindOrg, oid, orgName, true
	}
	return "", 0, "", false
}

// publicSpacesForHandle returns the owner account's PUBLIC spaces with the
// discover-style projection (page_count + last activity). For a user that means
// the spaces they own — their personal home (spaces.personal_user_id) or a team
// space they own (the space_members 'owner' row); for an org, spaces.org_id.
// Public-visibility only — never leaks a private space.
func (s *Server) publicSpacesForHandle(r *http.Request, kind string, ownerID int64) ([]handleSpaceDTO, error) {
	var where string
	switch kind {
	case handleKindOrg:
		where = `s.org_id = $1`
	default:
		where = `(s.personal_user_id = $1
		          OR EXISTS (SELECT 1 FROM space_members m
		                      WHERE m.space_id = s.id AND m.user_id = $1 AND m.role = 'owner'))`
	}
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT s.id, s.name, s.slug, s.description,
		       agg.page_count, agg.last_updated
		  FROM spaces s
		  LEFT JOIN LATERAL (
		         SELECT COUNT(*) AS page_count, MAX(p.updated_at) AS last_updated
		           FROM pages p
		          WHERE p.space_id = s.id AND p.deleted_at IS NULL
		       ) agg ON TRUE
		 WHERE s.visibility = 'public' AND `+where+`
		 ORDER BY agg.last_updated DESC NULLS LAST, s.id DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []handleSpaceDTO{}
	for rows.Next() {
		var (
			d           handleSpaceDTO
			lastUpdated *string
		)
		if err := rows.Scan(&d.ID, &d.Name, &d.Slug, &d.Description, &d.PageCount, &lastUpdated); err != nil {
			return nil, err
		}
		if lastUpdated != nil {
			d.UpdatedAt = *lastUpdated
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// publicSpaceIDForHandle finds the owner account's PUBLIC space with the given
// slug, returning its id and the owner's handle (for the byline). sql.ErrNoRows
// when no such public space exists for that owner.
func (s *Server) publicSpaceIDForHandle(r *http.Request, kind string, ownerID int64, slug string) (int64, string, error) {
	var (
		id    int64
		owner string
	)
	if kind == handleKindOrg {
		err := s.DB.QueryRowContext(r.Context(),
			`SELECT s.id, COALESCE((SELECT u.username FROM space_members m JOIN users u ON u.id = m.user_id
			                         WHERE m.space_id = s.id AND m.role = 'owner' ORDER BY m.user_id ASC LIMIT 1), '')
			   FROM spaces s
			  WHERE s.org_id = $1 AND s.slug = $2 AND s.visibility = 'public'
			  LIMIT 1`, ownerID, slug).Scan(&id, &owner)
		return id, owner, err
	}
	// User: their personal home OR a team space they own. The byline handle is
	// the user themselves.
	err := s.DB.QueryRowContext(r.Context(),
		`SELECT s.id, u.username
		   FROM spaces s JOIN users u ON u.id = $1
		  WHERE s.slug = $2 AND s.visibility = 'public'
		    AND (s.personal_user_id = $1
		         OR EXISTS (SELECT 1 FROM space_members m
		                     WHERE m.space_id = s.id AND m.user_id = $1 AND m.role = 'owner'))
		  LIMIT 1`, ownerID, slug).Scan(&id, &owner)
	return id, owner, err
}
