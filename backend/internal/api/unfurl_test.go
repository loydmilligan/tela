package api

import (
	"net/url"
	"net"
	"testing"
)

func TestIsBlockedIP(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1":     true,  // loopback
		"::1":           true,  // loopback v6
		"10.0.0.5":      true,  // RFC1918
		"192.168.1.1":   true,  // RFC1918
		"172.16.0.1":    true,  // RFC1918
		"169.254.1.1":   true,  // link-local
		"0.0.0.0":       true,  // unspecified
		"fe80::1":       true,  // link-local v6
		"fc00::1":       true,  // ULA (private v6)
		"8.8.8.8":       false, // public
		"1.1.1.1":       false, // public
		"93.184.216.34": false, // public (example.com)
	}
	for ipStr, want := range cases {
		ip := net.ParseIP(ipStr)
		if got := isBlockedIP(ip); got != want {
			t.Errorf("isBlockedIP(%s) = %v, want %v", ipStr, got, want)
		}
	}
	if !isBlockedIP(nil) {
		t.Errorf("isBlockedIP(nil) should be true (unparseable host)")
	}
}

func TestExtractTitle(t *testing.T) {
	cases := []struct {
		name string
		html string
		want string
	}{
		{"basic", `<html><head><title>Hello World</title></head>`, "Hello World"},
		{"entities", `<title>Tom &amp; Jerry &lt;3</title>`, "Tom & Jerry <3"},
		{"whitespace", "<title>\n  spaced\t out  \n</title>", "spaced out"},
		{"attrs", `<title data-x="1">With Attrs</title>`, "With Attrs"},
		{"case-insensitive", `<TITLE>Caps</TITLE>`, "Caps"},
		{"none", `<html><head></head></html>`, ""},
	}
	for _, c := range cases {
		if got := extractTitle([]byte(c.html)); got != c.want {
			t.Errorf("%s: extractTitle = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestExtractUnfurlMeta(t *testing.T) {
	base, _ := url.Parse("https://example.com/articles/1?ref=x")
	body := []byte(`<html><head>
	  <title> Fallback   Title </title>
	  <meta content="OG Title &amp; More" property="og:title">
	  <meta property="og:description" content='A description.'>
	  <meta name="description" content="ignored — og wins">
	  <meta property="og:site_name" content="Example">
	  <meta property="og:image" content="/img/cover.png">
	  <link rel="shortcut icon" href="../favicon.svg">
	</head><body></body></html>`)
	got := extractUnfurlMeta(base, body)
	if got.Title != "OG Title & More" {
		t.Errorf("title = %q", got.Title)
	}
	if got.Description != "A description." {
		t.Errorf("description = %q", got.Description)
	}
	if got.SiteName != "Example" {
		t.Errorf("site_name = %q", got.SiteName)
	}
	if got.Image != "https://example.com/img/cover.png" {
		t.Errorf("image = %q", got.Image)
	}
	if got.Favicon != "https://example.com/favicon.svg" {
		t.Errorf("favicon = %q", got.Favicon)
	}
}

func TestExtractUnfurlMetaFallbacks(t *testing.T) {
	base, _ := url.Parse("https://example.com/x")
	body := []byte(`<html><head><title>Only Title</title>
	  <meta name="description" content="plain desc"></head></html>`)
	got := extractUnfurlMeta(base, body)
	if got.Title != "Only Title" {
		t.Errorf("title fallback = %q", got.Title)
	}
	if got.Description != "plain desc" {
		t.Errorf("description fallback = %q", got.Description)
	}
	if got.Favicon != "https://example.com/favicon.ico" {
		t.Errorf("favicon fallback = %q", got.Favicon)
	}
	if got.Image != "" {
		t.Errorf("image should be empty, got %q", got.Image)
	}
}

func TestResolveMetaURLRejectsNonHTTP(t *testing.T) {
	base, _ := url.Parse("https://example.com/")
	if got := resolveMetaURL(base, "javascript:alert(1)"); got != "" {
		t.Errorf("javascript: url leaked: %q", got)
	}
	if got := resolveMetaURL(base, "data:image/png;base64,xx"); got != "" {
		t.Errorf("data: url leaked: %q", got)
	}
}
