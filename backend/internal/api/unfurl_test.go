package api

import (
	"net"
	"testing"
)

func TestIsBlockedIP(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1":       true,  // loopback
		"::1":             true,  // loopback v6
		"10.0.0.5":        true,  // RFC1918
		"192.168.1.1":     true,  // RFC1918
		"172.16.0.1":      true,  // RFC1918
		"169.254.1.1":     true,  // link-local
		"0.0.0.0":         true,  // unspecified
		"fe80::1":         true,  // link-local v6
		"fc00::1":         true,  // ULA (private v6)
		"8.8.8.8":         false, // public
		"1.1.1.1":         false, // public
		"93.184.216.34":   false, // public (example.com)
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
