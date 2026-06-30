package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/zcag/tela/backend/internal/models"
)

const (
	maxOrgNameLen = 200
	maxOrgSlugLen = 100
	// maxSelfServeOrgs caps how many orgs a non-admin user can create (counted as
	// org-admin memberships). Beyond it they're pointed at sales — keeps casual
	// abuse bounded while letting a real user spin up a team or two.
	maxSelfServeOrgs = 3
)

// orgSelfServeEnabled reports whether non-admins may create organizations. The
// `allow_org_self_serve` instance setting overrides; unset → defaults to the
// managed cloud (on for telawiki.com, off for a self-host instance, which keeps
// the curated-tenant posture unless an operator opts in).
func (s *Server) orgSelfServeEnabled() bool {
	if v, ok := s.settings.Get("allow_org_self_serve"); ok {
		return v == "1" || v == "true"
	}
	return s.managedCloud
}

// orgDTO is the wire shape for org listings. MyRole is the caller's org_role,
// or null when they administer the org as an instance-admin without a
// membership row.
type orgDTO struct {
	models.Org
	MemberCount int     `json:"member_count"`
	MyRole      *string `json:"my_role"`
	PlanKey     string  `json:"plan_key"`
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
		       om.org_role, o.plan_key
		  FROM orgs o
		  LEFT JOIN org_members om ON om.org_id = o.id AND om.user_id = $1`
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
		if err := rows.Scan(&it.ID, &it.Name, &it.Slug, &it.CreatedAt, &it.UpdatedAt, &it.MemberCount, &role, &it.PlanKey); err != nil {
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
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	// Self-serve: any authenticated (hence email-verified) user can create a team,
	// gated by the instance flag and a per-user cap. Instance admins are exempt —
	// they provision tenants without limit.
	if !u.IsInstanceAdmin {
		if !s.orgSelfServeEnabled() {
			writeError(w, http.StatusForbidden, "forbidden", "creating organizations is disabled on this instance")
			return
		}
		var owned int
		if err := s.DB.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM org_members WHERE user_id = $1 AND org_role = 'admin'`, u.ID).Scan(&owned); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "check org limit failed")
			return
		}
		if owned >= maxSelfServeOrgs {
			writeError(w, http.StatusPaymentRequired, "org_limit",
				fmt.Sprintf("you can create up to %d organizations — contact tela@telawiki.com for more", maxSelfServeOrgs))
			return
		}
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
	// Unified-handle guard: the org slug shares one public namespace with user
	// usernames, so reject a reserved word or a collision with an existing
	// username (the slug's own uniqueness is caught by the INSERT below).
	if ae := checkHandleAvailable(r.Context(), slug, usernameTaken, s.DB); ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "begin tx failed")
		return
	}
	defer tx.Rollback()

	var id int64
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO orgs(name, slug) VALUES ($1, $2) RETURNING id`, name, slug).Scan(&id); err != nil {
		if isUniqueConstraintErr(err) {
			writeError(w, http.StatusConflict, "slug_conflict", "an org with that slug already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "create org failed")
		return
	}
	// The creator is the org's first admin — this is what makes self-serve work
	// (otherwise only an instance admin could administer the org afterward).
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO org_members (org_id, user_id, org_role) VALUES ($1, $2, 'admin')`, id, u.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "add creator failed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "commit failed")
		return
	}
	org, err := selectOrgByID(ctx, s.DB, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch created org failed")
		return
	}
	s.audit(r.Context(), r, "org.create", "org", id, org.Name)
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
	n := 0
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || len(name) > maxOrgNameLen {
			writeError(w, http.StatusBadRequest, "invalid_name", "name must be 1-200 characters")
			return
		}
		n++
		sets = append(sets, "name = $"+strconv.Itoa(n))
		args = append(args, name)
	}
	if req.Slug != nil {
		slug := strings.TrimSpace(*req.Slug)
		if slug == "" || len(slug) > maxOrgSlugLen || !slugValidRe.MatchString(slug) {
			writeError(w, http.StatusBadRequest, "invalid_slug", "slug must be lowercase alphanumeric segments joined by '-'")
			return
		}
		n++
		sets = append(sets, "slug = $"+strconv.Itoa(n))
		args = append(args, slug)
	}
	sets = append(sets, "updated_at = tela_now()")
	n++
	args = append(args, id)

	stmt := "UPDATE orgs SET " + strings.Join(sets, ", ") + " WHERE id = $" + strconv.Itoa(n)
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
		`DELETE FROM space_grants WHERE principal_kind = 'org' AND principal_id = $1`, id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "remove org grants failed")
		return
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM orgs WHERE id = $1`, id)
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
	s.audit(ctx, r, "org.delete", "org", id, "")
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
		`SELECT id, name, slug, created_at, updated_at FROM orgs WHERE id = $1`, id,
	).Scan(&o.ID, &o.Name, &o.Slug, &o.CreatedAt, &o.UpdatedAt)
	return o, err
}
