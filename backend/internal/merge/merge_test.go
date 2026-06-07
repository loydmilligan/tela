package merge

import (
	"reflect"
	"testing"
)

func TestMerge3_Body(t *testing.T) {
	cases := []struct {
		name              string
		base, cur, inc    string
		want              string
		wantConflictCount int
	}{
		{
			name: "no change", base: "a\nb\nc\n", cur: "a\nb\nc\n", inc: "a\nb\nc\n",
			want: "a\nb\nc\n",
		},
		{
			name: "incoming only edits", base: "a\nb\nc\n", cur: "a\nb\nc\n", inc: "a\nB\nc\n",
			want: "a\nB\nc\n",
		},
		{
			name: "current only edits", base: "a\nb\nc\n", cur: "a\nB\nc\n", inc: "a\nb\nc\n",
			want: "a\nB\nc\n",
		},
		{
			name: "both make the same edit", base: "a\nb\nc\n", cur: "a\nX\nc\n", inc: "a\nX\nc\n",
			want: "a\nX\nc\n",
		},
		{
			// The case a naive "common-anchor" merge gets wrong: each side edits a
			// DIFFERENT line → must auto-merge, not conflict.
			name: "non-overlapping edits auto-merge", base: "a\nb\nc\n", cur: "a\nB\nc\n", inc: "a\nb\nC\n",
			want: "a\nB\nC\n",
		},
		{
			name: "incoming appends", base: "a\nb\n", cur: "a\nb\n", inc: "a\nb\nc\n",
			want: "a\nb\nc\n",
		},
		{
			name: "both append at the end different lines", base: "a\n", cur: "a\nx\n", inc: "a\ny\n",
			want: "a\nx\ny\n", // adjacent insertions both kept, no conflict
		},
		{
			name: "current deletes a line incoming untouched", base: "a\nb\nc\n", cur: "a\nc\n", inc: "a\nb\nc\n",
			want: "a\nc\n",
		},
		{
			name: "overlapping edit conflicts, incoming wins", base: "a\nb\nc\n", cur: "a\nB1\nc\n", inc: "a\nB2\nc\n",
			want: "a\nB2\nc\n", wantConflictCount: 1,
		},
		{
			name: "empty base, incoming adds", base: "", cur: "", inc: "hello\n",
			want: "hello\n",
		},
		{
			// current deletes the middle line, incoming edits it → genuine conflict.
			name: "delete vs edit conflicts", base: "a\nb\nc\n", cur: "a\nc\n", inc: "a\nB\nc\n",
			want: "a\nB\nc\n", wantConflictCount: 1, // incoming wins → keeps the edited line
		},
		{
			// multi-line blocks edited on opposite ends → both apply cleanly.
			name: "multi-line non-overlapping",
			base: "h1\np1\np2\nfoot\n", cur: "h1-edited\np1\np2\nfoot\n", inc: "h1\np1\np2\nfoot-edited\n",
			want: "h1-edited\np1\np2\nfoot-edited\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, conflicts := Merge3(c.base, c.cur, c.inc, PreferIncoming)
			if got != c.want {
				t.Fatalf("merged = %q, want %q", got, c.want)
			}
			if len(conflicts) != c.wantConflictCount {
				t.Fatalf("conflicts = %d, want %d (%+v)", len(conflicts), c.wantConflictCount, conflicts)
			}
		})
	}
}

func TestMerge3_ConflictPreferCurrent(t *testing.T) {
	got, conflicts := Merge3("a\nb\nc\n", "a\nB1\nc\n", "a\nB2\nc\n", PreferCurrent)
	if got != "a\nB1\nc\n" {
		t.Fatalf("merged = %q, want current side to win", got)
	}
	if len(conflicts) != 1 || conflicts[0].Current[0] != "B1" || conflicts[0].Incoming[0] != "B2" {
		t.Fatalf("conflict not recorded with both sides: %+v", conflicts)
	}
}

func TestMerge3_RoundTripExact(t *testing.T) {
	// An unchanged side must round-trip byte-for-byte, including the trailing
	// newline (= trailing empty line after split).
	doc := "# Title\n\nSome body.\nLine two.\n"
	got, _ := Merge3(doc, doc, doc, PreferIncoming)
	if got != doc {
		t.Fatalf("round trip = %q, want %q", got, doc)
	}
}

func TestMergeProps(t *testing.T) {
	base := map[string]any{"status": "draft", "tags": []any{"x"}}
	// current changes status; incoming adds a key → both survive, no conflict.
	cur := map[string]any{"status": "review", "tags": []any{"x"}}
	inc := map[string]any{"status": "draft", "tags": []any{"x"}, "owner": "cagdas"}
	got, conflicts := MergeProps(base, cur, inc, PreferIncoming)
	if len(conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %v", conflicts)
	}
	if got["status"] != "review" || got["owner"] != "cagdas" {
		t.Fatalf("merged props = %+v, want status=review owner=cagdas", got)
	}

	// Same key changed differently → conflict, prefer incoming.
	got2, conf2 := MergeProps(
		map[string]any{"status": "draft"},
		map[string]any{"status": "review"},
		map[string]any{"status": "published"},
		PreferIncoming)
	if len(conf2) != 1 || conf2[0] != "status" {
		t.Fatalf("expected status conflict, got %v", conf2)
	}
	if got2["status"] != "published" {
		t.Fatalf("conflict winner = %v, want published (incoming)", got2["status"])
	}

	// Incoming deletes a key current left alone → key removed.
	got3, _ := MergeProps(
		map[string]any{"a": 1, "b": 2},
		map[string]any{"a": 1, "b": 2},
		map[string]any{"a": 1},
		PreferIncoming)
	if _, ok := got3["b"]; ok {
		t.Fatalf("key b should have been deleted: %+v", got3)
	}
}

func TestScalar(t *testing.T) {
	if m, c := Scalar("Old", "Old", "New", PreferIncoming); m != "New" || c {
		t.Fatalf("incoming-only retitle = (%q,%v), want (New,false)", m, c)
	}
	if m, c := Scalar("Old", "Curr", "Inc", PreferIncoming); m != "Inc" || !c {
		t.Fatalf("divergent retitle = (%q,%v), want (Inc,true)", m, c)
	}
	if m, c := Scalar("Old", "Same", "Same", PreferIncoming); m != "Same" || c {
		t.Fatalf("same retitle = (%q,%v), want (Same,false)", m, c)
	}
}

func TestLcsPairsSanity(t *testing.T) {
	pairs := lcsPairs([]string{"a", "b", "c"}, []string{"a", "x", "c"})
	want := []pair{{0, 0}, {2, 2}}
	if !reflect.DeepEqual(pairs, want) {
		t.Fatalf("lcsPairs = %v, want %v", pairs, want)
	}
}
