package api

import "testing"

func TestPageSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Case 12 — RAN Site Outage RCA: Data Flow", "case-12-ran-site-outage-rca-data-flow"},
		{"Şağlam İçöü Güneş", "saglam-icou-gunes"},
		{"  Hello,  World!  ", "hello-world"},
		{"Héllo Wörld café", "hello-world-cafe"},
		{"already-a-slug", "already-a-slug"},
		{"UPPER Case", "upper-case"},
		{"日本語のページ", ""},          // CJK-only → empty
		{"🎉🎉🎉", ""},               // emoji-only → empty
		{"", ""},                   // empty title
		{"---", ""},                // punctuation-only
		{"Mixed 日本 text", "mixed-text"},
	}
	for _, c := range cases {
		if got := pageSlug(c.in); got != c.want {
			t.Errorf("pageSlug(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPageSlug_TruncatesAtWordBoundary(t *testing.T) {
	long := "one two three four five six seven eight nine ten eleven twelve thirteen"
	got := pageSlug(long)
	if len(got) > maxSlugLen {
		t.Fatalf("slug too long (%d): %q", len(got), got)
	}
	// Must not end mid-word (no trailing partial token) and not end with '-'.
	if got[len(got)-1] == '-' {
		t.Fatalf("slug ends with '-': %q", got)
	}
}

func TestPagePermalinkPath(t *testing.T) {
	if got := pagePermalinkPath(326, "Case 12 — RAN Site Outage RCA: Data Flow"); got != "/p/326/case-12-ran-site-outage-rca-data-flow" {
		t.Errorf("permalink = %q", got)
	}
	if got := pagePermalinkPath(7, "🎉"); got != "/p/7" {
		t.Errorf("emoji-only permalink should be bare id, got %q", got)
	}
}
