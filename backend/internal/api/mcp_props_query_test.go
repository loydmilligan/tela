package api

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zcag/tela/backend/internal/auth"
)

// seedPropsPage inserts a page carrying a props bag (JSONB) and returns its id.
func seedPropsPage(t *testing.T, d *sql.DB, spaceID int64, title, propsJSON string) int64 {
	t.Helper()
	var id int64
	if err := d.QueryRowContext(context.Background(),
		`INSERT INTO pages (space_id, parent_id, title, body, position, props)
		 VALUES ($1, NULL, $2, 'b', 0, $3::jsonb) RETURNING id`,
		spaceID, title, propsJSON).Scan(&id); err != nil {
		t.Fatalf("insert page %s: %v", title, err)
	}
	return id
}

// The headline: set_prop is the agent front door for a SINGLE prop. It must
// merge — the tool description tells other agents it won't clobber their
// sibling keys, and that promise is what steers them off update_page, so it is
// pinned here. Also covers the editor gate and the key-scope gate.
func TestMCP_SetProp(t *testing.T) {
	ts, d := newWiredServer(t)
	owner := seedUser(t, d, "spowner", "ownerpw12", false)
	viewer := seedUser(t, d, "spviewer", "viewerpw12", false)
	space := seedSpace(t, d, "Props", "props-space", owner)
	seedMember(t, d, space, viewer, "viewer")

	pageID := seedPropsPage(t, d, space, "Runbook", `{"status":"open","owner":"alice"}`)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	sess := mcpSession(t, ctx, ts, seedReadKey(t, d, owner, auth.ScopeWrite))

	t.Run("merges one key and leaves the siblings intact", func(t *testing.T) {
		var out setPropOut
		mcpCallJSON(t, ctx, sess, "set_prop",
			map[string]any{"page_id": pageID, "key": "status", "value": "closed"}, &out)

		if out.Props["status"] != "closed" {
			t.Errorf("status = %v, want closed", out.Props["status"])
		}
		// The whole point: a key we never mentioned survived the write.
		if out.Props["owner"] != "alice" {
			t.Errorf("owner = %v, want alice (set_prop must not clobber siblings)", out.Props["owner"])
		}
	})

	t.Run("non-string values round-trip verbatim", func(t *testing.T) {
		var out setPropOut
		mcpCallJSON(t, ctx, sess, "set_prop",
			map[string]any{"page_id": pageID, "key": "done", "value": true}, &out)
		if out.Props["done"] != true {
			t.Errorf("done = %#v, want bool true", out.Props["done"])
		}
		if out.Props["owner"] != "alice" || out.Props["status"] != "closed" {
			t.Errorf("earlier keys lost: %#v", out.Props)
		}
	})

	// The hazard set_prop exists to avoid, pinned as behavior: update_page's
	// props= REPLACES the bag. If this ever starts merging, set_prop's "prefer
	// this over update_page" description would be misleading and should change.
	t.Run("update_page by contrast replaces the whole bag", func(t *testing.T) {
		var gp getPageOut
		mcpCallJSON(t, ctx, sess, "update_page",
			map[string]any{"id": pageID, "props": map[string]any{"status": "reopened"}}, &gp)
		if gp.Page.Props["status"] != "reopened" {
			t.Fatalf("status = %v, want reopened", gp.Page.Props["status"])
		}
		if _, ok := gp.Page.Props["owner"]; ok {
			t.Fatalf("update_page kept 'owner' — it is documented to replace the bag: %#v", gp.Page.Props)
		}
		// Restore the bag for any later subtest.
		var out setPropOut
		mcpCallJSON(t, ctx, sess, "set_prop",
			map[string]any{"page_id": pageID, "key": "owner", "value": "alice"}, &out)
	})

	t.Run("a viewer is refused and the value is unchanged", func(t *testing.T) {
		vsess := mcpSession(t, ctx, ts, seedReadKey(t, d, viewer, auth.ScopeWrite))
		res, err := vsess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "set_prop",
			Arguments: map[string]any{"page_id": pageID, "key": "status", "value": "hacked"},
		})
		if err != nil {
			t.Fatalf("call set_prop: %v", err)
		}
		if !res.IsError {
			t.Fatalf("viewer set_prop succeeded; want a forbidden tool error")
		}
		var stored string
		if err := d.QueryRowContext(ctx,
			`SELECT props->>'status' FROM pages WHERE id = $1`, pageID).Scan(&stored); err != nil {
			t.Fatalf("read back: %v", err)
		}
		if stored == "hacked" {
			t.Fatalf("viewer write landed despite the tool error")
		}
	})

	t.Run("a read-scope key cannot call the write tool", func(t *testing.T) {
		rsess := mcpSession(t, ctx, ts, seedReadKey(t, d, owner, auth.ScopeRead))
		res, err := rsess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "set_prop",
			Arguments: map[string]any{"page_id": pageID, "key": "status", "value": "nope"},
		})
		if err != nil {
			t.Fatalf("call set_prop: %v", err)
		}
		if !res.IsError || !strings.Contains(mcpErrText(res), `"code":"api_key_scope"`) {
			t.Fatalf("read-scope set_prop: want api_key_scope error, got IsError=%v %s", res.IsError, mcpErrText(res))
		}
	})

	t.Run("a reserved key is rejected", func(t *testing.T) {
		res, err := sess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "set_prop",
			Arguments: map[string]any{"page_id": pageID, "key": "title", "value": "pwned"},
		})
		if err != nil {
			t.Fatalf("call set_prop: %v", err)
		}
		if !res.IsError {
			t.Fatalf("reserved key accepted; want a bad_request tool error")
		}
	})
}

// The headline: query_pages filters by props containment and can never return a
// page from a space the caller cannot read (the core's space_access join).
func TestMCP_QueryPages(t *testing.T) {
	ts, d := newWiredServer(t)
	owner := seedUser(t, d, "qpowner", "ownerpw12", false)
	outsider := seedUser(t, d, "qpoutsider", "outsiderpw12", false)

	spaceA := seedSpace(t, d, "Alpha", "alpha-q", owner)
	spaceB := seedSpace(t, d, "Bravo", "bravo-q", owner) // owner-only; outsider never a member

	seedPropsPage(t, d, spaceA, "Incident One", `{"type":"incident","status":"active"}`)
	seedPropsPage(t, d, spaceA, "Incident Two", `{"type":"incident","status":"resolved"}`)
	seedPropsPage(t, d, spaceA, "A Doc", `{"type":"doc"}`)
	seedPropsPage(t, d, spaceB, "Secret Incident", `{"type":"incident","status":"active"}`)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	sess := mcpSession(t, ctx, ts, seedReadKey(t, d, owner, auth.ScopeRead))

	titles := func(out queryPagesOut) map[string]bool {
		m := map[string]bool{}
		for _, r := range out.Pages {
			m[r.Title] = true
		}
		return m
	}

	t.Run("containment matches props @> where", func(t *testing.T) {
		var out queryPagesOut
		mcpCallJSON(t, ctx, sess, "query_pages",
			map[string]any{"where": map[string]any{"type": "incident"}}, &out)
		got := titles(out)
		for _, want := range []string{"Incident One", "Incident Two", "Secret Incident"} {
			if !got[want] {
				t.Errorf("missing %q; got %v", want, got)
			}
		}
		if got["A Doc"] {
			t.Errorf("type=incident matched the doc: %v", got)
		}
	})

	t.Run("multi-key containment narrows", func(t *testing.T) {
		var out queryPagesOut
		mcpCallJSON(t, ctx, sess, "query_pages",
			map[string]any{"where": map[string]any{"type": "incident", "status": "active"}}, &out)
		got := titles(out)
		if !got["Incident One"] || got["Incident Two"] {
			t.Errorf("want only active incidents; got %v", got)
		}
	})

	// space_id must reach the core as a real filter — the core's space resolver
	// type-switches on the value, so a native Go int has to be understood.
	t.Run("space_id scopes to one space", func(t *testing.T) {
		var out queryPagesOut
		mcpCallJSON(t, ctx, sess, "query_pages",
			map[string]any{"where": map[string]any{"type": "incident"}, "space_id": spaceA}, &out)
		got := titles(out)
		if !got["Incident One"] || !got["Incident Two"] {
			t.Errorf("space A incidents missing: %v", got)
		}
		if got["Secret Incident"] {
			t.Errorf("space_id=A leaked a space B page: %v", got)
		}
	})

	t.Run("a non-member gets no rows from a private space", func(t *testing.T) {
		osess := mcpSession(t, ctx, ts, seedReadKey(t, d, outsider, auth.ScopeRead))
		var out queryPagesOut
		mcpCallJSON(t, ctx, osess, "query_pages",
			map[string]any{"where": map[string]any{"type": "incident"}}, &out)
		if len(out.Pages) != 0 {
			t.Fatalf("outsider (member of nothing) got rows: %v", titles(out))
		}

		// Once granted read on A only, A's incidents appear and B's still never do.
		seedMember(t, d, spaceA, outsider, "viewer")
		var out2 queryPagesOut
		mcpCallJSON(t, ctx, osess, "query_pages",
			map[string]any{"where": map[string]any{"type": "incident"}}, &out2)
		got := titles(out2)
		if !got["Incident One"] || !got["Incident Two"] {
			t.Errorf("A viewer should see A's incidents; got %v", got)
		}
		if got["Secret Incident"] {
			t.Fatalf("space_access gate leaked a space B page to a non-member: %v", got)
		}
	})

	t.Run("an unsupported sort key is rejected", func(t *testing.T) {
		res, err := sess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "query_pages",
			Arguments: map[string]any{"sort": "bogus"},
		})
		if err != nil {
			t.Fatalf("call query_pages: %v", err)
		}
		if !res.IsError {
			t.Fatalf("bogus sort accepted; want a bad_request tool error")
		}
	})
}
