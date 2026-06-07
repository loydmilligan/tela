package pagemd

import (
	"reflect"
	"testing"
)

func i64(v int64) *int64 { return &v }

func TestDecodeDoc(t *testing.T) {
	cases := []struct {
		name        string
		in          string
		wantID      *int64
		wantUpdated string
		wantBody    string
		wantTitle   string
		wantProps   map[string]any
	}{
		{
			name:     "no frontmatter: body passthrough, no id",
			in:       "# Hello\n\nbody",
			wantBody: "# Hello\n\nbody",
		},
		{
			name:        "surfaces id and updated; drops other reserved keys",
			in:          "---\nid: 151\ntitle: My Page\nslug: hand-edited\nupdated: 2026-06-07 13:00:00\nstatus: live\n---\nbody",
			wantID:      i64(151),
			wantUpdated: "2026-06-07 13:00:00",
			wantBody:    "body",
			wantTitle:   "My Page",
			wantProps:   map[string]any{"status": "live"},
		},
		{
			name:      "quoted numeric id is bound",
			in:        "---\nid: \"42\"\ntitle: T\n---\nb",
			wantID:    i64(42),
			wantBody:  "b",
			wantTitle: "T",
			wantProps: map[string]any{},
		},
		{
			name:      "non-numeric id is not bound (treated as new)",
			in:        "---\nid: not-a-number\ntitle: T\n---\nb",
			wantID:    nil,
			wantBody:  "b",
			wantTitle: "T",
			wantProps: map[string]any{},
		},
		{
			name:      "id matched case-insensitively",
			in:        "---\nID: 7\nkeep: true\n---\nb",
			wantID:    i64(7),
			wantBody:  "b",
			wantProps: map[string]any{"keep": true},
		},
		{
			name:        "modified is read as the updated hint",
			in:          "---\nid: 3\nmodified: 2026-01-01\n---\nb",
			wantID:      i64(3),
			wantUpdated: "2026-01-01 00:00:00",
			wantBody:    "b",
			wantProps:   map[string]any{},
		},
		{
			name:     "thematic-break lookalike is not frontmatter",
			in:       "---\nsome prose\n---\nmore",
			wantBody: "---\nsome prose\n---\nmore",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := DecodeDoc(tc.in)
			if !reflect.DeepEqual(d.ID, tc.wantID) {
				t.Errorf("ID = %v, want %v", deref(d.ID), deref(tc.wantID))
			}
			if d.Updated != tc.wantUpdated {
				t.Errorf("Updated = %q, want %q", d.Updated, tc.wantUpdated)
			}
			if d.Body != tc.wantBody {
				t.Errorf("Body = %q, want %q", d.Body, tc.wantBody)
			}
			if d.Title != tc.wantTitle {
				t.Errorf("Title = %q, want %q", d.Title, tc.wantTitle)
			}
			if !reflect.DeepEqual(d.Props, tc.wantProps) {
				t.Errorf("Props = %v, want %v", d.Props, tc.wantProps)
			}
		})
	}
}

func deref(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}
