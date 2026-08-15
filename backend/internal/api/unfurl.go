package api

import (
	"context"
	"errors"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// URL unfurl: GET /api/unfurl?url=… returns {url, title} so the editor can
// turn a pasted link into `[title](url)` instead of a bare URL. Session-authed
// (an authenticated convenience that makes an outbound request — never public,
// so it can't be abused as an open SSRF proxy).
//
// SSRF defense (this is fetching ARBITRARY user-supplied URLs, so it can't use
// a host allowlist):
//   - http/https only.
//   - A connect-time Control hook rejects any dial to a private / loopback /
//     link-local / unspecified IP. Checking at dial time (not after a separate
//     LookupIP) closes the DNS-rebinding window.
//   - No redirects followed (a 3xx just yields no title).
//   - Whole-request timeout + a bounded body read (we only need <title>).

const (
	unfurlTimeout      = 6 * time.Second
	unfurlDialTimeout  = 4 * time.Second
	unfurlMaxBodyBytes = 512 << 10 // 512 KiB — plenty to reach <title> in <head>
)

var errBlockedAddr = errors.New("blocked address")

// titleRE pulls the first <title>…</title> (case-insensitive, across newlines).
var titleRE = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

// unfurlWhitespaceRE collapses runs of whitespace in the extracted title.
var unfurlWhitespaceRE = regexp.MustCompile(`\s+`)

// metaTagRE matches a whole <meta …> tag; attributes are picked out of the
// matched tag with metaAttrRE (attribute order varies wildly in the wild —
// `property` before `content` on some sites, after on others).
var metaTagRE = regexp.MustCompile(`(?is)<meta\b[^>]*>`)
var metaAttrRE = regexp.MustCompile(`(?is)\b(property|name|content)\s*=\s*("([^"]*)"|'([^']*)')`)

// iconLinkRE matches <link …> tags; rel/href picked out with linkAttrRE.
var iconLinkRE = regexp.MustCompile(`(?is)<link\b[^>]*>`)
var linkAttrRE = regexp.MustCompile(`(?is)\b(rel|href)\s*=\s*("([^"]*)"|'([^']*)')`)

// isBlockedIP reports whether dialing this IP could reach internal/link-local
// infrastructure and must be refused.
func isBlockedIP(ip net.IP) bool {
	return ip == nil ||
		ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified()
}

// unfurlDialControl runs after DNS resolution, with the concrete IP:port about
// to be dialed — reject internal targets here to defeat DNS rebinding.
func unfurlDialControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return errBlockedAddr
	}
	ip := net.ParseIP(host)
	if isBlockedIP(ip) {
		return errBlockedAddr
	}
	return nil
}

func newUnfurlClient() *http.Client {
	dialer := &net.Dialer{Timeout: unfurlDialTimeout, Control: unfurlDialControl}
	return &http.Client{
		Timeout: unfurlTimeout,
		// Don't follow redirects — return the 3xx as-is (no title extracted).
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			Proxy:                 nil,
			DisableKeepAlives:     true,
			TLSHandshakeTimeout:   unfurlDialTimeout,
			ResponseHeaderTimeout: unfurlTimeout,
		},
	}
}

// unfurlResponse is the metadata bag the bookmark card renders. Title keeps
// its original meaning (the editor's paste→titled-link path reads only it);
// the rest are additive and empty-string when absent.
type unfurlResponse struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	SiteName    string `json:"site_name"`
	Favicon     string `json:"favicon"`
	Image       string `json:"image"`
}

func extractTitle(body []byte) string {
	m := titleRE.FindSubmatch(body)
	if m == nil {
		return ""
	}
	return cleanMetaText(string(m[1]), 300)
}

func cleanMetaText(s string, max int) string {
	t := html.UnescapeString(s)
	t = unfurlWhitespaceRE.ReplaceAllString(strings.TrimSpace(t), " ")
	if len(t) > max {
		t = t[:max]
	}
	return t
}

// metaContent scans every <meta> tag for the first one whose property/name
// attribute equals key (og:* uses property; twitter:*/description use name).
func metaContent(body []byte, key string) string {
	for _, tag := range metaTagRE.FindAll(body, -1) {
		var prop, content string
		for _, m := range metaAttrRE.FindAllSubmatch(tag, -1) {
			val := string(m[3]) + string(m[4]) // whichever quote style matched
			switch strings.ToLower(string(m[1])) {
			case "property", "name":
				if prop == "" {
					prop = strings.ToLower(strings.TrimSpace(val))
				}
			case "content":
				content = val
			}
		}
		if prop == key && content != "" {
			return content
		}
	}
	return ""
}

// faviconHref returns the first <link rel="…icon…"> href, or "" — the caller
// resolves it against the page URL and falls back to /favicon.ico.
func faviconHref(body []byte) string {
	for _, tag := range iconLinkRE.FindAll(body, -1) {
		var rel, href string
		for _, m := range linkAttrRE.FindAllSubmatch(tag, -1) {
			val := string(m[3]) + string(m[4])
			switch strings.ToLower(string(m[1])) {
			case "rel":
				rel = strings.ToLower(strings.TrimSpace(val))
			case "href":
				href = val
			}
		}
		// "icon", "shortcut icon", "apple-touch-icon" all qualify; exclude
		// "mask-icon" (monochrome SVG) only when a better one exists — keep
		// simple: any rel containing "icon" works.
		if strings.Contains(rel, "icon") && href != "" {
			return href
		}
	}
	return ""
}

// resolveMetaURL absolutes a possibly-relative meta URL against the fetched
// page and keeps only http(s) results (a javascript:/data: href never leaves).
func resolveMetaURL(base *url.URL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	abs := base.ResolveReference(ref)
	if abs.Scheme != "http" && abs.Scheme != "https" {
		return ""
	}
	return abs.String()
}

// extractUnfurlMeta builds the full response from a fetched HTML body.
func extractUnfurlMeta(pageURL *url.URL, body []byte) unfurlResponse {
	res := unfurlResponse{URL: pageURL.String()}
	res.Title = cleanMetaText(metaContent(body, "og:title"), 300)
	if res.Title == "" {
		res.Title = extractTitle(body)
	}
	res.Description = cleanMetaText(metaContent(body, "og:description"), 500)
	if res.Description == "" {
		res.Description = cleanMetaText(metaContent(body, "description"), 500)
	}
	res.SiteName = cleanMetaText(metaContent(body, "og:site_name"), 100)
	res.Image = resolveMetaURL(pageURL, metaContent(body, "og:image"))
	if icon := faviconHref(body); icon != "" {
		res.Favicon = resolveMetaURL(pageURL, icon)
	}
	if res.Favicon == "" {
		res.Favicon = resolveMetaURL(pageURL, "/favicon.ico")
	}
	return res
}

// unfurlCache is a small in-process TTL cache so a page full of bookmark
// cards doesn't re-fetch the same origins on every render. Bounded; on
// overflow the whole map is dropped (simplest eviction that can't leak).
type unfurlCacheEntry struct {
	res     unfurlResponse
	expires time.Time
}

const (
	unfurlCacheTTL = 24 * time.Hour
	unfurlCacheMax = 2048
)

var (
	unfurlCacheMu sync.Mutex
	unfurlCache   = map[string]unfurlCacheEntry{}
)

func unfurlCacheGet(key string) (unfurlResponse, bool) {
	unfurlCacheMu.Lock()
	defer unfurlCacheMu.Unlock()
	e, ok := unfurlCache[key]
	if !ok || time.Now().After(e.expires) {
		return unfurlResponse{}, false
	}
	return e.res, true
}

func unfurlCachePut(key string, res unfurlResponse) {
	unfurlCacheMu.Lock()
	defer unfurlCacheMu.Unlock()
	if len(unfurlCache) >= unfurlCacheMax {
		unfurlCache = map[string]unfurlCacheEntry{}
	}
	unfurlCache[key] = unfurlCacheEntry{res: res, expires: time.Now().Add(unfurlCacheTTL)}
}

// Unfurl handles GET /api/unfurl?url=…
func (s *Server) Unfurl(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	if !s.allowRateLimit(w, r, "unfurl", s.unfurlLimiter) {
		return
	}
	raw := strings.TrimSpace(r.URL.Query().Get("url"))
	if raw == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "url query param required")
		return
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "url must be a valid http(s) URL")
		return
	}

	if cached, ok := unfurlCacheGet(parsed.String()); ok {
		writeJSON(w, http.StatusOK, cached)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), unfurlTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not build request")
		return
	}
	req.Header.Set("User-Agent", "tela-unfurl/1.0 (+https://telawiki.com)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := newUnfurlClient().Do(req)
	if err != nil {
		// Blocked address, timeout, DNS failure, connection refused — all
		// collapse to "couldn't fetch" so we don't leak network topology.
		// NOT cached: transient failures shouldn't stick for a day.
		writeJSON(w, http.StatusOK, unfurlResponse{URL: raw})
		return
	}
	defer resp.Body.Close()

	out := unfurlResponse{URL: raw}
	if resp.StatusCode == http.StatusOK &&
		strings.Contains(resp.Header.Get("Content-Type"), "html") {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, unfurlMaxBodyBytes))
		out = extractUnfurlMeta(parsed, body)
		out.URL = raw
		unfurlCachePut(parsed.String(), out)
	}
	writeJSON(w, http.StatusOK, out)
}
