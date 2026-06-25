package api

import "testing"

func TestEnumerationDirective(t *testing.T) {
	enum := []string{
		"which topics are used in UDN, give a table",
		"list all the services that talk to kafka",
		"what are the report types?",
		"how many loggers are there",
		"name the external delivery targets",
		"every topic the UFDR job reads",
	}
	for _, q := range enum {
		if enumerationDirective(q) == "" {
			t.Errorf("expected enumeration directive for %q, got none", q)
		}
	}
	plain := []string{
		"how does the UFDR job correlate data",
		"why is kafka used as a buffer",
		"explain the rollover policy",
		"",
	}
	for _, q := range plain {
		if d := enumerationDirective(q); d != "" {
			t.Errorf("expected no directive for %q, got %q", q, d)
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
