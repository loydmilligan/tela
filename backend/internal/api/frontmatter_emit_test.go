package api

import (
	"reflect"
	"strings"
	"testing"

	"github.com/zcag/tela/backend/internal/mdimport"
	"github.com/zcag/tela/backend/internal/models"
)

func TestEmitPageFrontmatter(t *testing.T) {
	p := models.Page{
		ID:        42,
		SpaceID:   1,
		Title:     "My Page",
		Body:      "# My Page\n\nbody text\n",
		Props:     map[string]any{"owner": "cagdas", "area": "infra"},
		CreatedAt: "2026-01-02 03:04:05",
		UpdatedAt: "2026-06-07 10:11:12",
	}
	out := emitPageFrontmatter(p)

	if !strings.HasPrefix(out, "---\n") {
		t.Fatalf("missing leading delimiter:\n%s", out)
	}
	if !strings.HasSuffix(out, p.Body) {
		t.Fatalf("body not appended verbatim:\n%s", out)
	}
	// System keys present and ordered before bag keys; bag keys sorted.
	for _, want := range []string{"id: 42", "title: My Page", "slug: my-page", "created:", "updated:", "area: infra", "owner: cagdas"} {
		if !strings.Contains(out, want) {
			t.Fatalf("emit missing %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "updated:") > strings.Index(out, "area:") {
		t.Fatalf("system keys must precede bag keys:\n%s", out)
	}
	if strings.Index(out, "area:") > strings.Index(out, "owner:") {
		t.Fatalf("bag keys must be sorted:\n%s", out)
	}
}

// TestEmitFrontmatter_RoundTrip is the contract: emitting then re-parsing
// recovers the body and the free-form bag exactly; the emit-only system keys are
// dropped on the way back in (they are reserved), and title is re-extracted.
func TestEmitFrontmatter_RoundTrip(t *testing.T) {
	p := models.Page{
		ID:        7,
		SpaceID:   3,
		Title:     "Round Trip",
		Body:      "the body\n",
		Props:     map[string]any{"owner": "cagdas", "tags": []any{"a", "b"}},
		CreatedAt: "2026-01-02 03:04:05",
		UpdatedAt: "2026-06-07 10:11:12",
	}
	body, title, props := mdimport.StripFrontmatter(emitPageFrontmatter(p))
	if body != p.Body {
		t.Fatalf("body=%q want %q", body, p.Body)
	}
	if title != p.Title {
		t.Fatalf("title=%q want %q", title, p.Title)
	}
	if !reflect.DeepEqual(props, p.Props) {
		t.Fatalf("props=%#v want %#v (system keys must be dropped)", props, p.Props)
	}
}
