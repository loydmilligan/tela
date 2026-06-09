package rag

import (
	"context"
	"strings"
	"testing"

	"github.com/zcag/tela/backend/internal/testdb"
)

func TestKnowledgeGaps_SurfacesUnanswered(t *testing.T) {
	d := testdb.New(t)
	ctx := context.Background()
	u := newUser(t, d, "alice")
	sp := newSpace(t, d, "alpha", u)
	svc := NewServiceWithEmbedder(d, &fakeEmbedder{})

	// "how do I configure SSO" asked 3×, never answered → a gap.
	for i := 0; i < 3; i++ {
		if err := svc.LogAsk(ctx, u, &sp, "How do I configure SSO?", 0, 0); err != nil {
			t.Fatalf("logask: %v", err)
		}
	}
	// "deploy" asked once, answered → not a gap.
	if err := svc.LogAsk(ctx, u, &sp, "How to deploy?", 4, 0.8); err != nil {
		t.Fatalf("logask: %v", err)
	}

	gaps, err := svc.KnowledgeGaps(ctx, 0, 50)
	if err != nil {
		t.Fatalf("gaps: %v", err)
	}
	if len(gaps) != 1 {
		t.Fatalf("gaps = %d, want 1 (only the unanswered SSO question)", len(gaps))
	}
	if gaps[0].Asks != 3 || gaps[0].Answered != 0 {
		t.Errorf("gap = %+v, want asks=3 answered=0", gaps[0])
	}
	if !strings.Contains(strings.ToLower(gaps[0].Question), "sso") {
		t.Errorf("gap question = %q, want the SSO one", gaps[0].Question)
	}
}
