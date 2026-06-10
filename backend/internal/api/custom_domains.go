package api

import (
	"cmp"
	"context"
	"net/http"

	"github.com/zcag/tela/backend/internal/auth"
)

// custom_domains.go is the request-time half of org custom domains: resolving
// the inbound host to an org, scoping the request to it, and answering "which
// origin does a browser-facing URL live on?". The management half (CRUD, DNS
// verification, the TLS ask-endpoint) lives in org_hostnames.go.

// hostOrgMiddleware resolves the request host to an org and, when it is an
// active custom domain, stamps an OrgContext onto the request. It runs BEFORE
// auth.Middleware so the context is available to the login screen and to
// session creation/validation (the session↔org binding). On the canonical host
// (or any unknown/pending host) it is a no-op — the app behaves as it always
// has. The lookup is a single indexed PK hit; the backend only sees API/ws/
// share/dav traffic (static assets are served by the proxy), so no cache is
// warranted.
func (s *Server) hostOrgMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := hostnameOnly(r.Host)
		if host != "" {
			if orgID, ok := s.orgByHost(r.Context(), host); ok {
				r = r.WithContext(auth.WithOrgContext(r.Context(), auth.OrgContext{OrgID: orgID, Host: host}))
			}
		}
		next.ServeHTTP(w, r)
	})
}

// orgByHost returns the org that owns an ACTIVE custom hostname. Pending or
// unknown hosts (and the canonical host) yield (0, false).
func (s *Server) orgByHost(ctx context.Context, host string) (int64, bool) {
	var orgID int64
	err := s.DB.QueryRowContext(ctx,
		`SELECT org_id FROM org_hostnames WHERE hostname = $1 AND status = 'active'`, host).Scan(&orgID)
	if err != nil {
		return 0, false
	}
	return orgID, true
}

// originFor returns the request's effective browser-facing origin for the
// surfaces that follow a custom domain (auth emails, SSO callback): the
// verified custom domain when the request arrived on one, else the canonical
// origin. The org context was resolved once by hostOrgMiddleware, so this is
// free.
func (s *Server) originFor(r *http.Request) string {
	if oc, ok := auth.OrgContextFromContext(r.Context()); ok {
		return requestScheme(r) + "://" + oc.Host
	}
	return canonicalBaseURL()
}

// linkOrigin is originFor with the dev fallback applied — for surfaces that must
// emit a complete URL even with no TELA_PUBLIC_BASE_URL set (auth emails, SSO
// callback).
func (s *Server) linkOrigin(r *http.Request) string {
	return cmp.Or(s.originFor(r), devBaseURL)
}

// shareOrigin returns the origin a share URL for spaceID should use: the
// space's owning org's active custom domain when it has one, else the canonical
// origin. Derived from the space (not the request host) so a copied share link
// is branded with the org's domain regardless of which host created it. Returns
// "" only when no custom domain applies AND TELA_PUBLIC_BASE_URL is unset
// (path-only, dev) — callers that need a complete URL wrap with cmp.Or.
func (s *Server) shareOrigin(ctx context.Context, spaceID int64) string {
	if host, ok := s.spaceOrgPrimaryHost(ctx, spaceID); ok {
		return "https://" + host
	}
	return canonicalBaseURL()
}

// shareOriginForPage is shareOrigin keyed by page id (the share OG handlers
// carry a page, not a space). Same fallback semantics.
func (s *Server) shareOriginForPage(ctx context.Context, pageID int64) string {
	var host string
	err := s.DB.QueryRowContext(ctx, `
		SELECT h.hostname
		  FROM pages p
		  JOIN spaces sp ON sp.id = p.space_id
		  JOIN org_hostnames h ON h.org_id = sp.org_id AND h.status = 'active'
		 WHERE p.id = $1
		 ORDER BY h.created_at ASC, h.hostname ASC
		 LIMIT 1`, pageID).Scan(&host)
	if err != nil {
		return canonicalBaseURL()
	}
	return "https://" + host
}

// ogOriginForPage picks the origin for a crawler OG card on a page: the request's
// OWN custom-domain host when the bot fetched the card there (the domain actually
// in the shared URL), else the page's owning-org custom domain, else canonical.
// Request host first so an in-app page deep link copied from a white-label domain
// unfurls as THAT domain even when the space carries no org_id (a member can view
// any space they belong to on the org host, regardless of the space's own org).
func (s *Server) ogOriginForPage(r *http.Request, pageID int64) string {
	if oc, ok := auth.OrgContextFromContext(r.Context()); ok {
		return requestScheme(r) + "://" + oc.Host
	}
	return s.shareOriginForPage(r.Context(), pageID)
}

// spaceOrgPrimaryHost returns the active custom hostname of the org that owns
// spaceID, if any. A space with no org (personal/legacy) or an org with no
// active hostname yields ("", false). When an org has several active hostnames
// the earliest-created wins as the canonical white-label host.
func (s *Server) spaceOrgPrimaryHost(ctx context.Context, spaceID int64) (string, bool) {
	var host string
	err := s.DB.QueryRowContext(ctx, `
		SELECT h.hostname
		  FROM spaces s
		  JOIN org_hostnames h ON h.org_id = s.org_id AND h.status = 'active'
		 WHERE s.id = $1
		 ORDER BY h.created_at ASC, h.hostname ASC
		 LIMIT 1`, spaceID).Scan(&host)
	if err != nil {
		return "", false
	}
	return host, true
}
