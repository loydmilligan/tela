package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/zcag/tela/backend/internal/ee"
)

// Self-host Enterprise licensing. A signed offline key (internal/ee) unlocks
// ee-gated features; api.entitled() consults s.license. The key is set either by
// env (TELA_LICENSE_KEY, takes precedence) or at runtime via this admin API,
// persisted in instance_settings under the secret/ prefix so it stays out of the
// general settings dump.

// licenseTokenSettingKey holds the installed key. secret/ prefix → All() omits
// it from the admin settings listing (it has its own status-only endpoint).
const licenseTokenSettingKey = "secret/license_key"

// loadLicense resolves and verifies the active Enterprise license — env first,
// then the persisted setting — and installs it on the server. Missing/invalid
// leaves s.license nil (no EE): fail-closed, never boot-fatal. Called at boot
// and after the key is changed via the admin API.
func (s *Server) loadLicense(ctx context.Context) {
	_ = ctx
	token := strings.TrimSpace(os.Getenv("TELA_LICENSE_KEY"))
	if token == "" {
		token, _ = s.settings.Get(licenseTokenSettingKey)
		token = strings.TrimSpace(token)
	}
	if token == "" {
		s.license.Store(nil)
		return
	}
	lic, err := ee.Verify(token)
	if err != nil {
		slog.Warn("license: ignoring invalid Enterprise key", "err", err)
		s.license.Store(nil)
		return
	}
	s.license.Store(lic)
	slog.Info("license: Enterprise key active", "customer", lic.Customer, "tier", lic.Tier)
}

// warnSelfHostSSO logs a prominent boot notice when a self-host instance has SSO
// configured but isn't entitled to it (post-editions, SSO is Enterprise) — so an
// operator who upgraded sees why SSO stopped working, with the recovery path.
// Managed cloud is exempt (plan flags grant there).
func (s *Server) warnSelfHostSSO(ctx context.Context) {
	if s.managedCloud {
		return
	}
	if lic := s.license.Load(); lic != nil && lic.Grants("sso") {
		return
	}
	var n int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM org_sso`).Scan(&n); err == nil && n > 0 {
		slog.Warn("SSO is configured but this instance isn't entitled to it — SSO is now an Enterprise feature requiring a license key (Settings → License). Until a key is installed, affected users can sign in via password reset.",
			"orgs_with_sso", n)
	}
}

// licenseStatus returns the active license summary, or a zero (invalid) status.
func (s *Server) licenseStatus() ee.Status {
	return s.license.Load().Status() // Load()==nil → nil-receiver Status() → zero
}

// selfHostSeatUsage counts active users on this instance — the number compared
// against the license's Seats for the SOFT seat check. Never used to block.
func (s *Server) selfHostSeatUsage(ctx context.Context) int {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE is_active = 1`).Scan(&n)
	return n
}

// warnSelfHostSeats logs a prominent boot notice when a self-host instance has
// MORE active users than its Enterprise license covers. This is the honor-system
// seat model: features stay on (offline keys can't phone home to count seats), so
// enforcement is a visible nudge to true-up, never a lockout. Managed cloud is
// exempt (seats are billed by Polar there). No-op without a seated license.
func (s *Server) warnSelfHostSeats(ctx context.Context) {
	if s.managedCloud {
		return
	}
	lic := s.license.Load()
	if lic == nil || lic.Seats <= 0 {
		return
	}
	if used := s.selfHostSeatUsage(ctx); used > lic.Seats {
		slog.Warn("this instance has more active users than the Enterprise license covers — Enterprise features stay on, but please update your subscription's seat count to stay licensed.",
			"seats_used", used, "seats_licensed", lic.Seats)
	}
}

// envLicensed reports whether the key is pinned via env (then the admin API is
// read-only — the env value always wins on the next boot).
func envLicensed() bool { return strings.TrimSpace(os.Getenv("TELA_LICENSE_KEY")) != "" }

// GetLicense returns the active license status (never the raw token). Instance-admin.
func (s *Server) GetLicense(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireInstanceAdmin(w, r); !ok {
		return
	}
	resp := map[string]any{
		"license":    s.licenseStatus(),
		"env_locked": envLicensed(),
	}
	if lic := s.license.Load(); lic != nil && !s.managedCloud {
		// Soft seat check: surface used-vs-licensed so the admin tab can nudge a
		// true-up when the instance is over.
		if lic.Seats > 0 {
			resp["seat_usage"] = map[string]int{"used": s.selfHostSeatUsage(r.Context()), "licensed": lic.Seats}
		}
		// A cloud-issued key (has a refresh handle) auto-renews from the cloud, so
		// the admin needn't re-paste on renewal — as long as this box can reach it.
		resp["refreshable"] = lic.LicenseID != "" && licenseRefreshURL() != ""
	}
	writeJSON(w, http.StatusOK, resp)
}

// PutLicense installs (or replaces) the license key. Instance-admin. The token
// is verified before it's persisted, so a bad key is rejected up front rather
// than silently disabling EE on the next boot.
func (s *Server) PutLicense(w http.ResponseWriter, r *http.Request) {
	u, ok := requireInstanceAdmin(w, r)
	if !ok {
		return
	}
	if envLicensed() {
		writeError(w, http.StatusConflict, "env_locked", "the license key is set via TELA_LICENSE_KEY and can't be changed here")
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	token := strings.TrimSpace(req.Token)
	if token == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "token is required")
		return
	}
	lic, err := ee.Verify(token)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_license", err.Error())
		return
	}
	if err := s.settings.Set(r.Context(), licenseTokenSettingKey, token, &u.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "save license failed")
		return
	}
	s.loadLicense(r.Context())
	s.audit(r.Context(), r, "license.set", "instance", 0, lic.Tier)
	writeJSON(w, http.StatusOK, map[string]any{"license": lic.Status(), "env_locked": false})
}

// DeleteLicense removes the installed key (downgrades the instance to Community).
// Instance-admin. No-op-safe; env-pinned keys can't be removed here.
func (s *Server) DeleteLicense(w http.ResponseWriter, r *http.Request) {
	u, ok := requireInstanceAdmin(w, r)
	if !ok {
		return
	}
	if envLicensed() {
		writeError(w, http.StatusConflict, "env_locked", "the license key is set via TELA_LICENSE_KEY and can't be changed here")
		return
	}
	if err := s.settings.Set(r.Context(), licenseTokenSettingKey, "", &u.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "clear license failed")
		return
	}
	s.loadLicense(r.Context())
	s.audit(r.Context(), r, "license.clear", "instance", 0, "")
	w.WriteHeader(http.StatusNoContent)
}
