package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
)

// og_space.go — crawler OG card for a space overview (/spaces/{id}), the link you
// copy to share a whole space with a teammate. Like the /p permalink it renders
// the space NAME only (never its contents), branded by the space's owning org (or
// the request's custom-domain org). Bot-gated by Caddy (humans get the SPA); on
// auth.IsPublicPath via isSpaceOGPath.

// loadSpaceForOG reads the space's name/description/owning-org by id, writing an
// HTML 404/500 and returning ok=false on miss/error.
func (s *Server) loadSpaceForOG(w http.ResponseWriter, r *http.Request) (id, orgID int64, name, desc string, ok bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeNotFoundHTML(w)
		return
	}
	err = s.DB.QueryRowContext(r.Context(),
		`SELECT name, COALESCE(description, ''), COALESCE(org_id, 0) FROM spaces WHERE id = $1`, id).
		Scan(&name, &desc, &orgID)
	if errors.Is(err, sql.ErrNoRows) {
		writeNotFoundHTML(w)
		return
	}
	if err != nil {
		writeInternalHTML(w)
		return
	}
	ok = true
	return
}

// HandleSpaceOG emits the OG envelope for a space overview.
func (s *Server) HandleSpaceOG(w http.ResponseWriter, r *http.Request) {
	id, orgID, name, desc, ok := s.loadSpaceForOG(w, r)
	if !ok {
		return
	}
	origin := s.originFor(r)
	if origin == "" {
		origin = canonicalBaseURL()
	}
	siteName := s.ogSiteName(r, orgID)
	title := name
	if siteName != "" && siteName != "tela" {
		title = name + " · " + siteName
	}
	if desc == "" {
		desc = "A space on " + siteName
	}
	writeOGDoc(w, ogDoc{
		Title:        runeTruncate(title, 110),
		Description:  runeTruncate(desc, 200),
		CanonicalURL: fmt.Sprintf("%s/spaces/%d", origin, id),
		ImageURL:     fmt.Sprintf("%s/spaces/%d/og.png", origin, id),
		OGType:       "website",
		SiteName:     siteName,
	})
}

// HandleSpaceOGImage renders the space card: a "SPACE" eyebrow + the name (+ the
// description as subtitle when set), branded by the owning/host org.
func (s *Server) HandleSpaceOGImage(w http.ResponseWriter, r *http.Request) {
	_, orgID, name, desc, ok := s.loadSpaceForOG(w, r)
	if !ok {
		return
	}
	png, err := renderOGCardOpts(ogCardOpts{
		kicker:        "Space",
		title:         name,
		subtitle:      desc,
		maxTitleLines: 2,
		brand:         s.resolveOGBrand(r, orgID),
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
