package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/zcag/tela/backend/internal/auth"
)

// M17.A.1 Feedback. Meta-feedback channel for Tela + tela-mcp themselves —
// bugs, friction, suggestions to the developers. DISTINCT from page-content
// comments (use /api/pages/{id}/comments for those). v0 is write-only: no
// GET endpoint, no admin UI; rows accumulate and PO inspects via psql.
//
// Auth: any authenticated caller (session OR bearer token). Bearer scope
// gating is relaxed at the middleware level for POST /api/feedback so that
// read-scope keys — the MCP `submit_feedback` tool's design scope — can
// submit. See auth.scopeAllowsRequest for the carve-out.

const (
	feedbackMaxSubjectLen = 200
	feedbackMaxBodyLen    = 8000
)

// feedbackDTO is the wire shape returned by CreateFeedback. Mirrors the
// columns 1:1; provenance pointers are nil when the row was inserted by a
// session caller (created_by_api_key_id) or after the underlying user/key
// has been deleted (ON DELETE SET NULL).
type feedbackDTO struct {
	ID                int64  `json:"id"`
	CreatedAt         string `json:"created_at"`
	CreatedByUserID   *int64 `json:"created_by_user_id"`
	CreatedByAPIKeyID *int64 `json:"created_by_api_key_id"`
	Subject           string `json:"subject"`
	Body              string `json:"body"`
}

type feedbackCreateRequest struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// CreateFeedback — POST /api/feedback. Validates trimmed subject + body
// against the migration's CHECK lengths, stamps whichever auth context
// applies (session → user_id only; bearer → user_id + api_key_id), and
// returns the inserted row.
func (s *Server) CreateFeedback(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req feedbackCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not parse request body")
		return
	}
	k, _ := auth.APIKeyFromContext(r.Context())
	dto, ae := s.feedbackCore(r.Context(), u, k, req)
	if ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"feedback": dto})
}

// feedbackCore is the transport-agnostic core behind POST /api/feedback and the
// MCP submit_feedback tool: validate subject + body, stamp the provenance
// (user always; api key when bearer-authed), insert, and return the row. Allowed
// for any scope, including read (the carve-out lives in scopeAllowsRequest /
// mcpRequireWrite is intentionally NOT called for this tool).
func (s *Server) feedbackCore(ctx context.Context, u *auth.User, k *auth.APIKey, req feedbackCreateRequest) (feedbackDTO, *apiErr) {
	subject := strings.TrimSpace(req.Subject)
	if subject == "" || len(subject) > feedbackMaxSubjectLen {
		return feedbackDTO{}, &apiErr{http.StatusBadRequest, "bad_request", "subject must be 1-200 characters"}
	}
	body := strings.TrimSpace(req.Body)
	if body == "" || len(body) > feedbackMaxBodyLen {
		return feedbackDTO{}, &apiErr{http.StatusBadRequest, "bad_request", "body must be 1-8000 characters"}
	}

	// OAuth-authed MCP callers (claude.ai/cowork) get a synthetic APIKey with no
	// persisted row (ID 0) — see verifyWorkOSToken. Stamping that into the
	// created_by_api_key_id FK violates the api_keys reference and 500s the insert,
	// so only record a real (persisted) key id; OAuth callers record user only.
	var apiKeyArg any = nil
	if k != nil && k.ID != 0 {
		apiKeyArg = k.ID
	}

	var id int64
	err := s.DB.QueryRowContext(ctx, `
		INSERT INTO feedback (created_by_user_id, created_by_api_key_id, subject, body)
		VALUES ($1, $2, $3, $4) RETURNING id`, u.ID, apiKeyArg, subject, body).Scan(&id)
	if err != nil {
		return feedbackDTO{}, &apiErr{http.StatusInternalServerError, "internal", "insert feedback failed"}
	}
	dto, err := selectFeedbackByID(ctx, s.DB, id)
	if err != nil {
		return feedbackDTO{}, &apiErr{http.StatusInternalServerError, "internal", "fetch created feedback failed"}
	}
	return dto, nil
}

// feedbackAdminEntry is the read shape for the instance-admin inbox: the row plus
// the submitter's username (joined; nil for a deleted/anonymous user) and a flag
// for whether it came through an API key / agent.
type feedbackAdminEntry struct {
	ID        int64   `json:"id"`
	CreatedAt string  `json:"created_at"`
	Subject   string  `json:"subject"`
	Body      string  `json:"body"`
	UserID    *int64  `json:"user_id"`
	Username  *string `json:"username"`
	ViaAPIKey bool    `json:"via_api_key"`
}

// ListFeedback returns the most recent feedback across the instance, newest first.
// Instance-admin only — feedback is global (about tela itself), not org-scoped.
func (s *Server) ListFeedback(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireInstanceAdmin(w, r); !ok {
		return
	}
	limit := clampLimit(r.URL.Query().Get("limit"), 100, 200)
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT f.id, f.created_at, f.subject, f.body, f.created_by_user_id, u.username,
		       CASE WHEN f.created_by_api_key_id IS NOT NULL THEN 1 ELSE 0 END
		  FROM feedback f
		  LEFT JOIN users u ON u.id = f.created_by_user_id
		 ORDER BY f.id DESC
		 LIMIT $1`, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list feedback failed")
		return
	}
	defer rows.Close()
	entries := []feedbackAdminEntry{}
	for rows.Next() {
		var (
			e        feedbackAdminEntry
			userID   sql.NullInt64
			username sql.NullString
			viaKey   int
		)
		if err := rows.Scan(&e.ID, &e.CreatedAt, &e.Subject, &e.Body, &userID, &username, &viaKey); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan feedback row failed")
			return
		}
		if userID.Valid {
			e.UserID = &userID.Int64
		}
		e.Username = nullableString(username)
		e.ViaAPIKey = viaKey == 1
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate feedback failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"feedback": entries})
}

func selectFeedbackByID(ctx context.Context, q *sql.DB, id int64) (feedbackDTO, error) {
	row := q.QueryRowContext(ctx, `
		SELECT id, created_at, created_by_user_id, created_by_api_key_id, subject, body
		  FROM feedback WHERE id = $1`, id)
	return scanFeedback(row)
}

func scanFeedback(r rowScanner) (feedbackDTO, error) {
	var (
		dto      feedbackDTO
		userID   sql.NullInt64
		apiKeyID sql.NullInt64
	)
	if err := r.Scan(&dto.ID, &dto.CreatedAt, &userID, &apiKeyID, &dto.Subject, &dto.Body); err != nil {
		return feedbackDTO{}, err
	}
	if userID.Valid {
		v := userID.Int64
		dto.CreatedByUserID = &v
	}
	if apiKeyID.Valid {
		v := apiKeyID.Int64
		dto.CreatedByAPIKeyID = &v
	}
	return dto, nil
}
