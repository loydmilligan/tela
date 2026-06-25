package api

import (
	"strings"
	"testing"
)

func TestAskUserPromptCarriesDirective(t *testing.T) {
	// The completeness directive is always appended (self-scoping), and the
	// excerpts + question are preserved verbatim so retrieval/citation are intact.
	p := askUserPrompt("EXCERPTS_HERE", "", "which topics are used?")
	for _, want := range []string{"EXCERPTS_HERE", "which topics are used?", "be exhaustive"} {
		if !strings.Contains(p, want) {
			t.Errorf("askUserPrompt missing %q in:\n%s", want, p)
		}
	}
}

func TestScoreAskCompleteness(t *testing.T) {
	c := AskCompletenessCase{
		Question:  "which topics are used, give a table",
		ExpectAll: []string{"ufdr-info", "ufdr-nat", "radresult", "  "}, // blank skipped
	}
	// ufdr-info: in answer (covered). ufdr-nat: in grounding, absent from answer
	// (generation drop). radresult: nowhere (retrieval gap).
	grounding := "The UFDR job reads ufdr-info and writes to ufdr-nat."
	answer := "| Topic |\n| UFDR-INFO |" // case-insensitive match on ufdr-info only

	sc := scoreAskCompleteness(c, grounding, answer)
	if got := join(sc.Covered); got != "ufdr-info" {
		t.Errorf("covered = %q, want [ufdr-info]", got)
	}
	if got := join(sc.GenerationDrops); got != "ufdr-nat" {
		t.Errorf("generation_drops = %q, want [ufdr-nat]", got)
	}
	if got := join(sc.RetrievalGaps); got != "radresult" {
		t.Errorf("retrieval_gaps = %q, want [radresult]", got)
	}
	if sc.Coverage != 1.0/3.0 {
		t.Errorf("coverage = %v, want 1/3 (blank expectation must not count)", sc.Coverage)
	}
}

func join(xs []string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += ","
		}
		out += x
	}
	return out
}
