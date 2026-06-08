package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/zcag/tela/backend/internal/auth"
	"github.com/zcag/tela/backend/internal/miraimport"
	"github.com/zcag/tela/backend/internal/models"
)

// M18-MiraImport.A.3. Wraps the miraimport package (A.1+A.2) behind a single
// HTTP endpoint: editor+ on the target space, accepts either an https URL on
// the configured allowlist (fetched server-side) or an inline payload, and
// returns the newly created page row using the same `{page: ...}` envelope as
// POST /api/pages.
//
// Caps: 1 MiB request body, 1 MiB fetched response body, 5s fetch timeout,
// https-only with exact host allowlist. The host allowlist is sourced from
// TELA_MIRA_ALLOWED_HOSTS (CSV); env unset → defaults to "mira.cagdas.io",
// env set to empty string → no allowed hosts (fail-closed).

const (
	miraDefaultAllowedHost = "mira.cagdas.io"
	miraMaxBodySize        = 1 << 20 // 1 MiB
	miraFetchTimeout       = 5 * time.Second
	miraSourceCommentTpl   = "\n\n<!-- mira-source: %s -->\n"
	// miraPasswordRespMaxSize bounds the read of a 401 body when probing for
	// mira's `{error: "password_required", unlock: "..."}` envelope so a
	// hostile (or buggy) upstream can't waste backend memory on a 401 path
	// that callers can't influence the size of via Content-Length.
	miraPasswordRespMaxSize = 4 << 10 // 4 KiB
)

// miraSlugPathRe matches a bare mira slug path like `/p/foo` or `/p/foo-bar`,
// where the slug component contains no dot (so already-suffixed `/p/foo.json`
// is a no-op) and no slash (so nested paths like `/p/foo/bar` are left alone).
// `/r/<token>` unlock URLs and any other shape pass through unchanged.
var miraSlugPathRe = regexp.MustCompile(`^/p/[^./]+$`)

type importMiraRequest struct {
	ParentID  *int64          `json:"parent_id"`
	SourceURL string          `json:"source_url"`
	Payload   json.RawMessage `json:"payload"`
}

// ImportMira handles POST /api/spaces/{id}/import-mira.
func (s *Server) ImportMira(w http.ResponseWriter, r *http.Request) {
	spaceID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}

	// 1 MiB cap on the inbound JSON body. Only meaningful for the payload
	// path — URL-mode requests are tiny — but applied uniformly so the
	// payload path can't be exploited to OOM the backend.
	r.Body = http.MaxBytesReader(w, r.Body, miraMaxBodySize)

	var req importMiraRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "bad_request", "request body exceeds 1 MiB")
			return
		}
		writeError(w, http.StatusBadRequest, "bad_request", "could not parse request body")
		return
	}

	k, _ := auth.APIKeyFromContext(r.Context())
	page, unlock, ae := s.importMiraCore(r.Context(), u, k, spaceID, req.ParentID, req.SourceURL, []byte(req.Payload))
	if unlock != "" {
		// Password-protected source: a non-error JSON carrying the unlock link.
		writeJSON(w, ae.Status, map[string]any{"error": ae.Message, "code": ae.Code, "unlock": unlock})
		return
	}
	if ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"page": page})
}

// importMiraCore is the transport-agnostic core behind POST
// /api/spaces/{id}/import-mira and the MCP import_mira tool: fetch (SSRF-guarded,
// https-only, allowlisted, no-redirect) or take an inline mira payload, convert
// it to markdown, and create a page (editor+). Returns a non-empty unlockURL
// (with the accompanying *apiErr describing it) when the source mira page is
// password-protected — neither a hard error nor a success.
func (s *Server) importMiraCore(ctx context.Context, u *auth.User, k *auth.APIKey, spaceID int64, parentID *int64, rawSourceURL string, payload []byte) (models.Page, string, *apiErr) {
	hasURL := strings.TrimSpace(rawSourceURL) != ""
	hasPayload := len(payload) > 0 && string(payload) != "null"
	if hasURL == hasPayload {
		return models.Page{}, "", &apiErr{http.StatusBadRequest, "bad_request", "exactly one of source_url or payload required"}
	}
	if parentID != nil && *parentID <= 0 {
		return models.Page{}, "", &apiErr{http.StatusBadRequest, "bad_request", "parent_id must be a positive integer"}
	}

	var (
		payloadBytes []byte
		sourceURL    string
	)
	if hasURL {
		sourceURL = strings.TrimSpace(rawSourceURL)
		parsed, perr := url.Parse(sourceURL)
		if perr != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return models.Page{}, "", &apiErr{http.StatusBadRequest, "bad_request", "source_url must be a valid https URL"}
		}
		host := strings.ToLower(parsed.Hostname())
		allowlist := parseAllowedMiraHosts()
		if _, allowed := allowlist[host]; !allowed {
			return models.Page{}, "", &apiErr{http.StatusForbidden, "forbidden", "source_url host is not on the mira allowlist"}
		}
		bodyBytes, status, code, msg, unlock := fetchMiraSource(ctx, sourceURL, allowlist)
		if status != 0 {
			return models.Page{}, unlock, &apiErr{status, code, msg}
		}
		payloadBytes = bodyBytes
	} else {
		payloadBytes = payload
	}

	title, body, err := miraimport.Convert(payloadBytes)
	if err != nil {
		return models.Page{}, "", &apiErr{http.StatusBadRequest, "bad_request", "invalid mira payload"}
	}
	if hasURL {
		body = body + fmt.Sprintf(miraSourceCommentTpl, sourceURL)
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return models.Page{}, "", &apiErr{http.StatusInternalServerError, "internal", "begin tx failed"}
	}
	defer tx.Rollback()

	if err := verifySpaceExistsTx(ctx, tx, spaceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Page{}, "", &apiErr{http.StatusNotFound, "space_not_found", "space not found"}
		}
		return models.Page{}, "", &apiErr{http.StatusInternalServerError, "internal", "lookup space failed"}
	}
	if ae := apiKeySpaceScopeErr(k, spaceID); ae != nil {
		return models.Page{}, "", ae
	}
	role, err := spaceRoleTx(ctx, tx, u.ID, spaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Page{}, "", &apiErr{http.StatusForbidden, "forbidden", "not a member"}
	}
	if err != nil {
		return models.Page{}, "", &apiErr{http.StatusInternalServerError, "internal", "lookup membership failed"}
	}
	if !canEdit(role) {
		return models.Page{}, "", &apiErr{http.StatusForbidden, "forbidden", "editor or owner role required"}
	}
	if ae := s.checkPageQuota(ctx, spaceID); ae != nil {
		return models.Page{}, "", ae
	}

	if parentID != nil {
		var parentSpaceID int64
		err := tx.QueryRowContext(ctx,
			`SELECT space_id FROM pages WHERE id = $1 AND deleted_at IS NULL`, *parentID).Scan(&parentSpaceID)
		if errors.Is(err, sql.ErrNoRows) {
			return models.Page{}, "", &apiErr{http.StatusBadRequest, "bad_request", "parent page does not exist"}
		}
		if err != nil {
			return models.Page{}, "", &apiErr{http.StatusInternalServerError, "internal", "lookup parent failed"}
		}
		if parentSpaceID != spaceID {
			return models.Page{}, "", &apiErr{http.StatusBadRequest, "bad_request", "parent page is in a different space"}
		}
	}

	var maxPos sql.NullInt64
	if parentID == nil {
		err = tx.QueryRowContext(ctx,
			`SELECT MAX(position) FROM pages WHERE space_id = $1 AND parent_id IS NULL AND deleted_at IS NULL`, spaceID).Scan(&maxPos)
	} else {
		err = tx.QueryRowContext(ctx,
			`SELECT MAX(position) FROM pages WHERE space_id = $1 AND parent_id = $2 AND deleted_at IS NULL`, spaceID, *parentID).Scan(&maxPos)
	}
	if err != nil {
		return models.Page{}, "", &apiErr{http.StatusInternalServerError, "internal", "compute position failed"}
	}
	var position int64
	if maxPos.Valid {
		position = maxPos.Int64 + 1
	}

	var id int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO pages(space_id, parent_id, title, body, position) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		spaceID, nullableInt64(parentID), title, body, position).Scan(&id)
	if err != nil {
		return models.Page{}, "", &apiErr{http.StatusInternalServerError, "internal", "create page failed"}
	}
	if err := syncPageLinks(ctx, tx, id, body); err != nil {
		return models.Page{}, "", &apiErr{http.StatusInternalServerError, "internal", "sync page_links failed"}
	}
	page, err := selectPageByIDTx(ctx, tx, id)
	if err != nil {
		return models.Page{}, "", &apiErr{http.StatusInternalServerError, "internal", "fetch created page failed"}
	}
	if err := tx.Commit(); err != nil {
		return models.Page{}, "", &apiErr{http.StatusInternalServerError, "internal", "commit failed"}
	}
	// Index the imported page's content (debounced; no-op when RAG is off). The
	// pre-extraction handler skipped this — imported pages are now searchable.
	s.rag.QueueReindex(id)
	return page, "", nil
}

// parseAllowedMiraHosts resolves the host allowlist on each call so tests can
// override via t.Setenv without process restart. Unset env → default to the
// production mira host. Empty string → no allowed hosts (fail-closed) so a
// misconfigured deploy cannot accidentally permit any host.
func parseAllowedMiraHosts() map[string]struct{} {
	raw, ok := os.LookupEnv("TELA_MIRA_ALLOWED_HOSTS")
	if !ok {
		return map[string]struct{}{miraDefaultAllowedHost: {}}
	}
	hosts := map[string]struct{}{}
	for _, h := range strings.Split(raw, ",") {
		h = strings.ToLower(strings.TrimSpace(h))
		if h != "" {
			hosts[h] = struct{}{}
		}
	}
	return hosts
}

// fetchMiraSource issues the upstream GET, validates the response, and reads
// the body within the 1 MiB cap. Returns (body, 0, "", "", "") on success;
// (nil, status, code, msg, unlock) on any failure (caller writes the envelope).
//
// Bare mira render URLs (`/p/<slug>`) serve `text/html`; the canonical JSON
// alternate lives at `/p/<slug>.json`. We rewrite at this single ingress so
// every caller (FE Settings, Milkdown paste-hook, MCP, raw curl) gets the
// JSON path transparently. The rewrite only runs against allowlisted hosts —
// the handler validates host membership before calling us, but we also pass
// the allowlist through so a future direct caller can't accidentally bypass
// the gate.
//
// 401 responses carrying mira's `{error: "password_required", unlock: "..."}`
// envelope surface as a distinct error code so clients can present the unlock
// URL to the user. All other non-2xx statuses collapse to the generic
// bad_request branch as before.
func fetchMiraSource(ctx context.Context, sourceURL string, allowlist map[string]struct{}) ([]byte, int, string, string, string) {
	if parsed, err := url.Parse(sourceURL); err == nil && parsed != nil {
		host := strings.ToLower(parsed.Hostname())
		if _, allowed := allowlist[host]; allowed && miraSlugPathRe.MatchString(parsed.Path) {
			parsed.Path = parsed.Path + ".json"
			sourceURL = parsed.String()
		}
	}

	// Disable redirect following: Go's default policy follows up to 10 hops
	// without re-validating the host against the allowlist, so a 30x to a
	// private/internal address would otherwise be followed blindly. Returning
	// the redirect response as-is surfaces it through the non-2xx branch below.
	client := &http.Client{
		Timeout: miraFetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, http.StatusBadRequest, "bad_request", "could not build source_url request", ""
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, http.StatusBadRequest, "bad_request", "could not fetch source_url", ""
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		if mediaType == "application/json" {
			limited := io.LimitReader(resp.Body, miraPasswordRespMaxSize+1)
			buf, readErr := io.ReadAll(limited)
			if readErr == nil && len(buf) <= miraPasswordRespMaxSize {
				var pr struct {
					Error  string `json:"error"`
					Unlock string `json:"unlock"`
				}
				if json.Unmarshal(buf, &pr) == nil && pr.Error == "password_required" && pr.Unlock != "" {
					return nil, http.StatusForbidden, "mira_password_required",
						"mira page is password-protected", pr.Unlock
				}
			}
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, http.StatusBadRequest, "bad_request", "source_url returned non-2xx status", ""
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return nil, http.StatusUnsupportedMediaType, "bad_request", "source_url must return application/json", ""
	}

	limited := io.LimitReader(resp.Body, miraMaxBodySize+1)
	bodyBytes, err := io.ReadAll(limited)
	if err != nil {
		return nil, http.StatusBadRequest, "bad_request", "could not read source_url response", ""
	}
	if len(bodyBytes) > miraMaxBodySize {
		return nil, http.StatusRequestEntityTooLarge, "bad_request", "source_url response exceeds 1 MiB", ""
	}
	return bodyBytes, 0, "", "", ""
}
