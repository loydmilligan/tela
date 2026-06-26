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

func TestScoreAskCompleteness_Leaks(t *testing.T) {
	// ExpectNone is the cross-project leak guard: "projectx" appears in the answer
	// (a leak), "projecty" does not. Coverage (over ExpectAll) is unaffected.
	c := AskCompletenessCase{
		Question:   "how does alpha deploy?",
		ExpectAll:  []string{"make deploy"},
		ExpectNone: []string{"projectx", "projecty"},
	}
	grounding := "Alpha ships with make deploy."
	answer := "Alpha ships with make deploy. Unrelated, projectx uses kubectl."

	sc := scoreAskCompleteness(c, grounding, answer)
	if got := join(sc.Leaks); got != "projectx" {
		t.Errorf("leaks = %q, want [projectx]", got)
	}
	if sc.Coverage != 1.0 {
		t.Errorf("coverage = %v, want 1.0 (expect_all fully covered)", sc.Coverage)
	}
}

func TestScoreAskCompleteness_LeakOnly(t *testing.T) {
	// A leak-only case (no expect_all) scores 1.0 coverage and is judged purely on
	// leaks — so a clean answer passes and a bleeding one is caught.
	c := AskCompletenessCase{Question: "how does alpha deploy?", ExpectNone: []string{"beta-secret"}}

	clean := scoreAskCompleteness(c, "grounding", "Alpha deploys via make deploy.")
	if clean.Coverage != 1.0 || len(clean.Leaks) != 0 {
		t.Errorf("clean leak-only case: coverage=%v leaks=%v, want 1.0 / none", clean.Coverage, clean.Leaks)
	}
	bleed := scoreAskCompleteness(c, "grounding", "Alpha deploys like beta-secret does.")
	if got := join(bleed.Leaks); got != "beta-secret" {
		t.Errorf("bleeding leak-only case: leaks=%q, want [beta-secret]", got)
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
