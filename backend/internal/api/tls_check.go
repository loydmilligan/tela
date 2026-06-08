package api

import (
	"net/http"
)

// TLSCheck is Caddy's on-demand-TLS "ask" endpoint:
//
//	GET /api/internal/tls-check?domain=tela.ngss.io
//
// Caddy calls it before provisioning a Let's Encrypt cert for an unknown SNI
// host and issues only on a 200. We return 200 iff the host is an ACTIVE
// org_hostname — so a stranger pointing DNS at the box can't force unbounded
// cert issuance for hostnames we don't recognise.
//
// It's reached over the internal docker network by Caddy with no session, so
// it's on IsPublicPath (the /api/internal/ prefix). It discloses nothing beyond
// "is this host active" (200 vs 404) and the public site blocks 404 the
// /api/internal/ prefix from the WAN, so it isn't externally reachable.
func (s *Server) TLSCheck(w http.ResponseWriter, r *http.Request) {
	host := hostnameOnly(r.URL.Query().Get("domain"))
	if host == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if _, ok := s.orgByHost(r.Context(), host); !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
}
