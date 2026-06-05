package rag

import (
	"strings"
	"testing"
)

func TestChunkMarkdown_HeadingPath(t *testing.T) {
	body := "intro prose\n\n## Deploy\ndeploy stuff\n\n### Production\nprod stuff\n\n## Tests\ntest stuff\n"
	chunks := ChunkMarkdown("Runbook", body)
	if len(chunks) != 4 {
		t.Fatalf("want 4 chunks, got %d: %+v", len(chunks), chunks)
	}
	want := []struct{ hp, mustContain string }{
		{"", "intro prose"},
		{"Deploy", "deploy stuff"},
		{"Deploy > Production", "prod stuff"},
		{"Tests", "test stuff"},
	}
	for i, w := range want {
		if chunks[i].HeadingPath != w.hp {
			t.Errorf("chunk %d heading path = %q, want %q", i, chunks[i].HeadingPath, w.hp)
		}
		if chunks[i].Ord != i {
			t.Errorf("chunk %d ord = %d, want %d", i, chunks[i].Ord, i)
		}
	}
	// EmbedText folds in page title + heading path (contextual retrieval).
	if got := chunks[2].EmbedText; !contains(got, "Runbook — Deploy > Production") || !contains(got, "prod stuff") {
		t.Errorf("embed text missing context prefix: %q", got)
	}
}

func TestChunkMarkdown_FenceNotSplitByHeading(t *testing.T) {
	// A `#` inside a code fence must not start a new section.
	body := "## Code\n```sh\n# this is a shell comment, not a heading\necho hi\n```\nafter\n"
	chunks := ChunkMarkdown("P", body)
	if len(chunks) != 1 {
		t.Fatalf("want 1 chunk (fence kept whole), got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].HeadingPath != "Code" {
		t.Errorf("heading path = %q, want Code", chunks[0].HeadingPath)
	}
}

func TestStripExcalidrawFences(t *testing.T) {
	body := "before\n```excalidraw\n{\"type\":\"excalidraw\",\"elements\":[1,2,3]}\n```\nafter"
	got := StripExcalidrawFences(body)
	if contains(got, "elements") || contains(got, "excalidraw") {
		t.Errorf("excalidraw JSON not stripped: %q", got)
	}
	if !contains(got, "before") || !contains(got, "after") {
		t.Errorf("surrounding text lost: %q", got)
	}
}

func TestChunkMarkdown_OversizeSectionSplits(t *testing.T) {
	var big string
	for len(big) < maxChunkChars*2 {
		big += "lorem ipsum dolor sit amet consectetur adipiscing elit\n"
	}
	chunks := ChunkMarkdown("P", "## Big\n"+big)
	if len(chunks) < 2 {
		t.Fatalf("oversize section should split into >=2 chunks, got %d", len(chunks))
	}
	for _, c := range chunks {
		if c.HeadingPath != "Big" {
			t.Errorf("split chunk lost heading path: %q", c.HeadingPath)
		}
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
