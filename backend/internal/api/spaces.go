package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	sqlitedrv "modernc.org/sqlite"

	"github.com/zcag/tela/backend/internal/models"
)

const (
	maxSpaceNameLen = 200
	maxSpaceSlugLen = 100

	// SQLITE_CONSTRAINT_UNIQUE — extended error code for UNIQUE violations.
	sqliteConstraintUnique = 2067
)

var (
	slugValidRe     = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	slugNormalizeRe = regexp.MustCompile(`[^a-z0-9]+`)
)

type spaceCreateRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type spaceUpdateRequest struct {
	Name *string `json:"name"`
	Slug *string `json:"slug"`
}

func (s *Server) ListSpaces(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT id, name, slug, created_at, updated_at FROM spaces ORDER BY name ASC`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list spaces failed")
		return
	}
	defer rows.Close()

	spaces := []models.Space{}
	for rows.Next() {
		var sp models.Space
		if err := rows.Scan(&sp.ID, &sp.Name, &sp.Slug, &sp.CreatedAt, &sp.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan space row failed")
			return
		}
		spaces = append(spaces, sp)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate spaces failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"spaces": spaces})
}

func (s *Server) CreateSpace(w http.ResponseWriter, r *http.Request) {
	var req spaceCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_name", "name is required")
		return
	}
	if len(name) > maxSpaceNameLen {
		writeError(w, http.StatusBadRequest, "invalid_name", "name exceeds 200 characters")
		return
	}

	slug := strings.TrimSpace(req.Slug)
	if slug == "" {
		slug = normalizeSlug(name)
		if slug == "" {
			writeError(w, http.StatusBadRequest, "invalid_name", "cannot derive a slug from the given name")
			return
		}
		if len(slug) > maxSpaceSlugLen {
			slug = slug[:maxSpaceSlugLen]
			slug = strings.TrimRight(slug, "-")
		}
	} else {
		if len(slug) > maxSpaceSlugLen {
			writeError(w, http.StatusBadRequest, "invalid_slug", "slug exceeds 100 characters")
			return
		}
		if !slugValidRe.MatchString(slug) {
			writeError(w, http.StatusBadRequest, "invalid_slug", "slug must be lowercase alphanumeric segments joined by '-'")
			return
		}
	}

	res, err := s.DB.ExecContext(r.Context(),
		`INSERT INTO spaces(name, slug) VALUES (?, ?)`, name, slug)
	if err != nil {
		if isUniqueConstraintErr(err) {
			writeError(w, http.StatusConflict, "slug_conflict", "a space with that slug already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "create space failed")
		return
	}
	id, err := res.LastInsertId()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "create space: last insert id failed")
		return
	}
	sp, err := selectSpaceByID(r.Context(), s.DB, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch created space failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"space": sp})
}

func (s *Server) GetSpace(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	sp, err := selectSpaceByID(r.Context(), s.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "space not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch space failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"space": sp})
}

func (s *Server) UpdateSpace(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}

	var req spaceUpdateRequest
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
		if name == "" {
			writeError(w, http.StatusBadRequest, "invalid_name", "name cannot be empty")
			return
		}
		if len(name) > maxSpaceNameLen {
			writeError(w, http.StatusBadRequest, "invalid_name", "name exceeds 200 characters")
			return
		}
		sets = append(sets, "name = ?")
		args = append(args, name)
	}
	if req.Slug != nil {
		slug := strings.TrimSpace(*req.Slug)
		if slug == "" {
			writeError(w, http.StatusBadRequest, "invalid_slug", "slug cannot be empty")
			return
		}
		if len(slug) > maxSpaceSlugLen {
			writeError(w, http.StatusBadRequest, "invalid_slug", "slug exceeds 100 characters")
			return
		}
		if !slugValidRe.MatchString(slug) {
			writeError(w, http.StatusBadRequest, "invalid_slug", "slug must be lowercase alphanumeric segments joined by '-'")
			return
		}
		sets = append(sets, "slug = ?")
		args = append(args, slug)
	}
	sets = append(sets, "updated_at = datetime('now')")
	args = append(args, id)

	stmt := "UPDATE spaces SET " + strings.Join(sets, ", ") + " WHERE id = ?"
	res, err := s.DB.ExecContext(r.Context(), stmt, args...)
	if err != nil {
		if isUniqueConstraintErr(err) {
			writeError(w, http.StatusConflict, "slug_conflict", "a space with that slug already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "update space failed")
		return
	}
	n, err := res.RowsAffected()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "update space: rows affected failed")
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "space not found")
		return
	}
	sp, err := selectSpaceByID(r.Context(), s.DB, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch updated space failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"space": sp})
}

func (s *Server) DeleteSpace(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	res, err := s.DB.ExecContext(r.Context(), `DELETE FROM spaces WHERE id = ?`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "delete space failed")
		return
	}
	n, err := res.RowsAffected()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "delete space: rows affected failed")
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "space not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func selectSpaceByID(ctx context.Context, db *sql.DB, id int64) (models.Space, error) {
	var sp models.Space
	err := db.QueryRowContext(ctx,
		`SELECT id, name, slug, created_at, updated_at FROM spaces WHERE id = ?`, id,
	).Scan(&sp.ID, &sp.Name, &sp.Slug, &sp.CreatedAt, &sp.UpdatedAt)
	return sp, err
}

// normalizeSlug lowercases the input, replaces runs of non-alphanumeric
// characters with a single '-', and trims leading/trailing '-'.
func normalizeSlug(s string) string {
	lower := strings.ToLower(s)
	collapsed := slugNormalizeRe.ReplaceAllString(lower, "-")
	return strings.Trim(collapsed, "-")
}

func isUniqueConstraintErr(err error) bool {
	var sqlErr *sqlitedrv.Error
	if errors.As(err, &sqlErr) && sqlErr.Code() == sqliteConstraintUnique {
		return true
	}
	return false
}
