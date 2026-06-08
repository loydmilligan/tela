package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// org_branding.go: per-org visual branding (logo + accent) for the white-label
// custom-domain surface. Read into host-context (login screen + app shell);
// managed by org admins. Sibling to org_login_settings (sign-in methods).

// accentPattern bounds what we store as an --accent value: a hex color or an
// oklch()/rgb()/rgba() function. Note the SPA injects this via
// CSSStyleDeclaration.setProperty, which already rejects invalid values (no CSS
// breakout possible) — this is data hygiene, not the security boundary.
var accentPattern = regexp.MustCompile(`^(#[0-9a-fA-F]{3,8}|(oklch|rgb|rgba)\([0-9a-zA-Z%.,/\s-]+\))$`)

type orgBrandingDTO struct {
	LogoURL string `json:"logo_url"`
	Accent  string `json:"accent"`
}

// orgBranding returns an org's stored logo URL + accent (empty strings when
// unset — the SPA then falls back to tela branding).
func (s *Server) orgBranding(ctx context.Context, orgID int64) (logoURL, accent string) {
	_ = s.DB.QueryRowContext(ctx,
		`SELECT logo_url, accent FROM org_branding WHERE org_id = $1`, orgID).Scan(&logoURL, &accent)
	return
}

// GetOrgBranding — GET /api/orgs/{id}/branding. Org-admin.
func (s *Server) GetOrgBranding(w http.ResponseWriter, r *http.Request) {
	orgID, ok := parseOrgID(w, r)
	if !ok {
		return
	}
	if !s.requireOrgAdmin(w, r, orgID) {
		return
	}
	logoURL, accent := s.orgBranding(r.Context(), orgID)
	writeJSON(w, http.StatusOK, orgBrandingDTO{LogoURL: logoURL, Accent: accent})
}

// PutOrgBranding — PUT /api/orgs/{id}/branding. Org-admin. Empty fields clear
// the override.
func (s *Server) PutOrgBranding(w http.ResponseWriter, r *http.Request) {
	orgID, ok := parseOrgID(w, r)
	if !ok {
		return
	}
	if !s.requireOrgAdmin(w, r, orgID) {
		return
	}
	var req orgBrandingDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	req.LogoURL = strings.TrimSpace(req.LogoURL)
	req.Accent = strings.TrimSpace(req.Accent)
	if req.LogoURL != "" {
		u, err := url.Parse(req.LogoURL)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "logo URL must be an https:// URL")
			return
		}
	}
	if req.Accent != "" && !accentPattern.MatchString(req.Accent) {
		writeError(w, http.StatusBadRequest, "bad_request", "accent must be a hex color or oklch()/rgb() value")
		return
	}
	if _, err := s.DB.ExecContext(r.Context(), `
		INSERT INTO org_branding (org_id, logo_url, accent)
		VALUES ($1, $2, $3)
		ON CONFLICT (org_id) DO UPDATE
		   SET logo_url = EXCLUDED.logo_url, accent = EXCLUDED.accent, updated_at = tela_now()`,
		orgID, req.LogoURL, req.Accent); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "save branding failed")
		return
	}
	s.audit(r.Context(), r, "org_branding.set", "org", orgID, "")
	writeJSON(w, http.StatusOK, req)
}
