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
// GET endpoint, no admin UI; rows accumulate and PO inspects via sqlite shell.
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
	subject := strings.TrimSpace(req.Subject)
	if subject == "" || len(subject) > feedbackMaxSubjectLen {
		writeError(w, http.StatusBadRequest, "bad_request", "subject must be 1-200 characters")
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" || len(body) > feedbackMaxBodyLen {
		writeError(w, http.StatusBadRequest, "bad_request", "body must be 1-8000 characters")
		return
	}

	var apiKeyArg any = nil
	if k, isBearer := auth.APIKeyFromContext(r.Context()); isBearer {
		apiKeyArg = k.ID
	}

	ctx := r.Context()
	var id int64
	err := s.DB.QueryRowContext(ctx, `
		INSERT INTO feedback (created_by_user_id, created_by_api_key_id, subject, body)
		VALUES ($1, $2, $3, $4) RETURNING id`, u.ID, apiKeyArg, subject, body).Scan(&id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "insert feedback failed")
		return
	}
	dto, err := selectFeedbackByID(ctx, s.DB, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch created feedback failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"feedback": dto})
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
