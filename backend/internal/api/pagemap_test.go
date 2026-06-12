package api

import "strings"

import "testing"

const samplePage = `Intro line.

## Setup

Install the thing.

### Linux

Use apt.

## Deploy

Run make deploy.

` + "```" + `bash
## not a heading (inside a fence)
` + "```" + `

## Notes

Old notes here.
`

func TestPageOutline(t *testing.T) {
	secs := pageOutline(samplePage)
	got := make([]string, len(secs))
	for i, s := range secs {
		got[i] = s.Path
	}
	want := []string{"Setup", "Setup > Linux", "Deploy", "Notes"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("outline paths = %v, want %v", got, want)
	}
	// Preview must be the section's OWN content (Setup → "Install the thing.",
	// not its Linux subsection), and a fenced "## not a heading" must not leak in.
	if secs[0].Preview != "Install the thing." {
		t.Fatalf("Setup preview = %q, want %q", secs[0].Preview, "Install the thing.")
	}
	if strings.Contains(secs[2].Preview, "not a heading") {
		t.Fatalf("Deploy preview leaked fenced text: %q", secs[2].Preview)
	}
}

func TestApplyPatch(t *testing.T) {
	cases := []struct {
		name, target, op, content string
		wantContains, wantAbsent  string
	}{
		{"append", "Deploy", "append", "Then verify.", "Then verify.", ""},
		{"prepend", "Setup", "prepend", "Prereqs first.", "Prereqs first.", ""},
		{"replace", "Setup > Linux", "replace", "Use the new installer.", "Use the new installer.", "Use apt."},
		{"delete", "Notes", "delete", "", "", "Old notes here."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, sec, err := applyPatch(samplePage, c.target, c.op, c.content)
			if err != nil {
				t.Fatalf("applyPatch: %v", err)
			}
			if sec == nil {
				t.Fatalf("no section matched")
			}
			if c.wantContains != "" && !strings.Contains(out, c.wantContains) {
				t.Fatalf("output missing %q:\n%s", c.wantContains, out)
			}
			if c.wantAbsent != "" && strings.Contains(out, c.wantAbsent) {
				t.Fatalf("output should have dropped %q:\n%s", c.wantAbsent, out)
			}
			// The fence content must survive every patch (never treated as a heading).
			if !strings.Contains(out, "## not a heading") {
				t.Fatalf("fenced text was lost / misparsed:\n%s", out)
			}
		})
	}
}

func TestApplyPatchUnknownTarget(t *testing.T) {
	if _, _, err := applyPatch(samplePage, "Nonexistent", "append", "x"); err == nil {
		t.Fatal("expected error for unknown target")
	}
}
