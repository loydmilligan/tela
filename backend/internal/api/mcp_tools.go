package api

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
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
	// is a closed world (its own DB): every tool sets OpenWorldHint false. *bool
	// so the value survives the SDK's omitempty.
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
		Description: "Full markdown body + metadata for a numeric page id. Includes an `epistemic` block — trust signals computed from the wiki's own state: freshness (age, stale, review_overdue), provenance (human / agent / sync), and corroboration vs. dispute against same-space pages. Weigh it: prefer fresh, corroborated, human-reviewed pages; treat a stale or disputed page as lower-confidence and check its listed disputes before relying on it.",
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
		Description: "Meaning-aware chunk search (vector + keyword, RRF) over pages AND attached files (PDFs, text docs). Returns ranked chunks with chunk_id + citations. `source_kind` is \"page\" or \"file\"; a file hit also carries file_name, the parent page_id, and a download_url. Requires a configured embedder.",
		Annotations: readOnly,
	}, s.mcpSemanticSearch)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "read_chunk",
		Title:       "Read chunk",
		Description: "Fetch one chunk's full section text by chunk_id (from semantic_search), for a page OR a file chunk. Middle granularity between a search snippet and get_page; a file chunk cites the file (file_name + parent page_id + download_url).",
		Annotations: readOnly,
	}, s.mcpReadChunk)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "related_pages",
		Title:       "Related pages",
		Description: "Pages semantically related to a given page (\"see also\"), ranked by similarity. Discovery beyond explicit [[wikilinks]]/backlinks. Works without a live embedder (uses stored vectors).",
		Annotations: readOnly,
	}, s.mcpRelatedPages)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "suggest_links",
		Title:       "Suggest links",
		Description: "Given draft text (a page you're writing), return existing pages it should link to, by semantic similarity. Use while authoring to wire a new page into the knowledge base instead of leaving it an orphan. Requires a configured embedder.",
		Annotations: readOnly,
	}, s.mcpSuggestLinks)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "find_overlaps",
		Title:       "Find overlapping pages",
		Description: "Near-duplicate page PAIRS that share a near-identical passage (real merge/redirect candidates) for wiki hygiene. Optional space_id restricts to one space; threshold (0..1, default 0.92) is the minimum chunk-level similarity to count as a duplicate.",
		Annotations: readOnly,
	}, s.mcpFindOverlaps)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "knowledge_gaps",
		Title:       "Knowledge gaps",
		Description: "The most-asked \"ask your docs\" questions the corpus could NOT answer — a content roadmap. Instance-admin only (exposes users' questions). Optional since_days window.",
		Annotations: readOnly,
	}, s.mcpKnowledgeGaps)

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
		Description: "Create a page in a space (editor+). Body is markdown; tela://page/{id} links and [[Page Title]] wikilinks (resolved by title within the space) are indexed as backlinks. " + authoringToolHint() + deckAuthoringToolHint(),
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, OpenWorldHint: &no},
	}, s.mcpCreatePage)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_page",
		Title:       "Update page",
		Description: "Patch a page's title and/or body (editor+). A body change auto-snapshots a revision. " + authoringToolHint() + deckAuthoringToolHint(),
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: &no, OpenWorldHint: &no},
	}, s.mcpUpdatePage)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "lint_deck",
		Title:       "Lint slide deck",
		Description: "Validate a deck page's slides against the tahta theme contract — unknown layouts, missing required fields, type/format mistakes. Run after authoring/editing a deck to catch problems before presenting. Returns structured issues per slide.",
		Annotations: readOnly,
	}, s.mcpLintDeck)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "preview_deck",
		Title:       "Preview slide deck",
		Description: "Render a deck page to slide images and return them, so you can SEE how the deck looks (don't author blind). Pass `slides` to preview specific 1-based frames; omit for the first few. Renders are cached.",
		Annotations: readOnly,
	}, s.mcpPreviewDeck)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "patch_page",
		Title:       "Patch page section",
		Description: "Surgically edit ONE section of a page instead of rewriting the whole body (editor+). First call get_page format:\"map\" to see the section paths, then patch the target. Cheaper and safer than update_page on a long page — it never touches the rest of the document. Snapshots a revision like any edit.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, OpenWorldHint: &no},
	}, s.mcpPatchPage)

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
		Name:        "submit_feedback",
		Title:       "Submit feedback",
		Description: "Submit free-text feedback about tela / tela-mcp itself (friction, bugs, missing capabilities). NOT for page content — use add_comment for that.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, OpenWorldHint: &no},
	}, s.mcpSubmitFeedback)

	// ---- attachments (files on a page: images, PDFs, datasets, …) ----

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_attachments",
		Title:       "List attachments",
		Description: "List the files attached to a page (uploads AND rclone-synced files): name, mime, byte size, a stable serve URL, an absolute download_url, and a ready-to-embed `markdown` snippet. `embedded` tells you the page body already references the file.",
		Annotations: readOnly,
	}, s.mcpListAttachments)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "upload_attachment",
		Title:       "Upload attachment",
		Description: "Upload a file (base64) and attach it to a page (editor+) — an image, PDF, dataset, etc. Returns the serve URL plus a ready-to-paste `markdown` snippet; then call update_page or patch_page to place it in the body (images render inline as ![](…), other files as a download card). The payload is inline base64 and rides through the model's context, so it is capped at 5 MB — keep it to small files (screenshots, charts, short PDFs). For larger files use request_attachment_upload (a direct PUT URL, bytes off-context), or the tela editor (drag-drop).",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, OpenWorldHint: &no},
	}, s.mcpUploadAttachment)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_attachment",
		Title:       "Delete attachment",
		Description: "Detach a file from a page by attachment id (editor+; ids come from list_attachments). Soft-delete. It does NOT edit the page body, so remove any inline embed separately with update_page/patch_page.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, OpenWorldHint: &no},
	}, s.mcpDeleteAttachment)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "request_attachment_upload",
		Title:       "Request a direct upload URL",
		Description: "Get a short-lived signed PUT URL to upload a file WITHOUT sending its bytes through the model context — for files over upload_attachment's 5 MB inline cap, or to avoid context bloat. Flow: call this → the host PUTs the raw bytes to the returned `put_url` over HTTP → then either read that PUT response or call confirm_attachment_upload to get the embed snippet, and place it with update_page/patch_page. Editor+. Only works on hosts that can make an outbound HTTP PUT; otherwise use upload_attachment.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, OpenWorldHint: &no},
	}, s.mcpRequestAttachmentUpload)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "confirm_attachment_upload",
		Title:       "Confirm a direct upload",
		Description: "After the bytes have been PUT to a request_attachment_upload URL, return the stored file's serve URL + ready-to-embed `markdown` (for hosts that couldn't read the PUT response). Editor+. Then place the snippet with update_page/patch_page.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: &no, OpenWorldHint: &no},
	}, s.mcpConfirmAttachmentUpload)
}

// ---- shared output shapes ------------------------------------------------

// mcpPage is a page row plus the human-shareable in-app URL. Embeds models.Page
// so the body + metadata fields are promoted verbatim.
type mcpPage struct {
	models.Page
	URL       string `json:"url"`
	Truncated bool   `json:"truncated,omitempty"`
	// Epistemic — trust signals (freshness, provenance, corroboration/dispute) so
	// an agent weighs the page, not just reads it. Set on get_page; nil elsewhere.
	Epistemic *EpistemicStatus `json:"epistemic,omitempty"`
	// Sections — the heading outline, returned (and Body emptied) when get_page is
	// called with format:"map". Each section's path is a patch_page target.
	Sections []pageSection `json:"sections,omitempty"`
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
	return canonicalBaseURL() + pageAppPath(p.SpaceID, p.ID, p.Title)
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
	ID     int64  `json:"id" jsonschema:"numeric page id"`
	Format string `json:"format,omitempty" jsonschema:"'full' (default) returns the markdown body; 'map' returns just the heading outline (section levels + paths) and no body — cheap to read, and each path is a target for patch_page"`
}

type getPageOut struct {
	Page mcpPage `json:"page"`
}

// createdPage is the create_page result: the new page's identity + metadata,
// deliberately WITHOUT the body. The caller just sent that body, so echoing it
// only doubles the payload; the id/url/parent_id is what a caller actually needs
// to keep building. Fetch the stored (normalized) body via get_page if required.
type createdPage struct {
	ID        int64          `json:"id"`
	SpaceID   int64          `json:"space_id"`
	ParentID  *int64         `json:"parent_id"`
	Title     string         `json:"title"`
	Position  int64          `json:"position"`
	Props     map[string]any `json:"props,omitempty"`
	CreatedAt string         `json:"created_at"`
	UpdatedAt string         `json:"updated_at"`
	URL       string         `json:"url"`
}

type createPageOut struct {
	Page createdPage `json:"page"`
}

func newCreatedPage(p models.Page) createdPage {
	return createdPage{
		ID:        p.ID,
		SpaceID:   p.SpaceID,
		ParentID:  p.ParentID,
		Title:     p.Title,
		Position:  p.Position,
		Props:     p.Props,
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
		URL:       mcpPageURL(p),
	}
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
	epi := s.pageEpistemic(ctx, p)
	if in.Format == "map" {
		sections := pageOutline(p.Body)
		p.Body = ""
		mp := mcpPage{Page: p, URL: mcpPageURL(p), Epistemic: epi, Sections: sections}
		return nil, getPageOut{Page: mp}, nil
	}
	body, whole := mcpCapBody(p.Body)
	p.Body = body
	out := getPageOut{Page: mcpPage{Page: p, URL: mcpPageURL(p), Truncated: !whole, Epistemic: epi}}
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
	enrichFileCitations(hits)
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
	enrichFileChunk(chunk)
	return nil, readChunkOut{Chunk: *chunk}, nil
}

// ---- related_pages -------------------------------------------------------

type relatedPagesIn struct {
	PageID int64 `json:"page_id" jsonschema:"the page to find related pages for"`
	Limit  int   `json:"limit,omitempty" jsonschema:"max related pages (default 10)"`
}

type relatedPagesOut struct {
	Related []rag.RelatedPage `json:"related"`
}

func (s *Server) mcpRelatedPages(ctx context.Context, req *mcp.CallToolRequest, in relatedPagesIn) (*mcp.CallToolResult, relatedPagesOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), relatedPagesOut{}, nil
	}
	// Verify read access to the source page (also handles bearer scope).
	if _, ae := s.getPageCore(ctx, u, k, in.PageID); ae != nil {
		return mcpErr(ae), relatedPagesOut{}, nil
	}
	var spaceID *int64
	if k != nil && k.SpaceID != nil {
		spaceID = k.SpaceID
	}
	related, err := s.rag.RelatedPages(ctx, u.ID, in.PageID, spaceID, in.Limit)
	if err != nil {
		return mcpErr(&apiErr{500, "internal", "related lookup failed"}), relatedPagesOut{}, nil
	}
	return nil, relatedPagesOut{Related: related}, nil
}

// ---- suggest_links -------------------------------------------------------

type suggestLinksIn struct {
	Text    string `json:"text" jsonschema:"draft text to find link targets for"`
	SpaceID *int64 `json:"space_id,omitempty" jsonschema:"optional space id to restrict suggestions to"`
	Limit   int    `json:"limit,omitempty" jsonschema:"max suggestions (default 10)"`
}

type suggestLinksOut struct {
	Suggestions []rag.RelatedPage `json:"suggestions"`
}

func (s *Server) mcpSuggestLinks(ctx context.Context, req *mcp.CallToolRequest, in suggestLinksIn) (*mcp.CallToolResult, suggestLinksOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), suggestLinksOut{}, nil
	}
	if !s.rag.Enabled() {
		return mcpErr(&apiErr{503, "rag_disabled", "semantic features are not configured"}), suggestLinksOut{}, nil
	}
	spaceID := in.SpaceID
	if k != nil && k.SpaceID != nil {
		spaceID = k.SpaceID
	}
	out, err := s.rag.SuggestLinks(ctx, u.ID, in.Text, spaceID, in.Limit)
	if err != nil {
		return mcpErr(&apiErr{500, "internal", "suggest-links failed"}), suggestLinksOut{}, nil
	}
	return nil, suggestLinksOut{Suggestions: out}, nil
}

// ---- find_overlaps -------------------------------------------------------

type findOverlapsIn struct {
	SpaceID   *int64  `json:"space_id,omitempty" jsonschema:"optional space id to restrict to overlaps within one space"`
	Threshold float64 `json:"threshold,omitempty" jsonschema:"min chunk-level cosine similarity 0..1 to count as a duplicate (default 0.92)"`
	Limit     int     `json:"limit,omitempty" jsonschema:"max pairs (default 50)"`
}

type findOverlapsOut struct {
	Overlaps []rag.OverlapPair `json:"overlaps"`
}

func (s *Server) mcpFindOverlaps(ctx context.Context, req *mcp.CallToolRequest, in findOverlapsIn) (*mcp.CallToolResult, findOverlapsOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), findOverlapsOut{}, nil
	}
	spaceID := in.SpaceID
	if k != nil && k.SpaceID != nil {
		spaceID = k.SpaceID
	}
	pairs, err := s.rag.FindOverlaps(ctx, u.ID, spaceID, in.Threshold, in.Limit)
	if err != nil {
		return mcpErr(&apiErr{500, "internal", "overlap lookup failed"}), findOverlapsOut{}, nil
	}
	return nil, findOverlapsOut{Overlaps: pairs}, nil
}

// ---- knowledge_gaps (admin) ----------------------------------------------

type knowledgeGapsIn struct {
	SinceDays int `json:"since_days,omitempty" jsonschema:"only count asks in the last N days (0 = all time)"`
	Limit     int `json:"limit,omitempty" jsonschema:"max gaps (default 50)"`
}

type knowledgeGapsOut struct {
	Gaps []rag.KnowledgeGap `json:"gaps"`
}

func (s *Server) mcpKnowledgeGaps(ctx context.Context, req *mcp.CallToolRequest, in knowledgeGapsIn) (*mcp.CallToolResult, knowledgeGapsOut, error) {
	u, _ := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), knowledgeGapsOut{}, nil
	}
	if !u.IsInstanceAdmin {
		return mcpErr(&apiErr{403, "forbidden", "knowledge_gaps is instance-admin only"}), knowledgeGapsOut{}, nil
	}
	gaps, err := s.rag.KnowledgeGaps(ctx, in.SinceDays, in.Limit)
	if err != nil {
		return mcpErr(&apiErr{500, "internal", "gaps query failed"}), knowledgeGapsOut{}, nil
	}
	return nil, knowledgeGapsOut{Gaps: gaps}, nil
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
	SpaceID        int64          `json:"space_id" jsonschema:"id of the space to create the page in"`
	ParentID       *int64         `json:"parent_id,omitempty" jsonschema:"optional parent page id"`
	Title          string         `json:"title" jsonschema:"page title"`
	Body           string         `json:"body" jsonschema:"markdown body"`
	Props          map[string]any `json:"props,omitempty" jsonschema:"optional page properties (frontmatter); free-form keys, reserved keys like id/title/slug/created are ignored"`
	IdempotencyKey string         `json:"idempotency_key,omitempty" jsonschema:"optional client-generated key; a retry with the same key returns the original result instead of creating a duplicate page (safe retries after a dropped connection)"`
}

func (s *Server) mcpCreatePage(ctx context.Context, req *mcp.CallToolRequest, in createPageIn) (*mcp.CallToolResult, createPageOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), createPageOut{}, nil
	}
	if ae := mcpRequireWrite(k); ae != nil {
		return mcpErr(ae), createPageOut{}, nil
	}
	return mcpIdempotent(ctx, s.DB, u.ID, in.IdempotencyKey, "create_page", func() (*mcp.CallToolResult, createPageOut, error) {
		p, ae := s.createPageCore(withAgentWrite(ctx), u, k, pageCreateRequest{
			SpaceID:  in.SpaceID,
			ParentID: in.ParentID,
			Title:    in.Title,
			Body:     in.Body,
			Props:    in.Props,
		})
		if ae != nil {
			return mcpErr(ae), createPageOut{}, nil
		}
		return nil, createPageOut{Page: newCreatedPage(p)}, nil
	})
}

// ---- update_page ---------------------------------------------------------

type updatePageIn struct {
	ID    int64          `json:"id" jsonschema:"page id to patch"`
	Title *string        `json:"title,omitempty" jsonschema:"new title (omit to leave unchanged)"`
	Body  *string        `json:"body,omitempty" jsonschema:"new markdown body (omit to leave unchanged)"`
	Props map[string]any `json:"props,omitempty" jsonschema:"replace the whole properties bag (omit to leave unchanged); reserved keys are ignored"`
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
	p, ae := s.updatePageCore(ctx, u, k, in.ID, pageUpdateRequest{Title: in.Title, Body: in.Body, Props: in.Props}, true)
	if ae != nil {
		return mcpErr(ae), getPageOut{}, nil
	}
	out := getPageOut{Page: mcpPage{Page: p, URL: mcpPageURL(p)}}
	return nil, out, nil
}

// ---- patch_page (surgical section edit) ----------------------------------

type patchPageIn struct {
	ID             int64  `json:"id" jsonschema:"page id to patch"`
	Target         string `json:"target" jsonschema:"the section to edit, by its heading path from get_page format:\"map\" (e.g. 'Setup' or 'Deploy > Production'); the bare heading text also resolves"`
	Operation      string `json:"operation" jsonschema:"append (add to the end of the section's body), prepend (add right under the heading), replace (swap the section's body, heading kept), or delete (remove the heading and its body)"`
	Content        string `json:"content,omitempty" jsonschema:"markdown to insert; omit for delete"`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"optional client-generated key; a retry with the same key returns the original result instead of re-applying the patch"`
}

func (s *Server) mcpPatchPage(ctx context.Context, req *mcp.CallToolRequest, in patchPageIn) (*mcp.CallToolResult, getPageOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), getPageOut{}, nil
	}
	if ae := mcpRequireWrite(k); ae != nil {
		return mcpErr(ae), getPageOut{}, nil
	}
	return mcpIdempotent(ctx, s.DB, u.ID, in.IdempotencyKey, "patch_page", func() (*mcp.CallToolResult, getPageOut, error) {
		p, ae := s.getPageCore(ctx, u, k, in.ID)
		if ae != nil {
			return mcpErr(ae), getPageOut{}, nil
		}
		newBody, _, err := applyPatch(p.Body, in.Target, in.Operation, in.Content)
		if err != nil {
			return mcpErr(&apiErr{Status: 400, Code: "bad_request", Message: err.Error()}), getPageOut{}, nil
		}
		// Write through the normal update path (agentWrite=true) so the revision,
		// reindex, agreement and provenance all fire exactly as for any edit.
		up, ae := s.updatePageCore(ctx, u, k, in.ID, pageUpdateRequest{Body: &newBody}, true)
		if ae != nil {
			return mcpErr(ae), getPageOut{}, nil
		}
		epi := s.pageEpistemic(ctx, up)
		body, whole := mcpCapBody(up.Body)
		up.Body = body
		return nil, getPageOut{Page: mcpPage{Page: up, URL: mcpPageURL(up), Truncated: !whole, Epistemic: epi}}, nil
	})
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
	PageID         int64            `json:"page_id" jsonschema:"page to comment on"`
	Anchor         addCommentAnchor `json:"anchor" jsonschema:"text-quote anchor locating the comment in the body"`
	Body           string           `json:"body" jsonschema:"comment text (1-10000 chars)"`
	IdempotencyKey string           `json:"idempotency_key,omitempty" jsonschema:"optional client-generated key; a retry with the same key returns the original result instead of posting a duplicate comment (safe retries after a dropped connection)"`
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
	return mcpIdempotent(ctx, s.DB, u.ID, in.IdempotencyKey, "add_comment", func() (*mcp.CallToolResult, addCommentOut, error) {
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
	})
}

// ---- create_space / update_space / delete_space --------------------------

type createSpaceIn struct {
	Name           string `json:"name" jsonschema:"space name (1-200 chars)"`
	Slug           string `json:"slug,omitempty" jsonschema:"optional url slug; derived from name when omitted"`
	OrgID          *int64 `json:"org_id,omitempty" jsonschema:"optional org id to own the space (caller must be a member); omit for a personal space"`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"optional client-generated key; a retry with the same key returns the original result instead of creating a duplicate space (safe retries after a dropped connection)"`
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
	return mcpIdempotent(ctx, s.DB, u.ID, in.IdempotencyKey, "create_space", func() (*mcp.CallToolResult, spaceOut, error) {
		sp, ae := s.createSpaceCore(ctx, u, in.Name, in.Slug, in.OrgID)
		if ae != nil {
			return mcpErr(ae), spaceOut{}, nil
		}
		return nil, spaceOut{Space: sp}, nil
	})
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

// ---- attachments (list_attachments / upload_attachment / delete_attachment) ----

// mcpAttachment is an attachmentOut plus two agent conveniences: an absolute
// download_url (the embedded `url` is relative, for the body; this one is
// fetchable over HTTP directly) and a ready-to-paste `markdown` embed snippet.
type mcpAttachment struct {
	attachmentOut
	DownloadURL string `json:"download_url"`
	Markdown    string `json:"markdown"`
}

func newMCPAttachment(a attachmentOut) mcpAttachment {
	return mcpAttachment{
		attachmentOut: a,
		DownloadURL:   canonicalBaseURL() + a.URL,
		Markdown:      attachmentEmbedMarkdown(a),
	}
}

// attachmentEmbedMarkdown is the snippet to drop into a page body: inline image
// syntax for image mimes, a :::file download card for everything else.
func attachmentEmbedMarkdown(a attachmentOut) string {
	if strings.HasPrefix(a.Mime, "image/") {
		return fmt.Sprintf("![%s](%s)", a.Name, a.URL)
	}
	return fmt.Sprintf(":::file{name=%q size=\"%d\"}\n%s\n:::", a.Name, a.ByteSize, a.URL)
}

// mcpInlineUploadCap bounds upload_attachment's base64 payload. Inline base64
// rides through the model's context (a tool argument IS model content), so a
// large blob bloats tokens + cost and can trip a host's content filter. Files
// above this belong on a side channel — the editor drag-drop, or the planned
// request_attachment_upload signed-PUT handshake — where the bytes never enter
// the context. This is the *transport* limit; davFileMaxBytes (50 MiB) remains
// the storage limit. A var so tests can shrink it without generating megabytes.
var mcpInlineUploadCap = 5 << 20 // 5 MiB

// decodeMCPBase64 decodes a base64 tool argument, tolerating a leading
// `data:<mime>;base64,` URL prefix that agents often include.
func decodeMCPBase64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "data:") {
		if i := strings.Index(s, ";base64,"); i >= 0 {
			s = s[i+len(";base64,"):]
		}
	}
	return base64.StdEncoding.DecodeString(s)
}

type listAttachmentsIn struct {
	PageID int64 `json:"page_id" jsonschema:"page whose attachments to list"`
}

type listAttachmentsOut struct {
	Attachments []mcpAttachment `json:"attachments"`
}

func (s *Server) mcpListAttachments(ctx context.Context, req *mcp.CallToolRequest, in listAttachmentsIn) (*mcp.CallToolResult, listAttachmentsOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), listAttachmentsOut{}, nil
	}
	atts, ae := s.listPageAttachmentsCore(ctx, u, k, in.PageID)
	if ae != nil {
		return mcpErr(ae), listAttachmentsOut{}, nil
	}
	out := listAttachmentsOut{Attachments: make([]mcpAttachment, len(atts))}
	for i, a := range atts {
		out.Attachments[i] = newMCPAttachment(a)
	}
	return nil, out, nil
}

type uploadAttachmentIn struct {
	PageID     int64  `json:"page_id" jsonschema:"page to attach the file to"`
	Name       string `json:"name" jsonschema:"file name including extension, e.g. report.pdf or chart.png (drives the displayed name + type detection)"`
	DataBase64 string `json:"data_base64" jsonschema:"the file bytes, base64-encoded; a leading data:<mime>;base64,… URL prefix is also accepted"`
}

type uploadAttachmentOut struct {
	Attachment mcpAttachment `json:"attachment"`
}

func (s *Server) mcpUploadAttachment(ctx context.Context, req *mcp.CallToolRequest, in uploadAttachmentIn) (*mcp.CallToolResult, uploadAttachmentOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), uploadAttachmentOut{}, nil
	}
	if ae := mcpRequireWrite(k); ae != nil {
		return mcpErr(ae), uploadAttachmentOut{}, nil
	}
	data, err := decodeMCPBase64(in.DataBase64)
	if err != nil {
		return mcpErr(&apiErr{400, "bad_request", "data_base64 is not valid base64"}), uploadAttachmentOut{}, nil
	}
	if len(data) > mcpInlineUploadCap {
		return mcpErr(&apiErr{413, "too_large", fmt.Sprintf(
			"file is %.1f MB; inline base64 upload is capped at %d MB (it rides through the model context). For larger files call request_attachment_upload for a direct PUT URL, or use the tela editor (drag-drop).",
			float64(len(data))/(1<<20), mcpInlineUploadCap>>20)}), uploadAttachmentOut{}, nil
	}
	a, ae := s.uploadPageAttachmentCore(ctx, u, k, in.PageID, in.Name, data)
	if ae != nil {
		return mcpErr(ae), uploadAttachmentOut{}, nil
	}
	return nil, uploadAttachmentOut{Attachment: newMCPAttachment(a)}, nil
}

type deleteAttachmentIn struct {
	PageID int64 `json:"page_id" jsonschema:"the page the file is attached to"`
	ID     int64 `json:"id" jsonschema:"attachment id (from list_attachments)"`
}

func (s *Server) mcpDeleteAttachment(ctx context.Context, req *mcp.CallToolRequest, in deleteAttachmentIn) (*mcp.CallToolResult, okOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), okOut{}, nil
	}
	if ae := mcpRequireWrite(k); ae != nil {
		return mcpErr(ae), okOut{}, nil
	}
	if ae := s.deletePageAttachmentCore(ctx, u, k, in.PageID, in.ID); ae != nil {
		return mcpErr(ae), okOut{}, nil
	}
	return nil, okOut{OK: true}, nil
}

// ---- direct upload handshake (request → PUT out-of-band → confirm) ----

type requestAttachmentUploadIn struct {
	PageID int64  `json:"page_id" jsonschema:"page to attach the file to"`
	Name   string `json:"name" jsonschema:"file name including extension, e.g. deck.pdf or photo.png"`
	Mime   string `json:"mime,omitempty" jsonschema:"optional content-type hint; for images the server still trusts magic bytes"`
}

type requestAttachmentUploadOut struct {
	Upload uploadTicket `json:"upload"`
}

func (s *Server) mcpRequestAttachmentUpload(ctx context.Context, req *mcp.CallToolRequest, in requestAttachmentUploadIn) (*mcp.CallToolResult, requestAttachmentUploadOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), requestAttachmentUploadOut{}, nil
	}
	if ae := mcpRequireWrite(k); ae != nil {
		return mcpErr(ae), requestAttachmentUploadOut{}, nil
	}
	t, ae := s.requestAttachmentUploadCore(ctx, u, k, in.PageID, in.Name, in.Mime)
	if ae != nil {
		return mcpErr(ae), requestAttachmentUploadOut{}, nil
	}
	return nil, requestAttachmentUploadOut{Upload: t}, nil
}

type confirmAttachmentUploadIn struct {
	UploadID string `json:"upload_id" jsonschema:"the upload_id from request_attachment_upload"`
}

func (s *Server) mcpConfirmAttachmentUpload(ctx context.Context, req *mcp.CallToolRequest, in confirmAttachmentUploadIn) (*mcp.CallToolResult, uploadAttachmentOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), uploadAttachmentOut{}, nil
	}
	if ae := mcpRequireWrite(k); ae != nil {
		return mcpErr(ae), uploadAttachmentOut{}, nil
	}
	a, ae := s.confirmAttachmentUploadCore(ctx, u, k, in.UploadID)
	if ae != nil {
		return mcpErr(ae), uploadAttachmentOut{}, nil
	}
	return nil, uploadAttachmentOut{Attachment: newMCPAttachment(a)}, nil
}
