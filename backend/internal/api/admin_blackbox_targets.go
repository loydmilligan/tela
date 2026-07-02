package api

import (
	"net/http"
)

// admin_blackbox_targets.go — GET /api/admin/blackbox-targets. Instance-admin
// gated (the monitoring stack authenticates with the same admin PAT the /metrics
// scrape uses). Returns the active custom domains in Prometheus http_sd format so
// the blackbox-http job discovers them dynamically: a domain starts being
// uptime- and cert-alerted the moment it goes active, and stops when it's
// removed — no per-domain monitoring config anywhere. Each target is the domain's
// /api/health URL (a real backend liveness signal, not just the SPA/edge).
//
// http_sd shape: [{"targets":["https://host/api/health"],"labels":{...}}]. The
// blackbox-http relabel_configs turn __address__ into the probe __param_target.

type sdTargetGroup struct {
	Targets []string          `json:"targets"`
	Labels  map[string]string `json:"labels"`
}

func (s *Server) BlackboxTargets(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireInstanceAdmin(w, r); !ok {
		return
	}
	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT hostname FROM org_hostnames WHERE status = 'active' ORDER BY hostname`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list custom domains failed")
		return
	}
	defer rows.Close()

	groups := []sdTargetGroup{}
	for rows.Next() {
		var host string
		if err := rows.Scan(&host); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan custom domain failed")
			return
		}
		groups = append(groups, sdTargetGroup{
			Targets: []string{"https://" + host + "/api/health"},
			Labels: map[string]string{
				"probe_kind":    "tela_custom_domain",
				"custom_domain": host,
			},
		})
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate custom domains failed")
		return
	}
	writeJSON(w, http.StatusOK, groups)
}
