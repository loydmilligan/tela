package api

import (
	"strings"
	"testing"
)

// TestBuildBacklinkSnippet exercises buildBacklinkSnippet for the edge cases
// called out in the M5.2 refactorer audit: bare-URL-only bodies, edges,
// multi-byte runes adjacent to the window, repeated wikilinks to the same
// target, and (the audit's core finding) markdown-wrapped wikilinks where
// raw `](` punctuation was bleeding into the rendered snippet.
func TestBuildBacklinkSnippet(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		targetID int64
		want     string // exact match when non-empty
		// when wantContains is non-nil, every entry must appear in the result
		wantContains []string
		// when wantNotContains is non-nil, none of these may appear
		wantNotContains []string
	}{
		{
			name:     "bare URL only — no surrounding prose",
			body:     "tela://page/5",
			targetID: 5,
			want:     "",
		},
		{
			name:     "URL at body start — graceful, no panic, no leading word",
			body:     "tela://page/5 trailing prose here",
			targetID: 5,
			wantContains: []string{
				"<mark>",
				"</mark>",
			},
			wantNotContains: []string{"tela://page/5"},
		},
		{
			name:     "URL at body end — graceful, no panic",
			body:     "Lorem ipsum see tela://page/5",
			targetID: 5,
			wantContains: []string{
				"Lorem",
				"<mark>",
				"</mark>",
			},
			wantNotContains: []string{"tela://page/5"},
		},
		{
			name:     "multi-byte runes adjacent to window edges (emoji)",
			body:     strings.Repeat("🚀", 30) + " context see tela://page/7 then " + strings.Repeat("🌟", 30),
			targetID: 7,
			wantContains: []string{
				"<mark>",
				"</mark>",
			},
			wantNotContains: []string{"tela://page/7"},
		},
		{
			name:     "multi-byte runes adjacent to window edges (Turkish)",
			body:     "Türkçe başlık metni bir şey görüşü gözükür see tela://page/9 sonra başka şeyler yazıyor",
			targetID: 9,
			wantContains: []string{
				"<mark>",
				"</mark>",
			},
			wantNotContains: []string{"tela://page/9"},
		},
		{
			name:     "multiple wikilinks to the same target — first occurrence wins",
			body:     "See [First](tela://page/5) and later [Second](tela://page/5).",
			targetID: 5,
			wantContains: []string{
				"<mark>First</mark>",
				"Second",
			},
			wantNotContains: []string{
				"[First](",
				"[Second](",
				"](",
				"tela://page/5",
				"<mark>Second</mark>",
			},
		},
		{
			name:     "wrapped wikilink in prose — shows surrounding context, no raw punctuation",
			body:     "Lorem ipsum see [Architecture overview](tela://page/2) for details.",
			targetID: 2,
			want:     "Lorem ipsum see Architecture <mark>overview</mark> for details.",
		},
		{
			name:     "bare URL appears before wrapped occurrence — anchor at first hit",
			body:     "An early tela://page/3 reference and then later [Title](tela://page/3) more text.",
			targetID: 3,
			wantContains: []string{
				"<mark>",
				"</mark>",
				"Title",
			},
			wantNotContains: []string{
				"tela://page/3",
				"](",
			},
		},
		{
			name:     "wrapped wikilink to a different target is collapsed but does not anchor",
			body:     "See [Other](tela://page/99) and then [Mine](tela://page/4) please.",
			targetID: 4,
			wantContains: []string{
				"Other",
				"<mark>Mine</mark>",
				"please",
			},
			wantNotContains: []string{
				"[Other](",
				"[Mine](",
				"](",
				"tela://page/4",
				"tela://page/99",
			},
		},
		{
			name:     "non-referenced target returns empty",
			body:     "Lorem ipsum dolor sit amet.",
			targetID: 42,
			want:     "",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := buildBacklinkSnippet(tc.body, tc.targetID)
			if tc.want != "" {
				if got != tc.want {
					t.Fatalf("snippet mismatch:\n got:  %q\n want: %q", got, tc.want)
				}
				return
			}
			if tc.want == "" && len(tc.wantContains) == 0 && len(tc.wantNotContains) == 0 {
				if got != "" {
					t.Fatalf("expected empty snippet, got %q", got)
				}
				return
			}
			for _, sub := range tc.wantContains {
				if !strings.Contains(got, sub) {
					t.Fatalf("snippet %q is missing expected substring %q", got, sub)
				}
			}
			for _, sub := range tc.wantNotContains {
				if strings.Contains(got, sub) {
					t.Fatalf("snippet %q unexpectedly contains %q", got, sub)
				}
			}
		})
	}
}
