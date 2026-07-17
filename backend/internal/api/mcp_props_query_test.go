package api

import (
	"context"
	"database/sql"
	"encoding/json"
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

	// Renamed from "non-string values round-trip verbatim". That name certified
	// every non-string value; the body checks one bool. The gap it left is exactly
	// where set_prop's real bug lived for a day — so the name now says what the
	// body actually reaches.
	t.Run("the server stores a bool verbatim", func(t *testing.T) {
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

	// SCOPE, stated up front because this test's ancestor lied about its own:
	// this proves the SERVER stores an array verbatim and leaves it queryable. It
	// does NOT — and cannot — catch the bug that actually bit prod.
	//
	// That bug: set_prop's schema left `value` untyped, so a real MCP client had
	// to guess how to encode it and guessed string, every time, for every
	// non-string value. This test hands the SDK a REAL Go []any. The real client
	// never sends one — that IS the bug — so this test would stay green forever
	// while prod filled with "[\"tela\",\"agents\"]".
	//
	// Naming it "array values round-trip" would repeat the exact sin of the bool
	// subtest above ("non-string values round-trip verbatim", body: one bool):
	// a name certifying a claim the body cannot reach. The claim this body CAN
	// reach is "the server does not coerce", so that is what it is called.
	//
	// The client coercion is unreachable from here — the client lives outside this
	// repo. What guards it is TestMCP_SetPropSchemaDeclaresValueType below, which
	// pins the typed schema that stops the guessing in the first place.
	t.Run("the server stores an array verbatim and leaves it queryable", func(t *testing.T) {
		var out setPropOut
		mcpCallJSON(t, ctx, sess, "set_prop",
			map[string]any{"page_id": pageID, "key": "tags", "value": []any{"tela", "agents"}}, &out)

		got, ok := out.Props["tags"].([]any)
		if !ok {
			t.Fatalf("tags = %#v (%T), want []any — a string here is the coercion bug: "+
				"props @> {\"tags\":[…]} can never match it", out.Props["tags"], out.Props["tags"])
		}
		if len(got) != 2 || got[0] != "tela" || got[1] != "agents" {
			t.Fatalf("tags = %#v, want [tela agents]", got)
		}

		// THE POINT. Round-trip through the reader that actually matters.
		var q queryPagesOut
		mcpCallJSON(t, ctx, sess, "query_pages",
			map[string]any{"where": map[string]any{"tags": []any{"tela"}}}, &q)
		var found bool
		for _, p := range q.Pages {
			if p.ID == pageID {
				found = true
			}
		}
		if !found {
			t.Fatalf("containment query {\"tags\":[\"tela\"]} did not find page %d — "+
				"the value was written but is unqueryable, which is silent data loss "+
				"to every dashboard built on containment", pageID)
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

// The guard for the bug that actually bit prod — and the only one this repo can
// honestly offer.
//
// set_prop published `value` with a description and NO `type`. A schema like that
// looks like a contract and is structurally incapable of constraining anything:
// the type rule lived in prose, and a client cannot validate or serialize against
// prose. So the client guessed, and guessed string — ["tela","agents"] landed as
// "[\"tela\",\"agents\"]", 42 as "42", true as "true". Silently unqueryable: no
// containment match, no numeric compare, no sort, no error.
//
// No test in this repo can catch that directly. The coercion happens inside a
// client outside this codebase; nothing here can make it guess on demand, and a
// server-side round-trip test hands the SDK a real Go value and so tests the one
// path that was never broken. The remedy is not a test that watches the schema
// lie — it is a schema that cannot lie. This pins that schema.
func TestMCP_SetPropSchemaDeclaresValueType(t *testing.T) {
	ts, d := newWiredServer(t)
	owner := seedUser(t, d, "schemaowner", "ownerpw12", false)
	seedSpace(t, d, "S", "s-space", owner)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	sess := mcpSession(t, ctx, ts, seedReadKey(t, d, owner, auth.ScopeWrite))

	tools, err := sess.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	var raw []byte
	for _, tool := range tools.Tools {
		if tool.Name == "set_prop" {
			if raw, err = json.Marshal(tool.InputSchema); err != nil {
				t.Fatalf("marshal input schema: %v", err)
			}
		}
	}
	if raw == nil {
		t.Fatal("set_prop not advertised in tools/list")
	}

	// Assert on the PUBLISHED wire JSON, not the Go value — the wire is what a
	// client actually reads, and it is where the type went missing.
	var schema struct {
		Properties map[string]struct {
			Type any `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("decode schema %s: %v", raw, err)
	}
	val, ok := schema.Properties["value"]
	if !ok {
		t.Fatalf("set_prop schema has no `value` property: %s", raw)
	}
	if val.Type == nil {
		t.Fatalf("set_prop `value` publishes NO type — this is the bug: a client "+
			"cannot serialize against a prose description and will guess string, "+
			"silently making every non-string prop unqueryable. Schema: %s", raw)
	}

	// The union must actually admit the non-string types the description promises.
	// A lone "string" would be worse than nothing: honest, but a lie about intent.
	types := map[string]bool{}
	switch v := val.Type.(type) {
	case string:
		types[v] = true
	case []any:
		for _, e := range v {
			if s, ok := e.(string); ok {
				types[s] = true
			}
		}
	}
	for _, want := range []string{"string", "number", "boolean", "object", "array", "null"} {
		if !types[want] {
			t.Errorf("set_prop `value` type union is missing %q (got %v) — the tool "+
				"documents it as accepted, so the schema must say so where a machine reads it",
				want, val.Type)
		}
	}
}
