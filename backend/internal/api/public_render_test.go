package api

import (
	"strings"
	"testing"
)

func TestRenderPublicBodyHTML(t *testing.T) {
	got := string(renderPublicBodyHTML("# Hi\n\nSome **bold** text and a [link](https://x.com).\n\n- a\n- b\n"))
	for _, want := range []string{"<h1>Hi</h1>", "<strong>bold</strong>", `<a href="https://x.com">link</a>`, "<li>a</li>"} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered body missing %q\ngot: %s", want, got)
		}
	}
	// Raw HTML in page markdown must NOT pass through into the crawler doc.
	if unsafe := string(renderPublicBodyHTML("<script>alert(1)</script>")); strings.Contains(unsafe, "<script>") {
		t.Errorf("raw <script> leaked into rendered body: %s", unsafe)
	}
	if renderPublicBodyHTML("") != "" {
		t.Error("empty body should render empty")
	}
}
