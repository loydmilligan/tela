package api

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zcag/tela/backend/internal/models"
	"github.com/zcag/tela/backend/internal/rag"
)

// registerMCPTools wires the tela tool surface onto the MCP server. Each tool
// reads identity from the request (mcpIdentity), calls the shared xCore that
// also backs the REST route, and returns a typed Out so the SDK emits an output
// schema + structured content. Write tools additionally gate on key scope via
// mcpRequireWrite (the public-path mount defers method-scope to the tool).
func (s *Server) registerMCPTools(server *mcp.Server) {
	// Hints default to OPEN/DESTRUCTIVE when absent (MCP spec: OpenWorldHint and
	// DestructiveHint default true), so every tool sets them explicitly — both
	// directories reject tools whose advertised hints don't match behavior. tela
	// is a closed world (its own DB) except import_mira, which fetches external
	// URLs. *bool so the value survives the SDK's omitempty.
	no, yes := false, true
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &no}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_spaces",
		Title:       "List spaces",
		Description: "List every space the API key can access (id, name, slug).",
		Annotations: readOnly,
	}, s.mcpListSpaces)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_space",
		Title:       "Get space",
		Description: "Fetch a single space's metadata (id, name, slug) by id.",
		Annotations: readOnly,
	}, s.mcpGetSpace)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_pages",
		Title:       "List pages",
		Description: "Flat page listing in a space. Optional parent_id for direct children (omit for top-level pages).",
		Annotations: readOnly,
	}, s.mcpListPages)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_page",
		Title:       "Get page",
		Description: "Full markdown body + metadata for a numeric page id.",
		Annotations: readOnly,
		Meta:        widgetToolMeta(uiPageReaderOpenAI, uiPageReaderMCPApp, "Renders the page as formatted markdown.", "Opening page…", "Page ready"),
	}, s.mcpGetPage)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_backlinks",
		Title:       "List backlinks",
		Description: "Pages that link to the given page via [[wikilink]] / tela://page/{id}.",
		Annotations: readOnly,
	}, s.mcpListBacklinks)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search",
		Title:       "Search",
		Description: "Ranked full-text search over title + body, snippet-highlighted. Optional space_id narrows to one space.",
		Annotations: readOnly,
		Meta:        widgetToolMeta(uiSearchOpenAI, uiSearchMCPApp, "Renders search hits as a clickable result list.", "Searching…", "Results ready"),
	}, s.mcpSearch)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_bodies",
		Title:       "Search page bodies",
		Description: "Ranked full-text body search within one space (no snippets). Re-fetch full bodies via get_page.",
		Annotations: readOnly,
	}, s.mcpSearchBodies)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "semantic_search",
		Title:       "Semantic search",
		Description: "Meaning-aware chunk search (vector + keyword, RRF). Returns ranked chunks with chunk_id + citations (page id + heading path). Requires a configured embedder.",
		Annotations: readOnly,
	}, s.mcpSemanticSearch)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "read_chunk",
		Title:       "Read chunk",
		Description: "Fetch one chunk's full section text by chunk_id (from semantic_search). Middle granularity between a search snippet and get_page.",
		Annotations: readOnly,
	}, s.mcpReadChunk)

	// `search` (above) + `fetch` are the ChatGPT Deep Research / company-knowledge
	// compatibility pair (read-only, fixed names/shapes). `search` already returns
	// id/title/text/url per result; `fetch` returns a page's full text by that id.
	mcp.AddTool(server, &mcp.Tool{
		Name:        "fetch",
		Title:       "Fetch document",
		Description: "Fetch a tela page's full text by id (id comes from a search result). The ChatGPT Deep Research 'fetch' tool.",
		Annotations: readOnly,
	}, s.mcpFetch)

	// ---- write tools (gate on write/admin scope via mcpRequireWrite) ----
	// Writes are closed-world (OpenWorldHint:false) and additive (DestructiveHint:
	// false) unless noted; deletes set DestructiveHint:true, idempotent patches
	// set IdempotentHint:true.

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_page",
		Title:       "Create page",
		Description: "Create a page in a space (editor+). Body is markdown; tela://page/{id} links and [[Page Title]] wikilinks (resolved by title within the space) are indexed as backlinks.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, OpenWorldHint: &no},
	}, s.mcpCreatePage)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_page",
		Title:       "Update page",
		Description: "Patch a page's title and/or body (editor+). A body change auto-snapshots a revision.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: &no, OpenWorldHint: &no},
	}, s.mcpUpdatePage)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_page",
		Title:       "Delete page",
		Description: "Delete a page (editor+). Backlinks from other pages are preserved with the last-known title.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, OpenWorldHint: &no},
	}, s.mcpDeletePage)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "move_page",
		Title:       "Move page",
		Description: "Move a page: reparent (parent_id), detach to top-level (make_root), reorder (position), and/or relocate to another space (space_id). Editor+ in both source and target space. Provide at least one of space_id / parent_id / make_root / position.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: &no, OpenWorldHint: &no},
	}, s.mcpMovePage)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "add_comment",
		Title:       "Add comment",
		Description: "Attach a root comment to a page, anchored by a {prefix, exact, suffix} text triplet (editor+).",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, OpenWorldHint: &no},
	}, s.mcpAddComment)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_space",
		Title:       "Create space",
		Description: "Create a space. The caller becomes its owner. slug is derived from name when omitted.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, OpenWorldHint: &no},
	}, s.mcpCreateSpace)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_space",
		Title:       "Update space",
		Description: "Patch a space's name and/or slug (editor+).",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: &no, OpenWorldHint: &no},
	}, s.mcpUpdateSpace)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_space",
		Title:       "Delete space",
		Description: "Delete a space AND all its pages, comments, revisions and share links. Owner only. Irreversible.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, OpenWorldHint: &no},
	}, s.mcpDeleteSpace)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "import_mira",
		Title:       "Import mira page",
		Description: "Import a single mira page into a space as a new page (editor+). Provide source_url (https, allowlisted host, fetched server-side) OR an inline payload (raw mira block JSON) — exactly one. A password-protected source returns an unlock link.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, OpenWorldHint: &yes},
	}, s.mcpImportMira)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "submit_feedback",
		Title:       "Submit feedback",
		Description: "Submit free-text feedback about tela / tela-mcp itself (friction, bugs, missing capabilities). NOT for page content — use add_comment for that.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, OpenWorldHint: &no},
	}, s.mcpSubmitFeedback)
}

// ---- shared output shapes ------------------------------------------------

// mcpPage is a page row plus the human-shareable in-app URL. Embeds models.Page
// so the body + metadata fields are promoted verbatim.
type mcpPage struct {
	models.Page
	URL       string `json:"url"`
	Truncated bool   `json:"truncated,omitempty"`
}

// mcpBodyCap bounds a tool-result body so a single huge page can't blow the
// host's tool-result token budget (Claude Code truncates at ~25k tokens). At
// ~4 chars/token, 80k chars ≈ 20k tokens, leaving headroom for the envelope.
// On truncation it appends a pointer to the paging tools and returns ok=false.
const mcpBodyCap = 80_000

func mcpCapBody(body string) (string, bool) {
	if len(body) <= mcpBodyCap {
		return body, true
	}
	// Trim to the cap on a rune boundary, then add a machine- and human-readable
	// marker so the agent knows to page the rest via read_chunk/semantic_search.
	cut := mcpBodyCap
	for cut > 0 && !utf8.RuneStart(body[cut]) {
		cut--
	}
	return body[:cut] + "\n\n…[truncated: page exceeds the tool-result size cap. Use semantic_search + read_chunk to read specific sections, or open the page URL.]", false
}

func mcpPageURL(p models.Page) string {
	return publicBaseURL() + pageAppPath(p.SpaceID, p.ID, p.Title)
}

// ---- list_spaces ---------------------------------------------------------

type listSpacesIn struct{}

type mcpSpace struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type listSpacesOut struct {
	Spaces []mcpSpace `json:"spaces"`
}

func (s *Server) mcpListSpaces(ctx context.Context, req *mcp.CallToolRequest, _ listSpacesIn) (*mcp.CallToolResult, listSpacesOut, error) {
	u, _ := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), listSpacesOut{}, nil
	}
	spaces, ae := s.listSpacesCore(ctx, u)
	if ae != nil {
		return mcpErr(ae), listSpacesOut{}, nil
	}
	out := listSpacesOut{Spaces: make([]mcpSpace, len(spaces))}
	for i, sp := range spaces {
		out.Spaces[i] = mcpSpace{ID: sp.ID, Name: sp.Name, Slug: sp.Slug}
	}
	return nil, out, nil
}

// ---- get_space -----------------------------------------------------------

type getSpaceIn struct {
	ID int64 `json:"id" jsonschema:"space id"`
}

func (s *Server) mcpGetSpace(ctx context.Context, req *mcp.CallToolRequest, in getSpaceIn) (*mcp.CallToolResult, spaceOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), spaceOut{}, nil
	}
	sp, ae := s.getSpaceCore(ctx, u, k, in.ID)
	if ae != nil {
		return mcpErr(ae), spaceOut{}, nil
	}
	return nil, spaceOut{Space: sp}, nil
}

// ---- list_pages ----------------------------------------------------------

type listPagesIn struct {
	SpaceID  int64  `json:"space_id" jsonschema:"id of the space to list pages in"`
	ParentID *int64 `json:"parent_id,omitempty" jsonschema:"optional parent page id; omit for top-level pages"`
}

type mcpPageListItem struct {
	ID       int64  `json:"id"`
	SpaceID  int64  `json:"space_id"`
	ParentID *int64 `json:"parent_id"`
	Title    string `json:"title"`
	Position int64  `json:"position"`
	URL      string `json:"url"`
}

type listPagesOut struct {
	Pages []mcpPageListItem `json:"pages"`
}

func (s *Server) mcpListPages(ctx context.Context, req *mcp.CallToolRequest, in listPagesIn) (*mcp.CallToolResult, listPagesOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), listPagesOut{}, nil
	}
	pages, ae := s.listPagesCore(ctx, u, k, in.SpaceID, in.ParentID)
	if ae != nil {
		return mcpErr(ae), listPagesOut{}, nil
	}
	out := listPagesOut{Pages: make([]mcpPageListItem, len(pages))}
	for i, p := range pages {
		out.Pages[i] = mcpPageListItem{
			ID:       p.ID,
			SpaceID:  p.SpaceID,
			ParentID: p.ParentID,
			Title:    p.Title,
			Position: p.Position,
			URL:      mcpPageURL(p.Page),
		}
	}
	return nil, out, nil
}

// ---- get_page ------------------------------------------------------------

type getPageIn struct {
	ID int64 `json:"id" jsonschema:"numeric page id"`
}

type getPageOut struct {
	Page mcpPage `json:"page"`
}

func (s *Server) mcpGetPage(ctx context.Context, req *mcp.CallToolRequest, in getPageIn) (*mcp.CallToolResult, getPageOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), getPageOut{}, nil
	}
	p, ae := s.getPageCore(ctx, u, k, in.ID)
	if ae != nil {
		return mcpErr(ae), getPageOut{}, nil
	}
	body, whole := mcpCapBody(p.Body)
	p.Body = body
	out := getPageOut{Page: mcpPage{Page: p, URL: mcpPageURL(p), Truncated: !whole}}
	return nil, out, nil
}

// ---- list_backlinks ------------------------------------------------------

type listBacklinksIn struct {
	PageID int64 `json:"page_id" jsonschema:"page whose incoming links to list"`
}

type listBacklinksOut struct {
	Backlinks []backlinkHit `json:"backlinks"`
}

func (s *Server) mcpListBacklinks(ctx context.Context, req *mcp.CallToolRequest, in listBacklinksIn) (*mcp.CallToolResult, listBacklinksOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), listBacklinksOut{}, nil
	}
	hits, ae := s.backlinksCore(ctx, u, k, in.PageID)
	if ae != nil {
		return mcpErr(ae), listBacklinksOut{}, nil
	}
	return nil, listBacklinksOut{Backlinks: hits}, nil
}

// ---- search --------------------------------------------------------------

type searchIn struct {
	Query   string `json:"query" jsonschema:"search terms"`
	SpaceID *int64 `json:"space_id,omitempty" jsonschema:"optional space id to restrict results to"`
	Limit   int    `json:"limit,omitempty" jsonschema:"max results (default 25)"`
}

type searchOut struct {
	Results []searchHit `json:"results"`
}

func (s *Server) mcpSearch(ctx context.Context, req *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, searchOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), searchOut{}, nil
	}
	results, ae := s.searchCore(ctx, u, k, in.Query, in.SpaceID, in.Limit)
	if ae != nil {
		return mcpErr(ae), searchOut{}, nil
	}
	out := searchOut{Results: results}
	return nil, out, nil
}

// ---- search_bodies -------------------------------------------------------

type searchBodiesIn struct {
	Query   string `json:"query" jsonschema:"search terms"`
	SpaceID int64  `json:"space_id" jsonschema:"id of the space to search within"`
	Limit   int    `json:"limit,omitempty" jsonschema:"max results 1-100 (default 20)"`
}

type searchBodiesOut struct {
	Results []searchBodyHit `json:"results"`
}

func (s *Server) mcpSearchBodies(ctx context.Context, req *mcp.CallToolRequest, in searchBodiesIn) (*mcp.CallToolResult, searchBodiesOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), searchBodiesOut{}, nil
	}
	results, ae := s.searchBodiesCore(ctx, u, k, in.SpaceID, in.Query, in.Limit)
	if ae != nil {
		return mcpErr(ae), searchBodiesOut{}, nil
	}
	return nil, searchBodiesOut{Results: results}, nil
}

// ---- semantic_search -----------------------------------------------------

type semanticSearchIn struct {
	Query   string `json:"query" jsonschema:"natural-language query"`
	SpaceID *int64 `json:"space_id,omitempty" jsonschema:"optional space id to restrict results to"`
	Limit   int    `json:"limit,omitempty" jsonschema:"max chunks (default service-defined)"`
	Mode    string `json:"mode,omitempty" jsonschema:"hybrid|semantic|lexical (default hybrid)"`
}

type semanticSearchOut struct {
	Results []rag.Hit `json:"results"`
}

func (s *Server) mcpSemanticSearch(ctx context.Context, req *mcp.CallToolRequest, in semanticSearchIn) (*mcp.CallToolResult, semanticSearchOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), semanticSearchOut{}, nil
	}
	if !s.rag.Enabled() {
		return mcpErr(&apiErr{503, "rag_disabled", "semantic search is not configured"}), semanticSearchOut{}, nil
	}
	// A space-pinned bearer key may only ever see its one space.
	spaceID := in.SpaceID
	if k != nil && k.SpaceID != nil {
		spaceID = k.SpaceID
	}
	hits, err := s.rag.Search(ctx, u.ID, in.Query, spaceID, in.Limit, in.Mode)
	if err != nil {
		return mcpErr(&apiErr{500, "internal", "semantic search failed"}), semanticSearchOut{}, nil
	}
	out := semanticSearchOut{Results: hits}
	return nil, out, nil
}

// ---- read_chunk ----------------------------------------------------------

type readChunkIn struct {
	ChunkID int64 `json:"chunk_id" jsonschema:"chunk id from a semantic_search result"`
}

type readChunkOut struct {
	Chunk rag.ChunkRead `json:"chunk"`
}

func (s *Server) mcpReadChunk(ctx context.Context, req *mcp.CallToolRequest, in readChunkIn) (*mcp.CallToolResult, readChunkOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), readChunkOut{}, nil
	}
	if !s.rag.Enabled() {
		return mcpErr(&apiErr{503, "rag_disabled", "semantic search is not configured"}), readChunkOut{}, nil
	}
	var spaceID *int64
	if k != nil && k.SpaceID != nil {
		spaceID = k.SpaceID
	}
	chunk, err := s.rag.ReadChunk(ctx, u.ID, in.ChunkID, spaceID)
	if err != nil {
		if errors.Is(err, rag.ErrChunkNotFound) {
			return mcpErr(&apiErr{404, "not_found", "chunk not found"}), readChunkOut{}, nil
		}
		return mcpErr(&apiErr{500, "internal", "read chunk failed"}), readChunkOut{}, nil
	}
	return nil, readChunkOut{Chunk: *chunk}, nil
}

// ---- fetch (ChatGPT Deep Research) ---------------------------------------

type fetchIn struct {
	ID string `json:"id" jsonschema:"page id, as returned by search results"`
}

type fetchOut struct {
	ID       string         `json:"id"`
	Title    string         `json:"title"`
	Text     string         `json:"text"`
	URL      string         `json:"url"`
	Metadata map[string]any `json:"metadata"`
}

func (s *Server) mcpFetch(ctx context.Context, req *mcp.CallToolRequest, in fetchIn) (*mcp.CallToolResult, fetchOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), fetchOut{}, nil
	}
	pid, err := strconv.ParseInt(strings.TrimSpace(in.ID), 10, 64)
	if err != nil || pid <= 0 {
		return mcpErr(&apiErr{400, "bad_request", "id must be a numeric page id"}), fetchOut{}, nil
	}
	p, ae := s.getPageCore(ctx, u, k, pid)
	if ae != nil {
		return mcpErr(ae), fetchOut{}, nil
	}
	text, whole := mcpCapBody(p.Body)
	return nil, fetchOut{
		ID:       in.ID,
		Title:    p.Title,
		Text:     text,
		URL:      mcpPageURL(p),
		Metadata: map[string]any{"space_id": p.SpaceID, "truncated": !whole},
	}, nil
}

// ---- create_page ---------------------------------------------------------

type createPageIn struct {
	SpaceID  int64  `json:"space_id" jsonschema:"id of the space to create the page in"`
	ParentID *int64 `json:"parent_id,omitempty" jsonschema:"optional parent page id"`
	Title    string `json:"title" jsonschema:"page title"`
	Body     string `json:"body" jsonschema:"markdown body"`
}

func (s *Server) mcpCreatePage(ctx context.Context, req *mcp.CallToolRequest, in createPageIn) (*mcp.CallToolResult, getPageOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), getPageOut{}, nil
	}
	if ae := mcpRequireWrite(k); ae != nil {
		return mcpErr(ae), getPageOut{}, nil
	}
	p, ae := s.createPageCore(ctx, u, k, pageCreateRequest{
		SpaceID:  in.SpaceID,
		ParentID: in.ParentID,
		Title:    in.Title,
		Body:     in.Body,
	})
	if ae != nil {
		return mcpErr(ae), getPageOut{}, nil
	}
	out := getPageOut{Page: mcpPage{Page: p, URL: mcpPageURL(p)}}
	return nil, out, nil
}

// ---- update_page ---------------------------------------------------------

type updatePageIn struct {
	ID    int64   `json:"id" jsonschema:"page id to patch"`
	Title *string `json:"title,omitempty" jsonschema:"new title (omit to leave unchanged)"`
	Body  *string `json:"body,omitempty" jsonschema:"new markdown body (omit to leave unchanged)"`
}

func (s *Server) mcpUpdatePage(ctx context.Context, req *mcp.CallToolRequest, in updatePageIn) (*mcp.CallToolResult, getPageOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), getPageOut{}, nil
	}
	if ae := mcpRequireWrite(k); ae != nil {
		return mcpErr(ae), getPageOut{}, nil
	}
	// agentWrite=true: an agent rewriting the body must invalidate the Yjs collab
	// overlay so live/next editors see it instead of stale CRDT state.
	p, ae := s.updatePageCore(ctx, u, k, in.ID, pageUpdateRequest{Title: in.Title, Body: in.Body}, true)
	if ae != nil {
		return mcpErr(ae), getPageOut{}, nil
	}
	out := getPageOut{Page: mcpPage{Page: p, URL: mcpPageURL(p)}}
	return nil, out, nil
}

// ---- delete_page ---------------------------------------------------------

type deletePageIn struct {
	ID int64 `json:"id" jsonschema:"page id to delete"`
}

type okOut struct {
	OK bool `json:"ok"`
}

func (s *Server) mcpDeletePage(ctx context.Context, req *mcp.CallToolRequest, in deletePageIn) (*mcp.CallToolResult, okOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), okOut{}, nil
	}
	if ae := mcpRequireWrite(k); ae != nil {
		return mcpErr(ae), okOut{}, nil
	}
	if ae := s.deletePageCore(ctx, u, k, in.ID); ae != nil {
		return mcpErr(ae), okOut{}, nil
	}
	return nil, okOut{OK: true}, nil
}

// ---- move_page -----------------------------------------------------------

type movePageIn struct {
	ID       int64  `json:"id" jsonschema:"page to move"`
	SpaceID  *int64 `json:"space_id,omitempty" jsonschema:"relocate to this space (omit to keep)"`
	ParentID *int64 `json:"parent_id,omitempty" jsonschema:"new parent page id (omit to keep; mutually exclusive with make_root)"`
	MakeRoot bool   `json:"make_root,omitempty" jsonschema:"detach to top-level (mutually exclusive with parent_id)"`
	Position *int64 `json:"position,omitempty" jsonschema:"new 0-based position among siblings (omit to keep)"`
}

func (s *Server) mcpMovePage(ctx context.Context, req *mcp.CallToolRequest, in movePageIn) (*mcp.CallToolResult, getPageOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), getPageOut{}, nil
	}
	if ae := mcpRequireWrite(k); ae != nil {
		return mcpErr(ae), getPageOut{}, nil
	}
	if in.MakeRoot && in.ParentID != nil {
		return mcpErr(&apiErr{400, "bad_request", "parent_id and make_root are mutually exclusive"}), getPageOut{}, nil
	}
	var mv pageMoveParams
	if in.SpaceID != nil {
		mv.SpaceIDSet = true
		mv.NewSpaceID = *in.SpaceID
	}
	switch {
	case in.MakeRoot:
		mv.ParentIDSet = true
		mv.ParentIDIsNull = true
	case in.ParentID != nil:
		mv.ParentIDSet = true
		mv.NewParentID = *in.ParentID
	}
	if in.Position != nil {
		mv.PositionSet = true
		mv.NewPosition = *in.Position
	}
	p, ae := s.movePageCore(ctx, u, k, in.ID, mv)
	if ae != nil {
		return mcpErr(ae), getPageOut{}, nil
	}
	out := getPageOut{Page: mcpPage{Page: p, URL: mcpPageURL(p)}}
	return nil, out, nil
}

// ---- add_comment ---------------------------------------------------------

type addCommentAnchor struct {
	Prefix string `json:"prefix" jsonschema:"text immediately before the anchored span"`
	Exact  string `json:"exact" jsonschema:"the exact anchored span"`
	Suffix string `json:"suffix" jsonschema:"text immediately after the anchored span"`
}

type addCommentIn struct {
	PageID int64            `json:"page_id" jsonschema:"page to comment on"`
	Anchor addCommentAnchor `json:"anchor" jsonschema:"text-quote anchor locating the comment in the body"`
	Body   string           `json:"body" jsonschema:"comment text (1-10000 chars)"`
}

type addCommentOut struct {
	Comment models.Comment `json:"comment"`
}

func (s *Server) mcpAddComment(ctx context.Context, req *mcp.CallToolRequest, in addCommentIn) (*mcp.CallToolResult, addCommentOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), addCommentOut{}, nil
	}
	if ae := mcpRequireWrite(k); ae != nil {
		return mcpErr(ae), addCommentOut{}, nil
	}
	c, ae := s.createCommentCore(ctx, u, k, in.PageID, commentCreateRequest{
		Body:         in.Body,
		AnchorPrefix: &in.Anchor.Prefix,
		AnchorExact:  &in.Anchor.Exact,
		AnchorSuffix: &in.Anchor.Suffix,
	})
	if ae != nil {
		return mcpErr(ae), addCommentOut{}, nil
	}
	return nil, addCommentOut{Comment: c}, nil
}

// ---- create_space / update_space / delete_space --------------------------

type createSpaceIn struct {
	Name string `json:"name" jsonschema:"space name (1-200 chars)"`
	Slug string `json:"slug,omitempty" jsonschema:"optional url slug; derived from name when omitted"`
}

type spaceOut struct {
	Space models.Space `json:"space"`
}

func (s *Server) mcpCreateSpace(ctx context.Context, req *mcp.CallToolRequest, in createSpaceIn) (*mcp.CallToolResult, spaceOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), spaceOut{}, nil
	}
	if ae := mcpRequireWrite(k); ae != nil {
		return mcpErr(ae), spaceOut{}, nil
	}
	sp, ae := s.createSpaceCore(ctx, u, in.Name, in.Slug)
	if ae != nil {
		return mcpErr(ae), spaceOut{}, nil
	}
	return nil, spaceOut{Space: sp}, nil
}

type updateSpaceIn struct {
	ID   int64   `json:"id" jsonschema:"space id to patch"`
	Name *string `json:"name,omitempty" jsonschema:"new name (omit to leave unchanged)"`
	Slug *string `json:"slug,omitempty" jsonschema:"new slug (omit to leave unchanged)"`
}

func (s *Server) mcpUpdateSpace(ctx context.Context, req *mcp.CallToolRequest, in updateSpaceIn) (*mcp.CallToolResult, spaceOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), spaceOut{}, nil
	}
	if ae := mcpRequireWrite(k); ae != nil {
		return mcpErr(ae), spaceOut{}, nil
	}
	sp, ae := s.updateSpaceCore(ctx, u, k, in.ID, spaceUpdateRequest{Name: in.Name, Slug: in.Slug})
	if ae != nil {
		return mcpErr(ae), spaceOut{}, nil
	}
	return nil, spaceOut{Space: sp}, nil
}

type deleteSpaceIn struct {
	ID int64 `json:"id" jsonschema:"space id to delete (cascades)"`
}

func (s *Server) mcpDeleteSpace(ctx context.Context, req *mcp.CallToolRequest, in deleteSpaceIn) (*mcp.CallToolResult, okOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), okOut{}, nil
	}
	if ae := mcpRequireWrite(k); ae != nil {
		return mcpErr(ae), okOut{}, nil
	}
	if ae := s.deleteSpaceCore(ctx, u, k, in.ID); ae != nil {
		return mcpErr(ae), okOut{}, nil
	}
	return nil, okOut{OK: true}, nil
}

// ---- import_mira ---------------------------------------------------------

type importMiraIn struct {
	SpaceID   int64  `json:"space_id" jsonschema:"id of the space to import into"`
	ParentID  *int64 `json:"parent_id,omitempty" jsonschema:"optional parent page id"`
	SourceURL string `json:"source_url,omitempty" jsonschema:"https mira page URL (allowlisted host, fetched server-side); mutually exclusive with payload"`
	Payload   any    `json:"payload,omitempty" jsonschema:"inline mira block JSON; mutually exclusive with source_url"`
}

func (s *Server) mcpImportMira(ctx context.Context, req *mcp.CallToolRequest, in importMiraIn) (*mcp.CallToolResult, getPageOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), getPageOut{}, nil
	}
	if ae := mcpRequireWrite(k); ae != nil {
		return mcpErr(ae), getPageOut{}, nil
	}
	var payload []byte
	if in.Payload != nil {
		payload, _ = json.Marshal(in.Payload)
	}
	page, unlock, ae := s.importMiraCore(ctx, u, k, in.SpaceID, in.ParentID, in.SourceURL, payload)
	if unlock != "" {
		// Password-protected source — surface the unlock link as a tool error so
		// the agent can prompt the user to unlock it.
		b, _ := json.Marshal(map[string]any{"error": ae.Message, "code": ae.Code, "status": ae.Status, "unlock": unlock})
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, getPageOut{}, nil
	}
	if ae != nil {
		return mcpErr(ae), getPageOut{}, nil
	}
	out := getPageOut{Page: mcpPage{Page: page, URL: mcpPageURL(page)}}
	return nil, out, nil
}

// ---- submit_feedback (any scope, no mcpRequireWrite) ---------------------

type submitFeedbackIn struct {
	Subject string `json:"subject" jsonschema:"short subject (1-200 chars)"`
	Body    string `json:"body" jsonschema:"feedback body (1-8000 chars)"`
}

type submitFeedbackOut struct {
	Feedback feedbackDTO `json:"feedback"`
}

func (s *Server) mcpSubmitFeedback(ctx context.Context, req *mcp.CallToolRequest, in submitFeedbackIn) (*mcp.CallToolResult, submitFeedbackOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), submitFeedbackOut{}, nil
	}
	dto, ae := s.feedbackCore(ctx, u, k, feedbackCreateRequest{Subject: in.Subject, Body: in.Body})
	if ae != nil {
		return mcpErr(ae), submitFeedbackOut{}, nil
	}
	return nil, submitFeedbackOut{Feedback: dto}, nil
}
