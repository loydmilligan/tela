package api

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestPublicShare_FullFlow exercises GET /p/{id} end-to-end through the wired
// stack: bot UA → 200 OG HTML, real browser → 302 SPA, missing page → 404,
// XSS escaping, markdown-strip, rune truncation, slug-suffix variant, and the
// auth-middleware bypass that lets cookie-less crawlers reach the handler.
func TestPublicShare_FullFlow(t *testing.T) {
	ts, d := newWiredServer(t)
	admin := seedUser(t, d, "admin", "adminpw12", true)
	space := seedSpace(t, d, "Engineering", "engineering", admin)

	// Seed a vanilla page.
	var pageID int64
	err := d.QueryRow(`INSERT INTO pages (space_id, parent_id, title, body, position)
	                    VALUES ($1, NULL, 'Welcome', 'hello world body', 0) RETURNING id`, space).Scan(&pageID)
	if err != nil {
		t.Fatalf("seed page: %v", err)
	}

	// Cookie-less client whose redirects are surfaced rather than followed,
	// so the test can assert 302 status + Location header.
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
			// Go's default http.Client sets a UA we explicitly want absent
			// for the missing-UA scenario.
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

	// 1. Bot UA "Slackbot-LinkExpanding 1.0" → 200 OG HTML with og:title +
	//    og:image referencing /p/{id}/og.png.
	resp, body := get("Slackbot-LinkExpanding 1.0", fmt.Sprintf("/p/%d", pageID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Slackbot status=%d want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Slackbot Content-Type=%q want text/html prefix", ct)
	}
	if !strings.Contains(body, `<meta property="og:title"`) {
		t.Fatalf("Slackbot body missing og:title: %s", body)
	}
	if !strings.Contains(body, fmt.Sprintf(`/p/%d/og.png`, pageID)) {
		t.Fatalf("Slackbot body missing /p/{id}/og.png reference: %s", body)
	}
	if got := resp.Header.Get("Cache-Control"); got != "public, max-age=300" {
		t.Fatalf("Cache-Control=%q want 'public, max-age=300'", got)
	}

	// 2. facebookexternalhit → 200 OG HTML.
	resp, _ = get("facebookexternalhit/1.1", fmt.Sprintf("/p/%d", pageID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("facebookexternalhit status=%d want 200", resp.StatusCode)
	}

	// 3. Case-insensitive: SLACKBOT-LINKEXPANDING → 200.
	resp, _ = get("SLACKBOT-LINKEXPANDING/2.0", fmt.Sprintf("/p/%d", pageID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("uppercase bot UA status=%d want 200", resp.StatusCode)
	}

	// 4. Generic *bot* via `bot/` substring: MyCustomCrawlerBot/1.0 → 200.
	resp, _ = get("MyCustomCrawlerBot/1.0", fmt.Sprintf("/p/%d", pageID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("generic bot UA status=%d want 200", resp.StatusCode)
	}

	// 5. Non-bot UA (Chrome) → 302 to the in-app SPA route
	//    /spaces/{spaceID}/pages/{id}/{slug}.
	resp, _ = get("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/120.0", fmt.Sprintf("/p/%d", pageID))
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("Chrome UA status=%d want 302", resp.StatusCode)
	}
	wantLoc := pageAppPath(space, pageID, "Welcome")
	if loc := resp.Header.Get("Location"); loc != wantLoc {
		t.Fatalf("Chrome UA Location=%q want %q", loc, wantLoc)
	}

	// 6. Missing UA → 302 (treated as human; no UA, no allowlist match).
	resp, _ = get("", fmt.Sprintf("/p/%d", pageID))
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("missing UA status=%d want 302", resp.StatusCode)
	}

	// 7. /p/9999 (missing) → 404 HTML.
	resp, body = get("Slackbot-LinkExpanding 1.0", "/p/9999")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing page status=%d want 404", resp.StatusCode)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("missing page Content-Type=%q want text/html", resp.Header.Get("Content-Type"))
	}
	if !strings.Contains(body, "Not found") {
		t.Fatalf("missing page body=%s missing 'Not found'", body)
	}

	// 8. Slug suffix (/p/{id}/some-slug) → same 200 OG HTML as /p/{id}.
	resp, body = get("Slackbot-LinkExpanding 1.0", fmt.Sprintf("/p/%d/some-slug", pageID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("slug-suffix status=%d want 200", resp.StatusCode)
	}
	if !strings.Contains(body, `<meta property="og:title"`) {
		t.Fatalf("slug-suffix body missing og:title: %s", body)
	}

	// 9. No-auth-required: this whole test has been hitting /p/* with
	//    cookie-less requests; if the middleware bypass were missing every
	//    bot scenario would have 401'd. Pin it explicitly with an assertion
	//    that the bot scenario above came back 200.
	if resp.StatusCode == http.StatusUnauthorized {
		t.Fatalf("middleware bypass for /p/* missing — cookie-less request returned 401")
	}
}

// TestPublicShare_BrandsCustomDomain pins that a /p/{id} permalink for a page in
// an org with an active custom domain unfurls with og:url + og:image on that
// domain (not the canonical host) — /p/* is served on custom domains too, so a
// permalink copied from a white-label app must brand to the org's domain, like
// /share/*. With no custom domain the origin stays canonical (path-only here).
func TestPublicShare_BrandsCustomDomain(t *testing.T) {
	ts, d := newWiredServer(t)
	owner := seedUser(t, d, "owner", "ownerpw12", true)
	org := seedOrg(t, d, "Acme", "acme")
	space := seedSpace(t, d, "Docs", "docs", owner)
	if _, err := d.Exec(`UPDATE spaces SET org_id = $1 WHERE id = $2`, org, space); err != nil {
		t.Fatalf("set space org: %v", err)
	}
	if _, err := d.Exec(
		`INSERT INTO org_hostnames (hostname, org_id, status, verify_token) VALUES ($1,$2,'active',$3)`,
		"wiki.acme.example", org, "tok"); err != nil {
		t.Fatalf("insert hostname: %v", err)
	}
	var pageID int64
	if err := d.QueryRow(`INSERT INTO pages (space_id, parent_id, title, body, position)
	                       VALUES ($1, NULL, 'Welcome', 'body', 0) RETURNING id`, space).Scan(&pageID); err != nil {
		t.Fatalf("seed page: %v", err)
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+fmt.Sprintf("/p/%d", pageID), nil)
	req.Header.Set("User-Agent", "Slackbot-LinkExpanding 1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	s := string(body)

	wantURL := fmt.Sprintf(`<meta property="og:url" content="https://wiki.acme.example/p/%d/welcome"`, pageID)
	if !strings.Contains(s, wantURL) {
		t.Fatalf("og:url not branded to custom domain; want %q\n%s", wantURL, s)
	}
	wantImg := fmt.Sprintf(`https://wiki.acme.example/p/%d/og.png`, pageID)
	if !strings.Contains(s, wantImg) {
		t.Fatalf("og:image not branded to custom domain; want %q\n%s", wantImg, s)
	}
}

// TestPublicShare_XSSGuards verifies HTML-escaping of user-controlled content
// in the OG payload. Stored XSS via OG cards is a real concern — crawlers
// rebroadcast title + description into Slack / Twitter / Discord clients.
func TestPublicShare_XSSGuards(t *testing.T) {
	ts, d := newWiredServer(t)
	admin := seedUser(t, d, "admin", "adminpw12", true)
	space := seedSpace(t, d, "Engineering", "engineering", admin)

	var pageID int64
	err := d.QueryRow(`INSERT INTO pages (space_id, parent_id, title, body, position)
	                    VALUES ($1, NULL, $2, $3, 0) RETURNING id`,
		space, `<script>alert(1)</script>`, `<img onerror="bad" src="x">`).Scan(&pageID)
	if err != nil {
		t.Fatalf("seed page: %v", err)
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+fmt.Sprintf("/p/%d", pageID), nil)
	req.Header.Set("User-Agent", "Slackbot-LinkExpanding 1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	s := string(body)

	if !strings.Contains(s, `&lt;script&gt;alert(1)&lt;/script&gt;`) {
		t.Fatalf("XSS: og:title raw script-tag content not escaped:\n%s", s)
	}
	// The dangerous attribute substring `onerror="bad"` must not appear
	// verbatim inside the meta content — html.EscapeString escapes the
	// double quote, so the inner double-quote on `"bad"` becomes &#34;
	if strings.Contains(s, `onerror="bad"`) {
		t.Fatalf("XSS: og:description raw onerror= attribute leaked unescaped:\n%s", s)
	}
}

// TestPublicShare_DescriptionIsTitleOnly pins the interim privacy contract
// (docs/visibility-model.md): the /p/{id} OG envelope must NOT carry a body
// excerpt — it's crawler-reachable for any page with no auth or share link, so
// og:description is the page title only, never page content. stripMarkdownToText
// stays unit-tested below for the future share-gated rich description.
func TestPublicShare_DescriptionIsTitleOnly(t *testing.T) {
	ts, d := newWiredServer(t)
	admin := seedUser(t, d, "admin", "adminpw12", true)
	space := seedSpace(t, d, "Engineering", "engineering", admin)

	// A long body that, under the old behaviour, would have leaked as a 200-char
	// excerpt. It must NOT appear in the envelope now.
	longBody := strings.Repeat("a", 500)
	var longID int64
	err := d.QueryRow(`INSERT INTO pages (space_id, parent_id, title, body, position)
	                    VALUES ($1, NULL, 'Long', $2, 0) RETURNING id`, space, longBody).Scan(&longID)
	if err != nil {
		t.Fatalf("seed long: %v", err)
	}

	// A markdown body whose stripped text ("H1 link") used to surface in the
	// description — must no longer appear.
	mdBody := "# H1\n\n```js\ncode\n```\n[link](https://x)"
	var mdID int64
	err = d.QueryRow(`INSERT INTO pages (space_id, parent_id, title, body, position)
	                    VALUES ($1, NULL, 'MD', $2, 1) RETURNING id`, space, mdBody).Scan(&mdID)
	if err != nil {
		t.Fatalf("seed md: %v", err)
	}

	get := func(id int64) string {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, ts.URL+fmt.Sprintf("/p/%d", id), nil)
		req.Header.Set("User-Agent", "Slackbot-LinkExpanding 1.0")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return string(body)
	}

	longResp := get(longID)
	if !strings.Contains(longResp, `<meta property="og:description" content="Long"`) {
		t.Fatalf("description should be the title 'Long', got:\n%s", longResp)
	}
	// The body excerpt must not have leaked into the envelope at all.
	if strings.Contains(longResp, strings.Repeat("a", 50)) {
		t.Fatalf("body content leaked into /p/ envelope:\n%s", longResp)
	}

	mdResp := get(mdID)
	if !strings.Contains(mdResp, `<meta property="og:description" content="MD"`) {
		t.Fatalf("description should be the title 'MD', got:\n%s", mdResp)
	}
	for _, leak := range []string{"H1 link", "code", "# H1"} {
		if strings.Contains(mdResp, leak) {
			t.Fatalf("body content %q leaked into /p/ envelope:\n%s", leak, mdResp)
		}
	}
}

// TestPublicShare_UnitHelpers covers the small helpers (isBotUA,
// stripMarkdownToText, runeTruncate) in isolation so failures localise to
// the right place when the integration tests above light up.
func TestPublicShare_UnitHelpers(t *testing.T) {
	t.Run("isBotUA", func(t *testing.T) {
		cases := []struct {
			ua   string
			want bool
		}{
			{"", false},
			{"Mozilla/5.0", false},
			{"Slackbot-LinkExpanding 1.0", true},
			{"SLACKBOT-LINKEXPANDING/2.0", true},
			{"Twitterbot/1.0", true},
			{"facebookexternalhit/1.1", true},
			{"Discordbot/2.0", true},
			{"TelegramBot (like TwitterBot)", true},
			{"LinkedInBot/1.0", true},
			{"WhatsApp/2.0", true},
			{"MyCustomCrawlerBot/1.0", true}, // matches "bot/"
			{"BotName Bot 5.0", true},        // matches "bot "
			{"NotABotEater", false},          // no "bot/" or "bot " or named bot
		}
		for _, c := range cases {
			if got := isBotUA(c.ua); got != c.want {
				t.Errorf("isBotUA(%q)=%v want %v", c.ua, got, c.want)
			}
		}
	})

	t.Run("runeTruncate", func(t *testing.T) {
		if got := runeTruncate("hello", 10); got != "hello" {
			t.Errorf("short: got %q", got)
		}
		if got := runeTruncate("hello", 5); got != "hello" {
			t.Errorf("exact: got %q", got)
		}
		if got := runeTruncate("helloworld", 5); got != "hello…" {
			t.Errorf("over: got %q", got)
		}
		// Rune-aware: emoji is one codepoint (4 bytes in UTF-8). Slicing on
		// bytes would corrupt; runes must not.
		if got := runeTruncate("a😀b😀c", 3); got != "a😀b…" {
			t.Errorf("emoji: got %q", got)
		}
	})

	t.Run("stripMarkdownToText", func(t *testing.T) {
		cases := []struct {
			in   string
			want string
		}{
			{"# H1\n\n```js\ncode\n```\n[link](https://x)", "H1 link"},
			{"plain text", "plain text"},
			{"[Foo](tela://page/42) bar", "Foo bar"},
			{"![alt](img.png) caption", "caption"},
			{"`inline` code", "inline code"},
			{"~~~bash\nsecret\n~~~ visible", "visible"},
			{"  multiple\t\twhitespace\n\nruns  ", "multiple whitespace runs"},
		}
		for _, c := range cases {
			if got := stripMarkdownToText(c.in); got != c.want {
				t.Errorf("stripMarkdownToText(%q)=%q want %q", c.in, got, c.want)
			}
		}
	})
}
