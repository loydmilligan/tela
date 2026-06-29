package api

import (
	"net/http"
	"strconv"
)

// og_root.go — crawler OG card for the bare apex of an org's white-label custom
// domain (e.g. a bot pasting https://tela.ngss.io/ into Slack). Without it the
// root unfurls as the empty "tela" SPA shell; with it the org gets a branded card
// (logo / accent / name) like every other surface on its domain.
//
// Scoped to custom domains by Caddy: ONLY the org-custom-domain block routes "/"
// (bot-gated) and "/og.png" (all UAs) here. The canonical apex keeps its richer
// marketing-landing OG — its block never sends the root to the backend. Branding
// follows the request host via the OrgContext stamped by hostOrgMiddleware, so a
// request with no org (dev / unknown host) falls back to the generic tela card.
// On auth.IsPublicPath; served to cookieless crawlers, so it self-authenticates.

const ogRootSubtitle = "Team knowledge base"

// HandleRootOG emits the OG envelope for the white-label apex.
func (s *Server) HandleRootOG(w http.ResponseWriter, r *http.Request) {
	siteName := s.ogSiteName(r, 0)
	origin := s.originFor(r)
	if origin == "" {
		origin = canonicalBaseURL()
	}
	writeOGDoc(w, ogDoc{
		Title:        runeTruncate(siteName, 110),
		Description:  runeTruncate(siteName+" — "+ogRootSubtitle+".", 200),
		CanonicalURL: origin + "/",
		ImageURL:     origin + "/og.png",
		OGType:       "website",
		SiteName:     siteName,
	})
}

// HandleRootOGImage renders the apex card: the org name under its brand, with a
// generic "knowledge base" subtitle. Served to all UAs (link-preview fetchers
// carry arbitrary UAs). Org-branded via the request's custom-domain org.
func (s *Server) HandleRootOGImage(w http.ResponseWriter, r *http.Request) {
	png, err := renderOGCardOpts(ogCardOpts{
		title:    s.ogSiteName(r, 0),
		subtitle: ogRootSubtitle,
		brand:    s.resolveOGBrand(r, 0),
	})
	if err != nil {
		writeInternalHTML(w)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Content-Length", strconv.Itoa(len(png)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(png)
}
