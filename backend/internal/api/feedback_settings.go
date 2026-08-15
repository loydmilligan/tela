package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// Feedback routing admin surface (issue-tracker phase 2a): receiver groups,
// per-user permissions (may use the widget / may request Claude triage), and
// the composer's default target. Admin-gated except GetFeedbackOptions, which
// powers the composer for every signed-in user.
// Schema: migration 0070_feedback_routing.sql. Design: the Issues space
// "About — Issue Tracker" page (phase 2 section).

type feedbackGroupDTO struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	MemberIDs []int64 `json:"member_ids"`
}

type feedbackPermissionDTO struct {
	UserID      int64  `json:"user_id"`
	Username    string `json:"username"`
	Enabled     bool   `json:"enabled"`
	AllowClaude bool   `json:"allow_claude"`
}

type feedbackSettingsDTO struct {
	DefaultUserID  *int64                  `json:"default_user_id"`
	DefaultGroupID *int64                  `json:"default_group_id"`
	Groups         []feedbackGroupDTO      `json:"groups"`
	Permissions    []feedbackPermissionDTO `json:"permissions"`
}

func (s *Server) loadFeedbackGroups(ctx context.Context) ([]feedbackGroupDTO, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, name FROM feedback_groups ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := []feedbackGroupDTO{}
	for rows.Next() {
		var g feedbackGroupDTO
		if err := rows.Scan(&g.ID, &g.Name); err != nil {
			return nil, err
		}
		g.MemberIDs = []int64{}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	mrows, err := s.DB.QueryContext(ctx, `SELECT group_id, user_id FROM feedback_group_members ORDER BY user_id`)
	if err != nil {
		return nil, err
	}
	defer mrows.Close()
	byID := map[int64]*feedbackGroupDTO{}
	for i := range groups {
		byID[groups[i].ID] = &groups[i]
	}
	for mrows.Next() {
		var gid, uid int64
		if err := mrows.Scan(&gid, &uid); err != nil {
			return nil, err
		}
		if g := byID[gid]; g != nil {
			g.MemberIDs = append(g.MemberIDs, uid)
		}
	}
	return groups, mrows.Err()
}

// GetFeedbackAdminSettings — GET /api/admin/feedback/settings. One bundle:
// defaults + groups (with members) + the per-user permission matrix.
func (s *Server) GetFeedbackAdminSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireInstanceAdmin(w, r); !ok {
		return
	}
	out := feedbackSettingsDTO{Permissions: []feedbackPermissionDTO{}}
	err := s.DB.QueryRowContext(r.Context(),
		`SELECT default_user_id, default_group_id FROM feedback_settings WHERE id = 1`).
		Scan(&out.DefaultUserID, &out.DefaultGroupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "settings read failed")
		return
	}
	out.Groups, err = s.loadFeedbackGroups(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "groups read failed")
		return
	}
	// Every active user with their effective permission bits (missing row =
	// enabled, no claude — same defaults feedbackCore enforces).
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT u.id, u.username,
		       COALESCE(p.enabled, 1), COALESCE(p.allow_claude, 0)
		FROM users u
		LEFT JOIN feedback_permissions p ON p.user_id = u.id
		WHERE u.is_active = 1 AND u.deleted_at IS NULL
		ORDER BY u.username`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "permissions read failed")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var p feedbackPermissionDTO
		var enabled, allow int
		if err := rows.Scan(&p.UserID, &p.Username, &enabled, &allow); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "permissions scan failed")
			return
		}
		p.Enabled = enabled == 1
		p.AllowClaude = allow == 1
		out.Permissions = append(out.Permissions, p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": out})
}

// UpdateFeedbackSettings — PUT /api/admin/feedback/settings. Sets the
// composer's default target; exactly one of user/group may be set (or both
// null to clear).
func (s *Server) UpdateFeedbackSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireInstanceAdmin(w, r); !ok {
		return
	}
	var req struct {
		DefaultUserID  *int64 `json:"default_user_id"`
		DefaultGroupID *int64 `json:"default_group_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not parse request body")
		return
	}
	if req.DefaultUserID != nil && req.DefaultGroupID != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "set a default user OR a default group, not both")
		return
	}
	_, err := s.DB.ExecContext(r.Context(),
		`UPDATE feedback_settings SET default_user_id = $1, default_group_id = $2 WHERE id = 1`,
		req.DefaultUserID, req.DefaultGroupID)
	if err != nil {
		// FK violation → the referenced user/group doesn't exist.
		writeError(w, http.StatusBadRequest, "bad_request", "unknown default user or group")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type feedbackGroupRequest struct {
	Name      string  `json:"name"`
	MemberIDs []int64 `json:"member_ids"`
}

func (s *Server) writeFeedbackGroupMembers(ctx context.Context, groupID int64, memberIDs []int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM feedback_group_members WHERE group_id = $1`, groupID); err != nil {
		return err
	}
	for _, uid := range memberIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO feedback_group_members (group_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			groupID, uid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CreateFeedbackGroup — POST /api/admin/feedback/groups.
func (s *Server) CreateFeedbackGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireInstanceAdmin(w, r); !ok {
		return
	}
	var req feedbackGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not parse request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 100 {
		writeError(w, http.StatusBadRequest, "bad_request", "name must be 1-100 characters")
		return
	}
	var id int64
	err := s.DB.QueryRowContext(r.Context(),
		`INSERT INTO feedback_groups (name) VALUES ($1) RETURNING id`, name).Scan(&id)
	if err != nil {
		writeError(w, http.StatusConflict, "conflict", "a group with that name already exists")
		return
	}
	if err := s.writeFeedbackGroupMembers(r.Context(), id, req.MemberIDs); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "unknown member user id")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"group": feedbackGroupDTO{ID: id, Name: name, MemberIDs: req.MemberIDs},
	})
}

// UpdateFeedbackGroup — PUT /api/admin/feedback/groups/{id}. A non-empty name
// renames; a non-nil member_ids replaces the membership.
func (s *Server) UpdateFeedbackGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireInstanceAdmin(w, r); !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid group id")
		return
	}
	var req feedbackGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not parse request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name != "" {
		if len(name) > 100 {
			writeError(w, http.StatusBadRequest, "bad_request", "name must be 1-100 characters")
			return
		}
		res, err := s.DB.ExecContext(r.Context(),
			`UPDATE feedback_groups SET name = $1 WHERE id = $2`, name, id)
		if err != nil {
			writeError(w, http.StatusConflict, "conflict", "a group with that name already exists")
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			writeError(w, http.StatusNotFound, "not_found", "group not found")
			return
		}
	} else {
		var x int64
		if err := s.DB.QueryRowContext(r.Context(),
			`SELECT id FROM feedback_groups WHERE id = $1`, id).Scan(&x); errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "group not found")
			return
		}
	}
	if req.MemberIDs != nil {
		if err := s.writeFeedbackGroupMembers(r.Context(), id, req.MemberIDs); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "unknown member user id")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// DeleteFeedbackGroup — DELETE /api/admin/feedback/groups/{id}. Members and
// any settings/feedback references clear via FK actions.
func (s *Server) DeleteFeedbackGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireInstanceAdmin(w, r); !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid group id")
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), `DELETE FROM feedback_groups WHERE id = $1`, id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "delete failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// UpdateFeedbackPermission — PUT /api/admin/feedback/permissions/{id} (user id).
func (s *Server) UpdateFeedbackPermission(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireInstanceAdmin(w, r); !ok {
		return
	}
	uid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || uid <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid user id")
		return
	}
	var req struct {
		Enabled     bool `json:"enabled"`
		AllowClaude bool `json:"allow_claude"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not parse request body")
		return
	}
	b := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}
	_, err = s.DB.ExecContext(r.Context(), `
		INSERT INTO feedback_permissions (user_id, enabled, allow_claude) VALUES ($1, $2, $3)
		ON CONFLICT (user_id) DO UPDATE SET enabled = $2, allow_claude = $3`,
		uid, b(req.Enabled), b(req.AllowClaude))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "unknown user id")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// feedbackSenderPermsCtx returns a user's effective widget permissions
// (missing row = enabled, no Claude). Shared by the composer bootstrap and
// the feedbackCore gate.
func (s *Server) feedbackSenderPermsCtx(ctx context.Context, userID int64) (enabled, allowClaude bool, err error) {
	var e, a int
	err = s.DB.QueryRowContext(ctx, `
		SELECT COALESCE(p.enabled, 1), COALESCE(p.allow_claude, 0)
		FROM users u LEFT JOIN feedback_permissions p ON p.user_id = u.id
		WHERE u.id = $1`, userID).Scan(&e, &a)
	return e == 1, a == 1, err
}

// GetFeedbackOptions — GET /api/feedback/options. Composer bootstrap for any
// signed-in user: own permission bits, the pickable recipients, the default.
func (s *Server) GetFeedbackOptions(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	enabled, allowClaude, err := s.feedbackSenderPermsCtx(r.Context(), u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "permissions read failed")
		return
	}
	type userOpt struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	}
	type groupOpt struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	users := []userOpt{}
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT id, username FROM users
		WHERE is_active = 1 AND deleted_at IS NULL ORDER BY username`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "users read failed")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var uo userOpt
		if err := rows.Scan(&uo.ID, &uo.Username); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "users scan failed")
			return
		}
		users = append(users, uo)
	}
	groups := []groupOpt{}
	grows, err := s.DB.QueryContext(r.Context(), `SELECT id, name FROM feedback_groups ORDER BY name`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "groups read failed")
		return
	}
	defer grows.Close()
	for grows.Next() {
		var g groupOpt
		if err := grows.Scan(&g.ID, &g.Name); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "groups scan failed")
			return
		}
		groups = append(groups, g)
	}
	var defUser, defGroup *int64
	_ = s.DB.QueryRowContext(r.Context(),
		`SELECT default_user_id, default_group_id FROM feedback_settings WHERE id = 1`).
		Scan(&defUser, &defGroup)
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":      enabled,
		"allow_claude": allowClaude,
		"users":        users,
		"groups":       groups,
		"default": map[string]any{
			"user_id":  defUser,
			"group_id": defGroup,
		},
	})
}
