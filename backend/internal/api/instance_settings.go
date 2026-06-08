package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/zcag/tela/backend/internal/settings"
)

// Instance settings admin surface — operator-editable runtime config backed by
// the instance_settings table (internal/settings). Instance-admin only.
//
// Secret-prefixed keys (api-key/share secrets, cloud token) are never returned
// and cannot be written through this API — they're managed by their own
// resolve paths. This keeps the generic KV surface safe to expose.

// GetInstanceSettings returns all non-secret instance settings.
func (s *Server) GetInstanceSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireInstanceAdmin(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": s.settings.All()})
}

type patchSettingsRequest struct {
	Settings map[string]string `json:"settings"`
}

// PatchInstanceSettings upserts the provided key/value pairs. Rejects any
// attempt to write a secret-prefixed key. Each change is audited and stamped
// with the acting admin's id (updated_by).
func (s *Server) PatchInstanceSettings(w http.ResponseWriter, r *http.Request) {
	u, ok := requireInstanceAdmin(w, r)
	if !ok {
		return
	}
	var req patchSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	if len(req.Settings) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "settings object is required")
		return
	}
	for k := range req.Settings {
		if k == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "empty setting key")
			return
		}
		if strings.HasPrefix(k, settings.SecretPrefix) {
			writeError(w, http.StatusForbidden, "forbidden", "secret keys cannot be set via this API")
			return
		}
	}
	ctx := r.Context()
	for k, v := range req.Settings {
		if err := s.settings.Set(ctx, k, v, &u.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "set setting failed")
			return
		}
		s.audit(ctx, r, "instance.set_setting", "setting", 0, k)
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": s.settings.All()})
}
