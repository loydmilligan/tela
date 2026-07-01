package pollmd

import "testing"

const sample = `Intro paragraph.

:::poll{id="offsite"}
### Where should we host the offsite?

- Lisbon
  - @ada
- Berlin
- Barcelona
:::

Outro.`

func TestApplyVote_Cast(t *testing.T) {
	out, changed, err := ApplyVote(sample, "offsite", "Berlin", "bob")
	if err != nil || !changed {
		t.Fatalf("cast: changed=%v err=%v", changed, err)
	}
	if voterOf2(out, "bob") != "Berlin" {
		t.Fatalf("bob should be under Berlin:\n%s", out)
	}
	// Untouched content survives.
	if !contains(out, "Intro paragraph.") || !contains(out, "Outro.") {
		t.Fatalf("surrounding content lost:\n%s", out)
	}
}

func TestApplyVote_Change(t *testing.T) {
	// ada starts under Lisbon; move her to Barcelona — exactly one vote for ada.
	out, changed, err := ApplyVote(sample, "offsite", "Barcelona", "ada")
	if err != nil || !changed {
		t.Fatalf("change: changed=%v err=%v", changed, err)
	}
	if got := voterOf2(out, "ada"); got != "Barcelona" {
		t.Fatalf("ada should have moved to Barcelona, got %q:\n%s", got, out)
	}
	if countVoter(out, "ada") != 1 {
		t.Fatalf("ada must appear once, got %d:\n%s", countVoter(out, "ada"), out)
	}
}

func TestApplyVote_Retract(t *testing.T) {
	out, changed, err := ApplyVote(sample, "offsite", "", "ada")
	if err != nil || !changed {
		t.Fatalf("retract: changed=%v err=%v", changed, err)
	}
	if countVoter(out, "ada") != 0 {
		t.Fatalf("ada should be gone:\n%s", out)
	}
}

func TestApplyVote_Idempotent(t *testing.T) {
	// ada already under Lisbon → re-casting the same vote is a no-op.
	_, changed, err := ApplyVote(sample, "offsite", "Lisbon", "ada")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if changed {
		t.Fatal("re-casting the same vote should not change the body")
	}
}

func TestApplyVote_Errors(t *testing.T) {
	if _, _, err := ApplyVote(sample, "nope", "Berlin", "bob"); err != ErrPollNotFound {
		t.Fatalf("want ErrPollNotFound, got %v", err)
	}
	if _, _, err := ApplyVote(sample, "offsite", "Atlantis", "bob"); err != ErrOptionNotFound {
		t.Fatalf("want ErrOptionNotFound, got %v", err)
	}
}

// The editor serializes bullets as `*` with `{#id}` shorthand — votes must still
// work against that form after a page has been round-tripped through the editor.
const editorSaved = "&#x20;(edited)\n\n:::poll{#offsite}\n### Where?\n\n* Lisbon\n\n  * @ada\n\n* Berlin\n\n* Barcelona\n:::\n\nOutro."

func TestApplyVote_StarMarkersAndHashId(t *testing.T) {
	out, changed, err := ApplyVote(editorSaved, "offsite", "Berlin", "bob")
	if err != nil || !changed {
		t.Fatalf("star-marker cast: changed=%v err=%v", changed, err)
	}
	// bob lands under Berlin with the sibling `*` marker.
	if !contains(out, "* Berlin\n  * @bob") {
		t.Fatalf("bob not under Berlin with * marker:\n%s", out)
	}
	// Changing ada's existing `*` vote still nets one vote.
	out2, _, err := ApplyVote(editorSaved, "offsite", "Barcelona", "ada")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if countVoter(out2, "ada") != 1 || voterOf2(out2, "ada") != "Barcelona" {
		t.Fatalf("ada move wrong:\n%s", out2)
	}
}

func TestAttrID(t *testing.T) {
	cases := map[string]string{
		`:::poll{id="offsite"}`: "offsite",
		`:::poll{id=offsite}`:   "offsite",
		`:::poll{#offsite}`:     "offsite",
		`:::poll{}`:             "",
	}
	for line, want := range cases {
		if got := attrID(line); got != want {
			t.Errorf("attrID(%q) = %q, want %q", line, got, want)
		}
	}
}

// --- helpers ---

// voterOf2 returns the option label the voter sits under, scanning the block.
func voterOf2(body, user string) string {
	lines := splitLines(body)
	option := ""
	for _, ln := range lines {
		if v := voterOf(ln); v != "" {
			if v == user {
				return option
			}
			continue
		}
		trimmed := trimLead(ln)
		if content, _, ok := bulletItem(trimmed); ok && trimmed == ln {
			option = content
		}
	}
	return ""
}

func countVoter(body, user string) int {
	n := 0
	for _, ln := range splitLines(body) {
		if voterOf(ln) == user {
			n++
		}
	}
	return n
}

func splitLines(s string) []string {
	out, cur := []string{}, ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	return append(out, cur)
}

func trimLead(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return s[i:]
}

func contains(hay, needle string) bool {
	return len(hay) >= len(needle) && indexOf(hay, needle) >= 0
}

func indexOf(hay, needle string) int {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
