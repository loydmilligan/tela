package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zcag/tela/backend/internal/auth"
)

// bearerRoundTripper injects a tela PAT on every request the MCP client makes,
// so the Streamable-HTTP transport authenticates against /api/mcp.
type bearerRoundTripper struct {
	token string
	base  http.RoundTripper
}

func (b bearerRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(r)
}

// TestMCP_SpikeListSpaces is the Phase-0 spike: it drives the real MCP Go SDK
// client over Streamable HTTP against the wired backend, authenticates with a
// tela PAT, and asserts the list_spaces tool returns the caller's spaces as
// structured output. This proves transport + bearer-verifier + identity
// threading + typed output end-to-end.
func TestMCP_SpikeListSpaces(t *testing.T) {
	ts, d := newWiredServer(t)

	alice := seedUser(t, d, "alice", "alicepw12", false)
	bob := seedUser(t, d, "bob", "bobpw1234", false)
	seedSpace(t, d, "Alice Space", "alice-space", alice)
	seedSpace(t, d, "Bob Space", "bob-space", bob) // not alice's — must not leak

	// Mint an unrestricted read PAT for alice.
	raw, prefix, _, err := auth.NewAPIKey(auth.LoadAPIKeySecret())
	if err != nil {
		t.Fatalf("new api key: %v", err)
	}
	hmacHex := auth.HMACAPIKey(auth.LoadAPIKeySecret(), raw)
	if _, err := d.ExecContext(context.Background(), `
		INSERT INTO api_keys (user_id, name, key_prefix, key_hmac, scope, space_id)
		VALUES ($1, 'mcp', $2, $3, $4, NULL)`,
		alice, prefix, hmacHex, auth.ScopeRead); err != nil {
		t.Fatalf("seed key: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	transport := &mcp.StreamableClientTransport{
		Endpoint: ts.URL + "/api/mcp",
		HTTPClient: &http.Client{
			Transport: bearerRoundTripper{token: raw, base: http.DefaultTransport},
		},
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "spike-test", Version: "0"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	// tools/list advertises list_spaces with an output schema.
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	var found *mcp.Tool
	for _, tl := range tools.Tools {
		if tl.Name == "list_spaces" {
			found = tl
		}
	}
	if found == nil {
		t.Fatalf("list_spaces tool not advertised; got %d tools", len(tools.Tools))
	}
	if found.OutputSchema == nil {
		t.Errorf("list_spaces has no output schema")
	}
	// Every tool needs a Title (Claude directory eligibility) and annotations.
	// openWorldHint MUST be set explicitly (the SDK omitempty default is "true",
	// i.e. open-world — wrong for tela's closed DB). Only import_mira is open.
	// Both directories reject hints that don't match behavior, so guard them.
	openWorld := map[string]bool{"import_mira": true}
	for _, tl := range tools.Tools {
		if tl.Title == "" {
			t.Errorf("tool %q has no Title", tl.Name)
		}
		if tl.Annotations == nil {
			t.Errorf("tool %q has no Annotations", tl.Name)
			continue
		}
		if tl.Annotations.OpenWorldHint == nil {
			t.Errorf("tool %q: OpenWorldHint unset (defaults to open-world)", tl.Name)
		} else if *tl.Annotations.OpenWorldHint != openWorld[tl.Name] {
			t.Errorf("tool %q: OpenWorldHint=%v, want %v", tl.Name, *tl.Annotations.OpenWorldHint, openWorld[tl.Name])
		}
	}
	// Drift guard: the full expected tool roster is advertised (catches an
	// accidentally-dropped or renamed tool).
	got := map[string]bool{}
	for _, tl := range tools.Tools {
		got[tl.Name] = true
	}
	for _, want := range []string{
		"list_spaces", "get_space", "list_pages", "get_page", "list_backlinks",
		"search", "search_bodies", "semantic_search", "read_chunk", "fetch",
		"create_page", "update_page", "delete_page", "move_page", "add_comment",
		"create_space", "update_space", "delete_space", "import_mira", "submit_feedback",
	} {
		if !got[want] {
			t.Errorf("tool %q not advertised", want)
		}
	}

	// tools/call returns alice's space (and only hers) as structured output.
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_spaces"})
	if err != nil {
		t.Fatalf("call list_spaces: %v", err)
	}
	if res.IsError {
		t.Fatalf("list_spaces returned tool error: %v", res.Content)
	}

	var out listSpacesOut
	raw2, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw2, &out); err != nil {
		t.Fatalf("decode structured content %s: %v", raw2, err)
	}
	if len(out.Spaces) != 1 {
		t.Fatalf("want exactly alice's 1 space, got %d: %+v", len(out.Spaces), out.Spaces)
	}
	if out.Spaces[0].Name != "Alice Space" || out.Spaces[0].Slug != "alice-space" {
		t.Errorf("unexpected space: %+v", out.Spaces[0])
	}
	_ = bob
}

// mcpSession opens an authenticated MCP client session against ts using token.
func mcpSession(t *testing.T, ctx context.Context, ts *httptest.Server, token string) *mcp.ClientSession {
	t.Helper()
	transport := &mcp.StreamableClientTransport{
		Endpoint: ts.URL + "/api/mcp",
		HTTPClient: &http.Client{
			Transport: bearerRoundTripper{token: token, base: http.DefaultTransport},
		},
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func seedReadKey(t *testing.T, d *sql.DB, uid int64, scope string) string {
	t.Helper()
	raw, prefix, _, err := auth.NewAPIKey(auth.LoadAPIKeySecret())
	if err != nil {
		t.Fatalf("new api key: %v", err)
	}
	hmacHex := auth.HMACAPIKey(auth.LoadAPIKeySecret(), raw)
	if _, err := d.ExecContext(context.Background(), `
		INSERT INTO api_keys (user_id, name, key_prefix, key_hmac, scope, space_id)
		VALUES ($1, 'mcp', $2, $3, $4, NULL)`, uid, prefix, hmacHex, scope); err != nil {
		t.Fatalf("seed key: %v", err)
	}
	return raw
}

func mcpCallJSON(t *testing.T, ctx context.Context, sess *mcp.ClientSession, name string, args map[string]any, out any) {
	t.Helper()
	res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("%s returned tool error: %v", name, res.Content)
	}
	raw, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("decode %s output %s: %v", name, raw, err)
	}
}

// TestMCP_ReadTools exercises the Phase-1 read surface end-to-end over the MCP
// transport: list_pages, get_page, search, search_bodies, list_backlinks. It
// asserts results are space-scoped and that the typed structured output decodes.
func TestMCP_ReadTools(t *testing.T) {
	ts, d := newWiredServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)
	space := seedSpace(t, d, "Docs", "docs", alice)
	other := seedUser(t, d, "bob", "bobpw1234", false)
	otherSpace := seedSpace(t, d, "Bob", "bob", other)

	// alice's pages: a parent "Alpha" with body, and a child "Beta" linking to it.
	var alphaID int64
	if err := d.QueryRowContext(context.Background(),
		`INSERT INTO pages (space_id, parent_id, title, body, position) VALUES ($1, NULL, 'Alpha', 'the quick brown fox', 0) RETURNING id`,
		space).Scan(&alphaID); err != nil {
		t.Fatalf("insert alpha: %v", err)
	}
	betaBody := "see [Alpha](tela://page/" + strconv.FormatInt(alphaID, 10) + ") for context"
	var betaID int64
	if err := d.QueryRowContext(context.Background(),
		`INSERT INTO pages (space_id, parent_id, title, body, position) VALUES ($1, $2, 'Beta', $3, 0) RETURNING id`,
		space, alphaID, betaBody).Scan(&betaID); err != nil {
		t.Fatalf("insert beta: %v", err)
	}
	// page_links row so backlinks resolve (mirrors syncPageLinks on save).
	if _, err := d.ExecContext(context.Background(),
		`INSERT INTO page_links (source_id, target_id, last_known_title) VALUES ($1, $2, 'Alpha')`,
		betaID, alphaID); err != nil {
		t.Fatalf("insert page_link: %v", err)
	}
	_ = otherSpace

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	sess := mcpSession(t, ctx, ts, seedReadKey(t, d, alice, auth.ScopeRead))

	// list_pages top-level → just Alpha.
	var lp listPagesOut
	mcpCallJSON(t, ctx, sess, "list_pages", map[string]any{"space_id": space}, &lp)
	if len(lp.Pages) != 1 || lp.Pages[0].Title != "Alpha" {
		t.Fatalf("list_pages top-level: %+v", lp.Pages)
	}
	if lp.Pages[0].URL == "" {
		t.Errorf("list_pages item missing url")
	}

	// list_pages with parent_id → just Beta.
	var lpc listPagesOut
	mcpCallJSON(t, ctx, sess, "list_pages", map[string]any{"space_id": space, "parent_id": alphaID}, &lpc)
	if len(lpc.Pages) != 1 || lpc.Pages[0].Title != "Beta" {
		t.Fatalf("list_pages child: %+v", lpc.Pages)
	}

	// get_page → full body + url.
	var gp getPageOut
	mcpCallJSON(t, ctx, sess, "get_page", map[string]any{"id": alphaID}, &gp)
	if gp.Page.Body != "the quick brown fox" || gp.Page.URL == "" {
		t.Fatalf("get_page: %+v", gp.Page)
	}

	// search → Alpha matches "fox".
	var sr searchOut
	mcpCallJSON(t, ctx, sess, "search", map[string]any{"query": "fox"}, &sr)
	if len(sr.Results) != 1 || sr.Results[0].PageID != alphaID {
		t.Fatalf("search fox: %+v", sr.Results)
	}
	// ChatGPT Deep-Research aliases present on each hit (id=string page id, text=snippet, url).
	if sr.Results[0].ID != strconv.FormatInt(alphaID, 10) || sr.Results[0].Text == "" || sr.Results[0].URL == "" {
		t.Fatalf("search hit missing ChatGPT id/text/url: %+v", sr.Results[0])
	}

	// fetch (Deep Research) → full page text by the search result's id.
	var fo fetchOut
	mcpCallJSON(t, ctx, sess, "fetch", map[string]any{"id": sr.Results[0].ID}, &fo)
	if fo.Text != "the quick brown fox" || fo.Title != "Alpha" || fo.URL == "" {
		t.Fatalf("fetch: %+v", fo)
	}

	// search_bodies within the space.
	var sbr searchBodiesOut
	mcpCallJSON(t, ctx, sess, "search_bodies", map[string]any{"query": "fox", "space_id": space}, &sbr)
	if len(sbr.Results) != 1 || sbr.Results[0].ID != alphaID {
		t.Fatalf("search_bodies: %+v", sbr.Results)
	}

	// list_backlinks → Beta links to Alpha.
	var bl listBacklinksOut
	mcpCallJSON(t, ctx, sess, "list_backlinks", map[string]any{"page_id": alphaID}, &bl)
	if len(bl.Backlinks) != 1 || bl.Backlinks[0].PageID != betaID {
		t.Fatalf("list_backlinks: %+v", bl.Backlinks)
	}
}

// TestMCP_ReadToolCrossSpaceDenied asserts a read key cannot reach a space the
// user isn't a member of (get_page collapses to the 403 "not a member").
func TestMCP_ReadToolCrossSpaceDenied(t *testing.T) {
	ts, d := newWiredServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)
	seedSpace(t, d, "Alice", "alice", alice)
	bob := seedUser(t, d, "bob", "bobpw1234", false)
	bobSpace := seedSpace(t, d, "Bob", "bob", bob)
	var bobPage int64
	if err := d.QueryRowContext(context.Background(),
		`INSERT INTO pages (space_id, parent_id, title, body, position) VALUES ($1, NULL, 'Secret', 'x', 0) RETURNING id`,
		bobSpace).Scan(&bobPage); err != nil {
		t.Fatalf("insert: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	sess := mcpSession(t, ctx, ts, seedReadKey(t, d, alice, auth.ScopeRead))

	res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: "get_page", Arguments: map[string]any{"id": bobPage}})
	if err != nil {
		t.Fatalf("call get_page: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected get_page on another user's space to be a tool error")
	}
}

// TestMCP_WriteTools exercises the Phase-1 write surface end-to-end: create_space,
// create_page, update_page, add_comment, delete_page, delete_space, plus the
// read-scope rejection (mcpRequireWrite) and submit_feedback's any-scope allowance.
func TestMCP_WriteTools(t *testing.T) {
	ts, d := newWiredServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	sess := mcpSession(t, ctx, ts, seedReadKey(t, d, alice, auth.ScopeWrite))

	// create_space → alice owns it.
	var sp spaceOut
	mcpCallJSON(t, ctx, sess, "create_space", map[string]any{"name": "Engineering"}, &sp)
	if sp.Space.ID == 0 || sp.Space.Slug != "engineering" {
		t.Fatalf("create_space: %+v", sp.Space)
	}
	spaceID := sp.Space.ID

	// create_page in it.
	var pg getPageOut
	mcpCallJSON(t, ctx, sess, "create_page", map[string]any{
		"space_id": spaceID, "title": "Runbook", "body": "step one",
	}, &pg)
	if pg.Page.Title != "Runbook" || pg.Page.URL == "" {
		t.Fatalf("create_page: %+v", pg.Page)
	}
	pageID := pg.Page.ID

	// update_page body → snapshot + new body.
	var up getPageOut
	mcpCallJSON(t, ctx, sess, "update_page", map[string]any{"id": pageID, "body": "step one\nstep two"}, &up)
	if up.Page.Body != "step one\nstep two" {
		t.Fatalf("update_page: %+v", up.Page)
	}

	// create a second page, then move_page it under the first (reparent).
	var pg2 getPageOut
	mcpCallJSON(t, ctx, sess, "create_page", map[string]any{"space_id": spaceID, "title": "Child", "body": "c"}, &pg2)
	var mv getPageOut
	mcpCallJSON(t, ctx, sess, "move_page", map[string]any{"id": pg2.Page.ID, "parent_id": pageID}, &mv)
	if mv.Page.ParentID == nil || *mv.Page.ParentID != pageID {
		t.Fatalf("move_page reparent: %+v", mv.Page)
	}
	// move_page detach back to root.
	var mv2 getPageOut
	mcpCallJSON(t, ctx, sess, "move_page", map[string]any{"id": pg2.Page.ID, "make_root": true}, &mv2)
	if mv2.Page.ParentID != nil {
		t.Fatalf("move_page make_root: %+v", mv2.Page)
	}
	// parent_id + make_root together is rejected.
	if res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: "move_page", Arguments: map[string]any{
		"id": pg2.Page.ID, "parent_id": pageID, "make_root": true,
	}}); err != nil {
		t.Fatalf("move_page conflict call: %v", err)
	} else if !res.IsError {
		t.Fatalf("expected parent_id+make_root to be rejected")
	}

	// add_comment (root, anchored).
	var cm addCommentOut
	mcpCallJSON(t, ctx, sess, "add_comment", map[string]any{
		"page_id": pageID,
		"anchor":  map[string]any{"prefix": "step ", "exact": "one", "suffix": "\nstep"},
		"body":    "is this still accurate?",
	}, &cm)
	if cm.Comment.ID == 0 || cm.Comment.Body != "is this still accurate?" {
		t.Fatalf("add_comment: %+v", cm.Comment)
	}

	// submit_feedback works on a write key too.
	var fb submitFeedbackOut
	mcpCallJSON(t, ctx, sess, "submit_feedback", map[string]any{"subject": "nice", "body": "the mcp is great"}, &fb)
	if fb.Feedback.ID == 0 {
		t.Fatalf("submit_feedback: %+v", fb.Feedback)
	}

	// import_mira (inline payload) → creates a page from mira block JSON.
	var im getPageOut
	mcpCallJSON(t, ctx, sess, "import_mira", map[string]any{
		"space_id": spaceID,
		"payload": map[string]any{
			"template": "page",
			"blocks": []any{map[string]any{
				"type": "heading_1",
				"heading_1": map[string]any{"rich_text": []any{map[string]any{
					"type": "text", "text": map[string]any{"content": "Imported"},
				}}},
			}},
		},
	}, &im)
	if im.Page.ID == 0 || im.Page.Title == "" {
		t.Fatalf("import_mira: %+v", im.Page)
	}

	// delete_page → ok.
	var del okOut
	mcpCallJSON(t, ctx, sess, "delete_page", map[string]any{"id": pageID}, &del)
	if !del.OK {
		t.Fatalf("delete_page: %+v", del)
	}

	// delete_space → ok.
	var dels okOut
	mcpCallJSON(t, ctx, sess, "delete_space", map[string]any{"id": spaceID}, &dels)
	if !dels.OK {
		t.Fatalf("delete_space: %+v", dels)
	}
}

// TestMCP_WriteToolReadKeyDenied asserts a read-scope key is refused at a write
// tool (mcpRequireWrite → api_key_scope) but allowed at submit_feedback.
func TestMCP_WriteToolReadKeyDenied(t *testing.T) {
	ts, d := newWiredServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)
	space := seedSpace(t, d, "Docs", "docs", alice)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	sess := mcpSession(t, ctx, ts, seedReadKey(t, d, alice, auth.ScopeRead))

	res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: "create_page", Arguments: map[string]any{
		"space_id": space, "title": "X", "body": "y",
	}})
	if err != nil {
		t.Fatalf("call create_page: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected read key to be denied at create_page")
	}

	// submit_feedback is the read-scope carve-out → must succeed.
	var fb submitFeedbackOut
	mcpCallJSON(t, ctx, sess, "submit_feedback", map[string]any{"subject": "s", "body": "b"}, &fb)
	if fb.Feedback.ID == 0 {
		t.Fatalf("submit_feedback under read key should succeed: %+v", fb.Feedback)
	}
}

// TestMCP_Resources exercises Phase 2: the tela://page/{id} resource template
// (list + read, with cross-space denial) and resource links in tool results.
func TestMCP_Resources(t *testing.T) {
	ts, d := newWiredServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)
	space := seedSpace(t, d, "Docs", "docs", alice)
	bob := seedUser(t, d, "bob", "bobpw1234", false)
	bobSpace := seedSpace(t, d, "Bob", "bob", bob)

	var pageID int64
	if err := d.QueryRowContext(context.Background(),
		`INSERT INTO pages (space_id, parent_id, title, body, position) VALUES ($1, NULL, 'Alpha', 'hello world', 0) RETURNING id`,
		space).Scan(&pageID); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var bobPage int64
	if err := d.QueryRowContext(context.Background(),
		`INSERT INTO pages (space_id, parent_id, title, body, position) VALUES ($1, NULL, 'Secret', 'x', 0) RETURNING id`,
		bobSpace).Scan(&bobPage); err != nil {
		t.Fatalf("insert: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	sess := mcpSession(t, ctx, ts, seedReadKey(t, d, alice, auth.ScopeRead))

	// The page template is advertised.
	tmpls, err := sess.ListResourceTemplates(ctx, nil)
	if err != nil {
		t.Fatalf("list templates: %v", err)
	}
	var hasPage bool
	for _, tm := range tmpls.ResourceTemplates {
		if tm.URITemplate == "tela://page/{id}" {
			hasPage = true
		}
	}
	if !hasPage {
		t.Fatalf("tela://page/{id} template not advertised: %+v", tmpls.ResourceTemplates)
	}

	// Read alice's page → markdown with title + body.
	rr, err := sess.ReadResource(ctx, &mcp.ReadResourceParams{URI: "tela://page/" + strconv.FormatInt(pageID, 10)})
	if err != nil {
		t.Fatalf("read resource: %v", err)
	}
	if len(rr.Contents) != 1 || rr.Contents[0].Text != "# Alpha\n\nhello world" {
		t.Fatalf("resource contents: %+v", rr.Contents)
	}

	// Reading bob's page → not found (membership-gated, collapsed).
	if _, err := sess.ReadResource(ctx, &mcp.ReadResourceParams{URI: "tela://page/" + strconv.FormatInt(bobPage, 10)}); err == nil {
		t.Fatalf("expected cross-space resource read to fail")
	}

	// get_page tool result carries no ResourceLink content blocks — hosts (Claude)
	// surface them as "Resource links are not currently supported" noise, and the
	// data is already in structuredContent. The tela://page/{id} resource template
	// (asserted above) remains the click-through / @-mention path.
	res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: "get_page", Arguments: map[string]any{"id": pageID}})
	if err != nil {
		t.Fatalf("get_page: %v", err)
	}
	for _, c := range res.Content {
		if _, ok := c.(*mcp.ResourceLink); ok {
			t.Fatalf("get_page result should not carry ResourceLink blocks: %+v", res.Content)
		}
	}

	// get_space tool → metadata for the space.
	var gs spaceOut
	mcpCallJSON(t, ctx, sess, "get_space", map[string]any{"id": space}, &gs)
	if gs.Space.ID != space || gs.Space.Slug != "docs" {
		t.Fatalf("get_space: %+v", gs.Space)
	}

	// tela://space/{id} resource → markdown index linking the page.
	sr, err := sess.ReadResource(ctx, &mcp.ReadResourceParams{URI: "tela://space/" + strconv.FormatInt(space, 10)})
	if err != nil {
		t.Fatalf("read space resource: %v", err)
	}
	wantLink := "[Alpha](tela://page/" + strconv.FormatInt(pageID, 10) + ")"
	if len(sr.Contents) != 1 || !strings.Contains(sr.Contents[0].Text, "# Docs") || !strings.Contains(sr.Contents[0].Text, wantLink) {
		t.Fatalf("space resource contents: %q", sr.Contents[0].Text)
	}

	// Cross-space space resource read → denied.
	if _, err := sess.ReadResource(ctx, &mcp.ReadResourceParams{URI: "tela://space/" + strconv.FormatInt(bobSpace, 10)}); err == nil {
		t.Fatalf("expected cross-space space-resource read to fail")
	}
}

// TestMCP_SpikeRejectsNoToken asserts the transport refuses an unauthenticated
// connection (the bearer verifier 401s with WWW-Authenticate).
func TestMCP_SpikeRejectsNoToken(t *testing.T) {
	ts, _ := newWiredServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	transport := &mcp.StreamableClientTransport{Endpoint: ts.URL + "/api/mcp"}
	client := mcp.NewClient(&mcp.Implementation{Name: "spike-test", Version: "0"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err == nil {
		session.Close()
		t.Fatalf("expected connect to fail without a token")
	}
}

// TestMCP_Widgets verifies the MCP Apps widget surface: the ui:// resources are
// advertised + serve HTML (both MIME variants) with the host bridge inlined (no
// external esm.sh import — that left a blank iframe in Claude), and the
// get_page/search tools carry the _meta that links them to their widget.
func TestMCP_Widgets(t *testing.T) {
	ts, d := newWiredServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	sess := mcpSession(t, ctx, ts, seedReadKey(t, d, alice, auth.ScopeRead))

	// All four widget resources advertised.
	res, err := sess.ListResources(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"ui://tela/page-reader/openai": false, "ui://tela/page-reader/mcp": false,
		"ui://tela/search-results/openai": false, "ui://tela/search-results/mcp": false,
	}
	for _, r := range res.Resources {
		if _, ok := want[r.URI]; ok {
			want[r.URI] = true
		}
	}
	for uri, found := range want {
		if !found {
			t.Errorf("widget resource %s not advertised", uri)
		}
	}

	// Reading a widget resource returns the HTML bundle with the right MIME and
	// the bridge inlined: the ChatGPT (window.openai) + MCP Apps (ui/initialize)
	// branches are present, the injection marker is consumed, and there's no
	// external esm.sh import (the cause of Claude's blank iframe).
	rr, err := sess.ReadResource(ctx, &mcp.ReadResourceParams{URI: "ui://tela/page-reader/openai"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rr.Contents) != 1 {
		t.Fatalf("widget html unexpected: %+v", rr.Contents)
	}
	html := rr.Contents[0].Text
	for _, must := range []string{"window.openai", "ui/initialize", "window.__telaWidget"} {
		if !strings.Contains(html, must) {
			t.Errorf("widget html missing %q (bridge not inlined?)", must)
		}
	}
	for _, mustNot := range []string{"esm.sh", "<!--TELA_BRIDGE-->"} {
		if strings.Contains(html, mustNot) {
			t.Errorf("widget html still contains %q", mustNot)
		}
	}
	if rr.Contents[0].MIMEType != "text/html+skybridge" {
		t.Errorf("widget mime: %q", rr.Contents[0].MIMEType)
	}

	// Tool→widget _meta is currently disabled (Claude's live iframe stays blank),
	// so neither get_page nor search should advertise an outputTemplate. The
	// resources above stay registered (bridge inlined) for when it's re-enabled.
	tools, err := sess.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range tools.Tools {
		if (tl.Name == "get_page" || tl.Name == "search") && tl.Meta["openai/outputTemplate"] != nil {
			t.Errorf("%s should not advertise a widget outputTemplate while disabled: %v", tl.Name, tl.Meta)
		}
	}
}
