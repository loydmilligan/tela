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
	res, err := d.Exec(`INSERT INTO pages (space_id, parent_id, title, body, position)
	                    VALUES (?, NULL, 'Welcome', 'hello world body', 0)`, space)
	if err != nil {
		t.Fatalf("seed page: %v", err)
	}
	pageID, _ := res.LastInsertId()

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

	// 5. Non-bot UA (Chrome) → 302 with Location: /pages/{id}.
	resp, _ = get("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/120.0", fmt.Sprintf("/p/%d", pageID))
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("Chrome UA status=%d want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != fmt.Sprintf("/pages/%d", pageID) {
		t.Fatalf("Chrome UA Location=%q want /pages/%d", loc, pageID)
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

// TestPublicShare_XSSGuards verifies HTML-escaping of user-controlled content
// in the OG payload. Stored XSS via OG cards is a real concern — crawlers
// rebroadcast title + description into Slack / Twitter / Discord clients.
func TestPublicShare_XSSGuards(t *testing.T) {
	ts, d := newWiredServer(t)
	admin := seedUser(t, d, "admin", "adminpw12", true)
	space := seedSpace(t, d, "Engineering", "engineering", admin)

	res, err := d.Exec(`INSERT INTO pages (space_id, parent_id, title, body, position)
	                    VALUES (?, NULL, ?, ?, 0)`,
		space, `<script>alert(1)</script>`, `<img onerror="bad" src="x">`)
	if err != nil {
		t.Fatalf("seed page: %v", err)
	}
	pageID, _ := res.LastInsertId()

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

// TestPublicShare_DescriptionTruncationAndStrip pins the description-trim and
// markdown-strip contract. The truncation should be rune-aware so emoji /
// CJK titles don't split mid-codepoint.
func TestPublicShare_DescriptionTruncationAndStrip(t *testing.T) {
	ts, d := newWiredServer(t)
	admin := seedUser(t, d, "admin", "adminpw12", true)
	space := seedSpace(t, d, "Engineering", "engineering", admin)

	// 500-character body of 'a' → og:description = first 200 runes + ellipsis.
	longBody := strings.Repeat("a", 500)
	res, err := d.Exec(`INSERT INTO pages (space_id, parent_id, title, body, position)
	                    VALUES (?, NULL, 'Long', ?, 0)`, space, longBody)
	if err != nil {
		t.Fatalf("seed long: %v", err)
	}
	longID, _ := res.LastInsertId()

	// Markdown-strip body: heading + code fence + link → only `H1 link` should
	// appear in the description excerpt.
	mdBody := "# H1\n\n```js\ncode\n```\n[link](https://x)"
	res, err = d.Exec(`INSERT INTO pages (space_id, parent_id, title, body, position)
	                    VALUES (?, NULL, 'MD', ?, 1)`, space, mdBody)
	if err != nil {
		t.Fatalf("seed md: %v", err)
	}
	mdID, _ := res.LastInsertId()

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
	// Description should contain exactly 200 'a' runes followed by the
	// ellipsis horizontal-ellipsis (…).
	wantDesc := strings.Repeat("a", 200) + "…"
	if !strings.Contains(longResp, fmt.Sprintf(`<meta property="og:description" content="%s"`, wantDesc)) {
		t.Fatalf("long body description not 200a+ellipsis:\n%s", longResp)
	}

	mdResp := get(mdID)
	if !strings.Contains(mdResp, `<meta property="og:description" content="H1 link"`) {
		t.Fatalf("markdown-strip wrong: want og:description='H1 link', got:\n%s", mdResp)
	}
	if strings.Contains(mdResp, "code") {
		t.Fatalf("markdown-strip leaked code-fence content into excerpt:\n%s", mdResp)
	}
	if strings.Contains(mdResp, "# H1") {
		t.Fatalf("markdown-strip left heading marker in excerpt:\n%s", mdResp)
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
