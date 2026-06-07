package pagemd

import "testing"

func TestSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Case 12 — RAN Site Outage RCA: Data Flow", "case-12-ran-site-outage-rca-data-flow"},
		{"Şağlam İçöü Güneş", "saglam-icou-gunes"},
		{"  Hello,  World!  ", "hello-world"},
		{"Héllo Wörld café", "hello-world-cafe"},
		{"already-a-slug", "already-a-slug"},
		{"UPPER Case", "upper-case"},
		{"日本語のページ", ""}, // CJK-only → empty
		{"🎉🎉🎉", ""},     // emoji-only → empty
		{"", ""},        // empty title
		{"---", ""},     // punctuation-only
		{"Mixed 日本 text", "mixed-text"},
	}
	for _, c := range cases {
		if got := Slug(c.in); got != c.want {
			t.Errorf("Slug(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSlug_TruncatesAtWordBoundary(t *testing.T) {
	long := "one two three four five six seven eight nine ten eleven twelve thirteen"
	got := Slug(long)
	if len(got) > maxSlugLen {
		t.Fatalf("slug too long (%d): %q", len(got), got)
	}
	if got[len(got)-1] == '-' {
		t.Fatalf("slug ends with '-': %q", got)
	}
}
