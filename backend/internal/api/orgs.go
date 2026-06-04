package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/zcag/tela/backend/internal/models"
)

const (
	maxOrgNameLen = 200
	maxOrgSlugLen = 100
)

// orgDTO is the wire shape for org listings. MyRole is the caller's org_role,
// or null when they administer the org as an instance-admin without a
// membership row.
type orgDTO struct {
	models.Org
	MemberCount int     `json:"member_count"`
	MyRole      *string `json:"my_role"`
}

type orgCreateRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type orgUpdateRequest struct {
	Name *string `json:"name"`
	Slug *string `json:"slug"`
}

// ListOrgs returns the caller's orgs. Instance-admins see every org (with
// my_role reflecting their own membership, if any). Ordered by name.
func (s *Server) ListOrgs(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	// Instance-admins get the full list so they can administer tenants; regular
	// users see only orgs they belong to. my_role is a LEFT JOIN either way.
	var (
		rows *sql.Rows
		err  error
	)
	base := `
		SELECT o.id, o.name, o.slug, o.created_at, o.updated_at,
		       (SELECT COUNT(*) FROM org_members m WHERE m.org_id = o.id) AS member_count,
		       om.org_role
		  FROM orgs o
		  LEFT JOIN org_members om ON om.org_id = o.id AND om.user_id = ?`
	if u.IsInstanceAdmin {
		rows, err = s.DB.QueryContext(r.Context(), base+` ORDER BY o.name ASC`, u.ID)
	} else {
		rows, err = s.DB.QueryContext(r.Context(),
			base+` WHERE om.user_id IS NOT NULL ORDER BY o.name ASC`, u.ID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list orgs failed")
		return
	}
	defer rows.Close()

	orgs := []orgDTO{}
	for rows.Next() {
		var (
			it   orgDTO
			role sql.NullString
		)
		if err := rows.Scan(&it.ID, &it.Name, &it.Slug, &it.CreatedAt, &it.UpdatedAt, &it.MemberCount, &role); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan org row failed")
			return
		}
		it.MyRole = nullableString(role)
		orgs = append(orgs, it)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate orgs failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"orgs": orgs})
}

// CreateOrg provisions a new org. Instance-admin only (curated multi-tenant
// rollout). The org starts empty — members are added via the members endpoint.
func (s *Server) CreateOrg(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireInstanceAdmin(w, r); !ok {
		return
	}
	var req orgCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > maxOrgNameLen {
		writeError(w, http.StatusBadRequest, "invalid_name", "name must be 1-200 characters")
		return
	}
	slug, ok := resolveOrgSlug(w, name, req.Slug)
	if !ok {
		return
	}

	res, err := s.DB.ExecContext(r.Context(),
		`INSERT INTO orgs(name, slug) VALUES (?, ?)`, name, slug)
	if err != nil {
		if isUniqueConstraintErr(err) {
			writeError(w, http.StatusConflict, "slug_conflict", "an org with that slug already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "create org failed")
		return
	}
	id, err := res.LastInsertId()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "create org: last insert id failed")
		return
	}
	org, err := selectOrgByID(r.Context(), s.DB, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch created org failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"org": org})
}

// GetOrg returns a single org. Member or instance-admin.
func (s *Server) GetOrg(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	if _, ok := s.requireOrgMember(w, r, id); !ok {
		return
	}
	org, err := selectOrgByID(r.Context(), s.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "org not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch org failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"org": org})
}

// UpdateOrg renames / re-slugs an org. Org admin or instance-admin.
func (s *Server) UpdateOrg(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	if !s.requireOrgAdmin(w, r, id) {
		return
	}
	var req orgUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	if req.Name == nil && req.Slug == nil {
		writeError(w, http.StatusBadRequest, "no_fields", "at least one of name, slug must be provided")
		return
	}

	sets := make([]string, 0, 3)
	args := make([]any, 0, 4)
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || len(name) > maxOrgNameLen {
			writeError(w, http.StatusBadRequest, "invalid_name", "name must be 1-200 characters")
			return
		}
		sets = append(sets, "name = ?")
		args = append(args, name)
	}
	if req.Slug != nil {
		slug := strings.TrimSpace(*req.Slug)
		if slug == "" || len(slug) > maxOrgSlugLen || !slugValidRe.MatchString(slug) {
			writeError(w, http.StatusBadRequest, "invalid_slug", "slug must be lowercase alphanumeric segments joined by '-'")
			return
		}
		sets = append(sets, "slug = ?")
		args = append(args, slug)
	}
	sets = append(sets, "updated_at = datetime('now')")
	args = append(args, id)

	stmt := "UPDATE orgs SET " + strings.Join(sets, ", ") + " WHERE id = ?"
	if _, err := s.DB.ExecContext(r.Context(), stmt, args...); err != nil {
		if isUniqueConstraintErr(err) {
			writeError(w, http.StatusConflict, "slug_conflict", "an org with that slug already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "update org failed")
		return
	}
	org, err := selectOrgByID(r.Context(), s.DB, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch updated org failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"org": org})
}

// DeleteOrg removes an org. Instance-admin only — it's a tenant-level
// destructive op. org_members and org_email_domains cascade via FK; spaces.org_id
// is SET NULL via FK; the org's space_grants have no FK (polymorphic principal),
// so they're deleted explicitly in the same tx.
func (s *Server) DeleteOrg(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireInstanceAdmin(w, r); !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "begin tx failed")
		return
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM space_grants WHERE principal_kind = 'org' AND principal_id = ?`, id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "remove org grants failed")
		return
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM orgs WHERE id = ?`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "delete org failed")
		return
	}
	n, err := res.RowsAffected()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "delete org: rows affected failed")
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "org not found")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "commit failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resolveOrgSlug validates an explicit slug or derives one from the name,
// writing the 400 envelope and returning false on failure.
func resolveOrgSlug(w http.ResponseWriter, name, raw string) (string, bool) {
	slug := strings.TrimSpace(raw)
	if slug == "" {
		slug = normalizeSlug(name)
		if slug == "" {
			writeError(w, http.StatusBadRequest, "invalid_name", "cannot derive a slug from the given name")
			return "", false
		}
		if len(slug) > maxOrgSlugLen {
			slug = strings.TrimRight(slug[:maxOrgSlugLen], "-")
		}
		return slug, true
	}
	if len(slug) > maxOrgSlugLen || !slugValidRe.MatchString(slug) {
		writeError(w, http.StatusBadRequest, "invalid_slug", "slug must be lowercase alphanumeric segments joined by '-'")
		return "", false
	}
	return slug, true
}

func selectOrgByID(ctx context.Context, db *sql.DB, id int64) (models.Org, error) {
	var o models.Org
	err := db.QueryRowContext(ctx,
		`SELECT id, name, slug, created_at, updated_at FROM orgs WHERE id = ?`, id,
	).Scan(&o.ID, &o.Name, &o.Slug, &o.CreatedAt, &o.UpdatedAt)
	return o, err
}
