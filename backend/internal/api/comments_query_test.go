package api

import (
	"context"
	"testing"
	"time"

	"github.com/zcag/tela/backend/internal/auth"
)

// The headline: comments carry a queryable props bag, and a comment query is
// gated by the comment's PAGE access — a non-member must never see discussion
// on a page they cannot read.
func TestMCP_QueryComments(t *testing.T) {
	ts, d := newWiredServer(t)
	owner := seedUser(t, d, "qcowner", "ownerpw12", false)
	outsider := seedUser(t, d, "qcoutsider", "outsiderpw12", false)

	spaceA := seedSpace(t, d, "Alpha", "alpha-c", owner)
	spaceB := seedSpace(t, d, "Bravo", "bravo-c", owner) // owner-only

	pageA := seedPropsPage(t, d, spaceA, "Runbook", `{}`)
	pageB := seedPropsPage(t, d, spaceB, "Secret", `{}`)

	mkComment := func(pageID int64, body, props string) int64 {
		t.Helper()
		var id int64
		if err := d.QueryRowContext(context.Background(),
			`INSERT INTO comments (page_id, author_id, body, resolved, created_at, updated_at, props)
			 VALUES ($1, $2, $3, 0, tela_now(), tela_now(), $4::jsonb) RETURNING id`,
			pageID, owner, body, props).Scan(&id); err != nil {
			t.Fatalf("insert comment: %v", err)
		}
		return id
	}

	mkComment(pageA, "shipped the query block", `{"type":"change","summary":"query block","status":"done"}`)
	mkComment(pageA, "decided on jsonb_path_ops", `{"type":"decision","summary":"index choice"}`)
	mkComment(pageA, "just a chat message", `{}`)
	mkComment(pageB, "secret change", `{"type":"change","summary":"private work"}`)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	sess := mcpSession(t, ctx, ts, seedReadKey(t, d, owner, auth.ScopeRead))

	summaries := func(out queryCommentsOut) map[string]bool {
		m := map[string]bool{}
		for _, c := range out.Comments {
			if s, ok := c.Props["summary"].(string); ok {
				m[s] = true
			}
		}
		return m
	}

	t.Run("containment filters comment props", func(t *testing.T) {
		var out queryCommentsOut
		mcpCallJSON(t, ctx, sess, "query_comments",
			map[string]any{"where": map[string]any{"type": "change"}}, &out)
		got := summaries(out)
		if !got["query block"] || !got["private work"] {
			t.Errorf("owner should see both change-comments; got %v", got)
		}
		if got["index choice"] {
			t.Errorf("type=change matched a decision: %v", got)
		}
		if len(out.Comments) != 2 {
			t.Errorf("want 2 change-comments, got %d", len(out.Comments))
		}
	})

	t.Run("a props-less comment is excluded by a filter but included by an empty where", func(t *testing.T) {
		var out queryCommentsOut
		mcpCallJSON(t, ctx, sess, "query_comments", map[string]any{}, &out)
		if len(out.Comments) != 4 {
			t.Errorf("empty where should match every readable comment (4), got %d", len(out.Comments))
		}
	})

	// The changelog-footer shape: one page's change-comments.
	t.Run("page_id scopes to one page's changelog", func(t *testing.T) {
		var out queryCommentsOut
		mcpCallJSON(t, ctx, sess, "query_comments",
			map[string]any{"where": map[string]any{"type": "change"}, "page_id": pageA}, &out)
		got := summaries(out)
		if !got["query block"] {
			t.Errorf("page A changelog missing its change; got %v", got)
		}
		if got["private work"] {
			t.Errorf("page_id=A leaked a page B comment: %v", got)
		}
	})

	t.Run("a non-member sees no comments on a private page", func(t *testing.T) {
		osess := mcpSession(t, ctx, ts, seedReadKey(t, d, outsider, auth.ScopeRead))
		var out queryCommentsOut
		mcpCallJSON(t, ctx, osess, "query_comments", map[string]any{}, &out)
		if len(out.Comments) != 0 {
			t.Fatalf("outsider (member of nothing) got comments: %v", summaries(out))
		}

		// Granted read on A only: A's comments appear, B's still never do.
		seedMember(t, d, spaceA, outsider, "viewer")
		var out2 queryCommentsOut
		mcpCallJSON(t, ctx, osess, "query_comments",
			map[string]any{"where": map[string]any{"type": "change"}}, &out2)
		got := summaries(out2)
		if !got["query block"] {
			t.Errorf("A viewer should see A's change-comment; got %v", got)
		}
		if got["private work"] {
			t.Fatalf("space_access gate leaked a space B comment to a non-member: %v", got)
		}
	})

	_ = pageB
}

// add_comment (the agent front door) round-trips a structured change-comment
// into the queryable bag — the proof the two halves connect.
func TestMCP_AddComment_Props(t *testing.T) {
	ts, d := newWiredServer(t)
	owner := seedUser(t, d, "acowner", "ownerpw12", false)
	space := seedSpace(t, d, "Docs", "docs-ac", owner)
	page := seedPropsPage(t, d, space, "Runbook", `{}`)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	sess := mcpSession(t, ctx, ts, seedReadKey(t, d, owner, auth.ScopeWrite))

	var added addCommentOut
	mcpCallJSON(t, ctx, sess, "add_comment", map[string]any{
		"page_id": page,
		"body":    "bumped the index to jsonb_path_ops",
		"anchor":  map[string]any{"prefix": "", "exact": "b", "suffix": ""},
		"props":   map[string]any{"type": "change", "summary": "index bump", "status": "done"},
	}, &added)

	if added.Comment.Props["summary"] != "index bump" {
		t.Fatalf("props did not round-trip on create: %#v", added.Comment.Props)
	}

	// And it is immediately queryable as a change-comment.
	var out queryCommentsOut
	mcpCallJSON(t, ctx, sess, "query_comments",
		map[string]any{"where": map[string]any{"type": "change"}, "page_id": page}, &out)
	if len(out.Comments) != 1 || out.Comments[0].Props["summary"] != "index bump" {
		t.Fatalf("agent-authored change-comment not queryable: %#v", out.Comments)
	}
	if out.Comments[0].PageTitle != "Runbook" || out.Comments[0].Author != "acowner" {
		t.Errorf("row lacks page/author context: %#v", out.Comments[0])
	}
}

// A comment with no props still works everywhere (the default '{}' bag).
func TestComments_PropsDefaultEmpty(t *testing.T) {
	ts, d := newWiredServer(t)
	owner := seedUser(t, d, "dpowner", "ownerpw12", false)
	space := seedSpace(t, d, "Docs", "docs-dp", owner)
	page := seedPropsPage(t, d, space, "P", `{}`)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	sess := mcpSession(t, ctx, ts, seedReadKey(t, d, owner, auth.ScopeWrite))

	var added addCommentOut
	mcpCallJSON(t, ctx, sess, "add_comment", map[string]any{
		"page_id": page,
		"body":    "no props here",
		"anchor":  map[string]any{"prefix": "", "exact": "b", "suffix": ""},
	}, &added)
	// An empty bag is absent on the wire (omitempty, matching pages) — what
	// matters is that a props-less comment still creates and carries no props.
	if len(added.Comment.Props) != 0 {
		t.Fatalf("want empty bag, got %#v", added.Comment.Props)
	}

	// And it is still queryable: an empty `where` matches it.
	var out queryCommentsOut
	mcpCallJSON(t, ctx, sess, "query_comments", map[string]any{"page_id": page}, &out)
	if len(out.Comments) != 1 {
		t.Fatalf("props-less comment should still be queryable, got %d", len(out.Comments))
	}
}
