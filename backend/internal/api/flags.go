package api

import (
	"net/http"
	"os"
	"strings"
)

// Experimental feature flags. The point: ship new, still-baking features DARK so
// they never change behaviour for existing users, and let an instance admin flip
// them on per-instance (and off again) without a deploy. Built on the existing
// instance_settings store — same env → setting → default precedence as the rest
// of instance config, so an operator can also pin a flag from the environment.
//
//   precedence: env TELA_FEATURE_<NAME>  →  instance_settings "feature.<name>"  →  OFF
//
// Toggle at runtime via the admin instance-settings API (PUT a "feature.<name>"
// key). Keys are non-secret, so they show up in GET /api/instance-settings.

const flagPrefix = "feature."

// featureFlag reports whether an experimental feature is enabled for this
// instance. Defaults OFF — an unset flag is disabled.
func (s *Server) featureFlag(name string) bool {
	if v, ok := os.LookupEnv("TELA_FEATURE_" + strings.ToUpper(name)); ok {
		return truthy(v)
	}
	if v, ok := s.settings.Get(flagPrefix + name); ok {
		return truthy(v)
	}
	return false
}

// requireFeature gates an HTTP handler on a flag, writing a 404 (the feature is
// invisible until enabled) when off. Returns true to proceed.
func (s *Server) requireFeature(w http.ResponseWriter, name string) bool {
	if s.featureFlag(name) {
		return true
	}
	writeError(w, http.StatusNotFound, "feature_disabled", "this feature is not enabled on this instance")
	return false
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "on", "yes", "enabled":
		return true
	}
	return false
}

// featureKnowledge is the flag for the knowledge-intelligence surface (related
// pages, link suggestions, overlap detection, knowledge gaps). One umbrella flag
// for the experimental suite; split into per-feature flags if/when any graduates.
const featureKnowledge = "knowledge"
