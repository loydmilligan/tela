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
	"syscall"
	"time"
)

// URL unfurl: GET /api/unfurl?url=… returns {url, title} so the editor can
// turn a pasted link into `[title](url)` instead of a bare URL. Session-authed
// (an authenticated convenience that makes an outbound request — never public,
// so it can't be abused as an open SSRF proxy).
//
// SSRF defense (this is fetching ARBITRARY user-supplied URLs, so unlike the
// mira import it can't use a host allowlist):
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

type unfurlResponse struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

func extractTitle(body []byte) string {
	m := titleRE.FindSubmatch(body)
	if m == nil {
		return ""
	}
	t := html.UnescapeString(string(m[1]))
	t = unfurlWhitespaceRE.ReplaceAllString(strings.TrimSpace(t), " ")
	if len(t) > 300 {
		t = t[:300]
	}
	return t
}

// Unfurl handles GET /api/unfurl?url=…
func (s *Server) Unfurl(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
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

	ctx, cancel := context.WithTimeout(r.Context(), unfurlTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not build request")
		return
	}
	req.Header.Set("User-Agent", "tela-unfurl/1.0 (+https://tela.cagdas.io)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := newUnfurlClient().Do(req)
	if err != nil {
		// Blocked address, timeout, DNS failure, connection refused — all
		// collapse to "couldn't fetch" so we don't leak network topology.
		writeJSON(w, http.StatusOK, unfurlResponse{URL: raw, Title: ""})
		return
	}
	defer resp.Body.Close()

	title := ""
	if resp.StatusCode == http.StatusOK &&
		strings.Contains(resp.Header.Get("Content-Type"), "html") {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, unfurlMaxBodyBytes))
		title = extractTitle(body)
	}
	writeJSON(w, http.StatusOK, unfurlResponse{URL: raw, Title: title})
}
