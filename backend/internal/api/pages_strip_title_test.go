package api

import "testing"

func TestStripLeadingTitleH1(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		title string
		want  string
	}{
		{"matching h1 stripped", "# Premise & Goals\n\nbody text", "Premise & Goals", "body text"},
		{"case insensitive", "# premise & goals\n\nbody", "Premise & Goals", "body"},
		{"leading blank lines", "\n\n#  Title  \n\ncontent", "Title", "content"},
		{"crlf", "# Title\r\n\r\ncontent", "Title", "content"},
		{"only the h1", "# Title", "Title", ""},
		{"different h1 kept", "# Other\n\nbody", "Title", "# Other\n\nbody"},
		{"non-leading h1 kept", "intro\n\n# Title\n\nbody", "Title", "intro\n\n# Title\n\nbody"},
		{"h2 not stripped", "## Title\n\nbody", "Title", "## Title\n\nbody"},
		{"no heading", "just body", "Title", "just body"},
		{"empty body", "", "Title", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stripLeadingTitleH1(c.body, c.title); got != c.want {
				t.Errorf("stripLeadingTitleH1(%q, %q) = %q, want %q", c.body, c.title, got, c.want)
			}
		})
	}
}
