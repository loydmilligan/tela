package pagemd

import (
	"reflect"
	"strings"
	"testing"

	"github.com/zcag/tela/backend/internal/models"
)

func TestDecode(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		wantBody  string
		wantTitle string
		wantProps map[string]any
	}{
		{
			name:     "no frontmatter returns content unchanged",
			in:       "# Hello\n\nbody",
			wantBody: "# Hello\n\nbody",
		},
		{
			name:      "parses free-form props and strips block",
			in:        "---\nstatus: draft\nowner: cagdas\ntags: [a, b]\n---\nbody text",
			wantBody:  "body text",
			wantProps: map[string]any{"status": "draft", "owner": "cagdas", "tags": []any{"a", "b"}},
		},
		{
			name:      "title extracted; reserved keys dropped from bag",
			in:        "---\ntitle: My Page\nid: 999\nslug: hand-edited\ncreated: 2020-01-01\nstatus: live\n---\nbody",
			wantBody:  "body",
			wantTitle: "My Page",
			wantProps: map[string]any{"status": "live"},
		},
		{
			name:     "thematic-break lookalike is NOT frontmatter",
			in:       "---\nsome prose paragraph\n---\nmore",
			wantBody: "---\nsome prose paragraph\n---\nmore",
		},
		{
			name:     "yaml sequence is NOT frontmatter",
			in:       "---\n- one\n- two\n---\nbody",
			wantBody: "---\n- one\n- two\n---\nbody",
		},
		{
			name:      "empty frontmatter block yields empty props",
			in:        "---\n\n---\nbody",
			wantBody:  "body",
			wantProps: map[string]any{},
		},
		{
			name:      "reserved keys matched case-insensitively",
			in:        "---\nTitle: T\nID: 5\nkeep: true\n---\nb",
			wantBody:  "b",
			wantTitle: "T",
			wantProps: map[string]any{"keep": true},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body, title, props := Decode(c.in)
			if body != c.wantBody {
				t.Errorf("body = %q, want %q", body, c.wantBody)
			}
			if title != c.wantTitle {
				t.Errorf("title = %q, want %q", title, c.wantTitle)
			}
			if !reflect.DeepEqual(props, c.wantProps) {
				t.Errorf("props = %#v, want %#v", props, c.wantProps)
			}
		})
	}
}

func TestFilterReserved(t *testing.T) {
	in := map[string]any{"id": 1, "title": "x", "slug": "s", "created": "d", "status": "live", "tags": []any{"a"}}
	got := FilterReserved(in)
	want := map[string]any{"status": "live", "tags": []any{"a"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FilterReserved = %#v, want %#v", got, want)
	}
}

func TestEncode(t *testing.T) {
	p := models.Page{
		ID:        42,
		SpaceID:   1,
		Title:     "My Page",
		Body:      "# My Page\n\nbody text\n",
		Props:     map[string]any{"owner": "cagdas", "area": "infra"},
		CreatedAt: "2026-01-02 03:04:05",
		UpdatedAt: "2026-06-07 10:11:12",
	}
	out := string(Encode(p, "https://t.test"))

	if !strings.HasPrefix(out, "---\n") {
		t.Fatalf("missing leading delimiter:\n%s", out)
	}
	if !strings.HasSuffix(out, p.Body) {
		t.Fatalf("body not appended verbatim:\n%s", out)
	}
	for _, want := range []string{
		"id: 42", "title: My Page", "slug: my-page",
		"link: https://t.test/spaces/1/pages/42/my-page",
		"created:", "updated:", "area: infra", "owner: cagdas",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("encode missing %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "updated:") > strings.Index(out, "area:") {
		t.Fatalf("system keys must precede bag keys:\n%s", out)
	}
	if strings.Index(out, "area:") > strings.Index(out, "owner:") {
		t.Fatalf("bag keys must be sorted:\n%s", out)
	}
}

// TestRoundTrip is the contract: Encode then Decode recovers the body and the
// free-form bag exactly; emit-only system keys are dropped on the way back in
// (they are reserved), and title is re-extracted. baseURL is injected, so the
// kernel reads no globals.
func TestRoundTrip(t *testing.T) {
	p := models.Page{
		ID:        7,
		SpaceID:   3,
		Title:     "Round Trip",
		Body:      "the body\n",
		Props:     map[string]any{"owner": "cagdas", "tags": []any{"a", "b"}},
		CreatedAt: "2026-01-02 03:04:05",
		UpdatedAt: "2026-06-07 10:11:12",
	}
	body, title, props := Decode(string(Encode(p, "https://x")))
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

// TestEncode_Deterministic locks the property a sync/diff layer relies on:
// identical (page, baseURL) → byte-identical output.
func TestEncode_Deterministic(t *testing.T) {
	p := models.Page{
		ID: 1, SpaceID: 1, Title: "T", Body: "b",
		Props:     map[string]any{"z": 1, "a": 2, "m": 3},
		CreatedAt: "2026-01-01 00:00:00", UpdatedAt: "2026-01-01 00:00:00",
	}
	if a, b := Encode(p, "https://x"), Encode(p, "https://x"); !reflect.DeepEqual(a, b) {
		t.Fatalf("Encode not deterministic:\n%s\n---\n%s", a, b)
	}
}
