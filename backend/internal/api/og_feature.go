package api

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// og_feature.go gives authed app routes that have no page/space behind them — but
// a stable, public *purpose* — a real link-preview card. /ask is the first: a
// crawler pasting tela.<domain>/ask into Slack / iMessage / etc. gets an
// "Ask your docs" card instead of the empty SPA shell. Humans still get the SPA
// (Caddy bot-gates these paths to the backend, mirroring /p and /public; the
// og.png is served to all UAs like /p/{id}/og.png).
//
// The copy is the feature's PUBLIC purpose — identical for everyone, leaking
// nothing — so the card is honest even though clicking it lands on a login wall.
// Brand (logo / accent / name) and origin follow the request's custom-domain org,
// reusing the page-card machinery, so /ask on a white-label domain is branded.

type featureCard struct {
	title    string // card heading + og:title base
	subtitle string // rendered under the title on the image
	desc     string // og:description / twitter:description
}

// featureCards maps an app path to its crawler card. To expose a new one: add an
// entry here, a route pair in router.go (GET <path> + GET <path>/og.png), the
// path to auth.IsPublicPath, and an @<x>_bots gate in deploy/proxy/sites.caddy.
var featureCards = map[string]featureCard{
	"/ask": {
		title:    "Ask your docs",
		subtitle: "AI answers grounded in your team's wiki",
		desc:     "Ask a question and get an answer grounded in your team's wiki, with citations to the source pages.",
	},
	"/graph": {
		title:    "Knowledge graph",
		subtitle: "How your pages connect",
		desc:     "Explore the wiki as a graph — every page and the links between them.",
	},
	"/discover": {
		title:    "Discover",
		subtitle: "Public spaces & people",
		desc:     "Browse the public spaces and authors published here.",
	},
}

// HandleFeatureOG emits the OG HTML envelope for a feature route (bot-gated by
// Caddy; humans hit the SPA). Self-authenticating — the path is on IsPublicPath
// and the copy is public.
func (s *Server) HandleFeatureOG(w http.ResponseWriter, r *http.Request) {
	fc, ok := featureCards[r.URL.Path]
	if !ok {
		writeNotFoundHTML(w)
		return
	}
	origin := s.originFor(r)
	if origin == "" {
		origin = canonicalBaseURL()
	}
	siteName := s.ogSiteName(r, 0)
	// og:url is the shared link itself (query preserved) so the unfurl clicks
	// through to the same pre-filled/auto-running Ask.
	pageURL := origin + r.URL.Path
	if r.URL.RawQuery != "" {
		pageURL += "?" + r.URL.RawQuery
	}
	imageURL := origin + r.URL.Path + "/og.png"

	// A shared ask link carries ?q=<question> — feature the question itself (the
	// compelling bit in a paste), with the answer pointedly NOT in the card. No q
	// → the generic "Ask your docs" feature card.
	title, desc := fc.title, fc.desc
	if q := featureQuestion(r); q != "" {
		// "Ask:" prefix frames the unfurl text as a prompt to go ask, not a page.
		title = "Ask: " + q
		desc = "Open to ask " + siteName + " your docs — the answer, with sources. (Beats pinging a human.)"
		imageURL += "?q=" + url.QueryEscape(q)
	}
	ogTitle := title
	if siteName != "" && siteName != "tela" {
		ogTitle = title + " · " + siteName
	}
	ogTitle = runeTruncate(ogTitle, 110)
	desc = runeTruncate(desc, 200)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>%s</title>
  <meta name="description" content="%s">
  <meta property="og:site_name" content="%s">
  <meta property="og:title" content="%s">
  <meta property="og:description" content="%s">
  <meta property="og:image" content="%s">
  <meta property="og:image:width" content="1200">
  <meta property="og:image:height" content="630">
  <meta property="og:image:alt" content="%s">
  <meta property="og:url" content="%s">
  <meta property="og:type" content="website">
  <meta name="twitter:card" content="summary_large_image">
  <meta name="twitter:title" content="%s">
  <meta name="twitter:description" content="%s">
  <meta name="twitter:image" content="%s">
</head>
<body></body>
</html>`,
		html.EscapeString(ogTitle),
		html.EscapeString(desc),
		html.EscapeString(siteName),
		html.EscapeString(ogTitle),
		html.EscapeString(desc),
		html.EscapeString(imageURL),
		html.EscapeString(ogTitle),
		html.EscapeString(pageURL),
		html.EscapeString(ogTitle),
		html.EscapeString(desc),
		html.EscapeString(imageURL),
	)
}

// featureQuestion reads + sanitizes a shared ask link's ?q= for use in the card:
// trimmed, whitespace collapsed to single spaces, capped so a pathological query
// can't blow up the title. Empty when absent.
func featureQuestion(r *http.Request) string {
	q := strings.Join(strings.Fields(r.URL.Query().Get("q")), " ")
	return runeTruncate(q, 300)
}

// HandleFeatureOGImage renders the 1200×630 card for a feature route's og.png.
// Served to all UAs (link-preview fetchers carry arbitrary UAs). Path is
// "<feature>/og.png"; the leading "<feature>" selects the card. Org-branded via
// the request's custom-domain org.
func (s *Server) HandleFeatureOGImage(w http.ResponseWriter, r *http.Request) {
	base := strings.TrimSuffix(r.URL.Path, "/og.png")
	fc, ok := featureCards[base]
	if !ok {
		writeNotFoundHTML(w)
		return
	}
	brand := s.resolveOGBrand(r, 0)
	// With a shared ?q=, the question is the hero under an "ASK YOUR DOCS" eyebrow
	// (so it reads as a prompt to go ask, not an article) + a nudge CTA, capped to
	// two lines so a long question is cut off rather than sprawling. No q → the
	// generic feature card.
	opts := ogCardOpts{title: fc.title, subtitle: fc.subtitle, brand: brand}
	if q := featureQuestion(r); q != "" {
		opts = ogCardOpts{
			kicker:        fc.title, // "Ask your docs"
			title:         q,
			subtitle:      "Open to see the answer →",
			maxTitleLines: 2,
			brand:         brand,
		}
	}
	png, err := renderOGCardOpts(opts)
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
