package api

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestPublicShareLink_BotGate exercises the M15.5 /share/{token} +
// /share/{token}/p/{page_id} routes end-to-end. The handlers mirror the M11.0
// /p/{id} bot-allowlist pattern but pick the OG URL off the share token + the
// password-protected envelope flips to a generic locked card to avoid
// title/image leaks.
func TestPublicShareLink_BotGate(t *testing.T) {
	ts, d := newWiredServer(t)
	admin := seedUser(t, d, "admin", "adminpw12", true)
	space := seedSpace(t, d, "Engineering", "engineering", admin)

	rootID := seedPageInSpace(t, d, space, nil, "Root Page", "root body")
	childID := seedPageInSpace(t, d, space, &rootID, "Child Page", "child body")
	// A page in another space — used to confirm the subtree walk doesn't leak
	// across spaces even if a token's IncludeDescendants is true.
	otherSpace := seedSpace(t, d, "Other", "other", admin)
	outsideID := seedPageInSpace(t, d, otherSpace, nil, "Outside", "outside body")

	// Insert tokens directly so we know exactly what state we're testing.
	// All times use the SQLite datetime format the rest of the share code
	// relies on for wire-format consistency.
	insertShare := func(token string, pageID int64, includeDesc bool, passwordHash *string, expiresAt, revokedAt *string) {
		t.Helper()
		var inc int
		if includeDesc {
			inc = 1
		}
		var pwArg any
		if passwordHash != nil {
			pwArg = *passwordHash
		}
		var expArg any
		if expiresAt != nil {
			expArg = *expiresAt
		}
		var revArg any
		if revokedAt != nil {
			revArg = *revokedAt
		}
		if _, err := d.Exec(`INSERT INTO share_links
			(token, page_id, include_descendants, password_hash,
			 created_by, created_at, expires_at, revoked_at)
			VALUES (?, ?, ?, ?, ?, datetime('now'), ?, ?)`,
			token, pageID, inc, pwArg, admin, expArg, revArg); err != nil {
			t.Fatalf("insert share %s: %v", token, err)
		}
	}

	pwHash := "argon2id$dummy-hash-not-used-for-verify"
	insertShare("tok-no-pw-no-desc", rootID, false, nil, nil, nil)
	insertShare("tok-no-pw-with-desc", rootID, true, nil, nil, nil)
	insertShare("tok-pw", rootID, false, &pwHash, nil, nil)
	pastTS := "2020-01-01 00:00:00"
	insertShare("tok-expired", rootID, false, nil, &pastTS, nil)
	revokedTS := "2025-01-01 00:00:00"
	insertShare("tok-revoked", rootID, false, nil, nil, &revokedTS)

	// XSS test: a separate page whose title contains a script tag. Used to
	// confirm html.EscapeString is wired through.
	xssPageID := seedPageInSpace(t, d, space, nil, `<script>alert(1)</script>`, "body")
	insertShare("tok-xss", xssPageID, false, nil, nil, nil)

	noFollow := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	get := func(ua, path string) (*http.Response, string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, ts.URL+path, nil)
		if err != nil {
			t.Fatalf("build req: %v", err)
		}
		if ua != "" {
			req.Header.Set("User-Agent", ua)
		} else {
			req.Header["User-Agent"] = nil
		}
		resp, err := noFollow.Do(req)
		if err != nil {
			t.Fatalf("do req: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp, string(body)
	}

	const botUA = "Slackbot-LinkExpanding 1.0"
	const browserUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/120.0"

	t.Run("unknown_token_bot_ua_404", func(t *testing.T) {
		resp, body := get(botUA, "/share/no-such-token")
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status=%d want 404", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("Content-Type=%q want text/html prefix", ct)
		}
		if !strings.Contains(body, "Not found") {
			t.Fatalf("body=%q missing Not found", body)
		}
	})

	t.Run("revoked_token_bot_ua_404_identical_body", func(t *testing.T) {
		resp, body := get(botUA, "/share/tok-revoked")
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status=%d want 404", resp.StatusCode)
		}
		// Compare against unknown-token body to lock the no-oracle promise.
		_, unknown := get(botUA, "/share/no-such-token-2")
		if body != unknown {
			t.Fatalf("revoked body %q != unknown body %q (oracle leak)", body, unknown)
		}
	})

	t.Run("expired_token_bot_ua_404", func(t *testing.T) {
		resp, _ := get(botUA, "/share/tok-expired")
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status=%d want 404", resp.StatusCode)
		}
	})

	t.Run("valid_no_pw_root_bot_ua_200_with_og_url", func(t *testing.T) {
		resp, body := get(botUA, "/share/tok-no-pw-no-desc")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d want 200; body=%s", resp.StatusCode, body)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
			t.Fatalf("Content-Type=%q want 'text/html; charset=utf-8'", ct)
		}
		if !strings.Contains(body, `<meta property="og:title" content="Root Page — Engineering"`) {
			t.Fatalf("missing og:title for root page; body=%s", body)
		}
		// og:url carries the cosmetic slug ("Root Page" → "root-page").
		if !strings.Contains(body, `<meta property="og:url" content="/share/tok-no-pw-no-desc/root-page"`) {
			t.Fatalf("og:url should be /share/{token}/{slug}; body=%s", body)
		}
		if !strings.Contains(body, fmt.Sprintf(`/p/%d/og.png`, rootID)) {
			t.Fatalf("og:image should reference /p/{root}/og.png; body=%s", body)
		}
	})

	t.Run("valid_descendant_bot_ua_200_descendant_title_and_url", func(t *testing.T) {
		resp, body := get(botUA, fmt.Sprintf("/share/tok-no-pw-with-desc/p/%d", childID))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d want 200; body=%s", resp.StatusCode, body)
		}
		if !strings.Contains(body, `Child Page — Engineering`) {
			t.Fatalf("descendant og:title missing; body=%s", body)
		}
		wantURL := fmt.Sprintf(`/share/tok-no-pw-with-desc/p/%d`, childID)
		if !strings.Contains(body, fmt.Sprintf(`<meta property="og:url" content="%s"`, wantURL)) {
			t.Fatalf("og:url should be %q; body=%s", wantURL, body)
		}
	})

	t.Run("descendant_not_in_subtree_404", func(t *testing.T) {
		// outsideID is in a different space — pageInShareSubtree must reject.
		resp, _ := get(botUA, fmt.Sprintf("/share/tok-no-pw-with-desc/p/%d", outsideID))
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status=%d want 404 for out-of-subtree page", resp.StatusCode)
		}
	})

	t.Run("descendant_path_on_no_descendants_share_404", func(t *testing.T) {
		// tok-no-pw-no-desc has include_descendants=false; even though child is
		// a real descendant of root, the share doesn't cover it.
		resp, _ := get(botUA, fmt.Sprintf("/share/tok-no-pw-no-desc/p/%d", childID))
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status=%d want 404 for descendant of no-descendants share", resp.StatusCode)
		}
	})

	t.Run("root_id_on_no_descendants_share_via_p_path_200", func(t *testing.T) {
		// /share/{tok}/p/{root_id} on a no-descendants share is the root page
		// itself — should still serve OG. Pin the contract.
		resp, body := get(botUA, fmt.Sprintf("/share/tok-no-pw-no-desc/p/%d", rootID))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d want 200 (root via /p/ path); body=%s", resp.StatusCode, body)
		}
		if !strings.Contains(body, `Root Page — Engineering`) {
			t.Fatalf("root og:title missing; body=%s", body)
		}
	})

	t.Run("password_share_bot_ua_serves_locked_envelope", func(t *testing.T) {
		resp, body := get(botUA, "/share/tok-pw")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d want 200 (locked OG card); body=%s", resp.StatusCode, body)
		}
		if !strings.Contains(body, "Protected page on Tela") {
			t.Fatalf("locked envelope missing generic title; body=%s", body)
		}
		// Real page title must NOT leak.
		if strings.Contains(body, "Root Page") {
			t.Fatalf("locked envelope leaked real page title; body=%s", body)
		}
		// No og:image on locked envelope — sharing the page card defeats the password.
		if strings.Contains(body, `<meta property="og:image"`) {
			t.Fatalf("locked envelope must not include og:image; body=%s", body)
		}
		// og:url is the share's root URL.
		if !strings.Contains(body, `<meta property="og:url" content="/share/tok-pw"`) {
			t.Fatalf("locked envelope og:url should point at /share/{token}; body=%s", body)
		}
	})

	t.Run("real_browser_ua_valid_share_404_defense_in_depth", func(t *testing.T) {
		// Caddy is the real branch in prod (UA-regexp matcher), but the backend
		// defensively 404s real browsers so a misconfigured Caddy block won't
		// render the OG envelope in place of the SPA.
		resp, _ := get(browserUA, "/share/tok-no-pw-no-desc")
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status=%d want 404 for browser UA", resp.StatusCode)
		}
	})

	t.Run("xss_title_is_escaped", func(t *testing.T) {
		resp, body := get(botUA, "/share/tok-xss")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d want 200; body=%s", resp.StatusCode, body)
		}
		if !strings.Contains(body, `&lt;script&gt;alert(1)&lt;/script&gt;`) {
			t.Fatalf("XSS title not escaped: %s", body)
		}
		if strings.Contains(body, `<script>alert(1)</script>`) {
			t.Fatalf("raw <script> tag in body: %s", body)
		}
	})

	t.Run("middleware_bypass_no_session_cookie_required", func(t *testing.T) {
		// Every scenario above ran without a session cookie. If
		// auth.IsPublicPath weren't extended for /share/, the middleware
		// would return 401 long before our handler ran — pin this explicitly
		// so a future revert of the IsPublicPath edit lights up here too.
		resp, _ := get(botUA, "/share/tok-no-pw-no-desc")
		if resp.StatusCode == http.StatusUnauthorized {
			t.Fatalf("middleware bypass for /share/* missing — cookie-less req returned 401")
		}
	})
}

