package api

import (
	"context"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	pageResourceScheme  = "tela://page/"
	spaceResourceScheme = "tela://space/"
)

// registerMCPResources wires the resource surface: templates that round-trip the
// schemes tela writes into page bodies, so hosts can @-mention / re-read the
// pages and spaces the agent surfaces. Registered as templates (not enumerated)
// — spaces hold many pages, so there is no resources/list explosion; the host
// resolves a concrete id on demand.
func (s *Server) registerMCPResources(server *mcp.Server) {
	server.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:        "page",
		Title:       "Tela page",
		Description: "A tela page's markdown, addressed by numeric id — matches the tela://page/{id} wikilink scheme written into page bodies.",
		URITemplate: pageResourceScheme + "{id}",
		MIMEType:    "text/markdown",
	}, s.mcpReadPageResource)

	server.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:        "space",
		Title:       "Tela space",
		Description: "A tela space's metadata + a linked index of its pages, addressed by numeric id.",
		URITemplate: spaceResourceScheme + "{id}",
		MIMEType:    "text/markdown",
	}, s.mcpReadSpaceResource)
}

// pageResourceURI is the canonical resource URI for a page id.
func pageResourceURI(id int64) string {
	return pageResourceScheme + strconv.FormatInt(id, 10)
}

// parseResourceID extracts the trailing numeric id from a `scheme{id}` URI. The
// SDK matches the template to pick the handler but does NOT hand us the captured
// variable, so we re-parse it ourselves.
func parseResourceID(uri, scheme string) (int64, bool) {
	rest := strings.TrimPrefix(uri, scheme)
	if rest == uri {
		return 0, false
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// mcpReadPageResource serves tela://page/{id}: re-parse the id, gate on
// membership via getPageCore, and return the page as markdown. Any failure
// collapses to ResourceNotFoundError so resource reads can't enumerate pages
// across membership boundaries (mirrors get_page's 403-collapse).
func (s *Server) mcpReadPageResource(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	uri := req.Params.URI
	id, ok := parseResourceID(uri, pageResourceScheme)
	if !ok {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	u, k := mcpIdentity(req)
	if u == nil {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	p, ae := s.getPageCore(ctx, u, k, id)
	if ae != nil {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{
			URI:      uri,
			MIMEType: "text/markdown",
			Text:     "# " + p.Title + "\n\n" + p.Body,
		}},
	}, nil
}

// mcpReadSpaceResource serves tela://space/{id}: membership-gated, returns the
// space name + a markdown index linking each page (via tela://page/{id}) so the
// host can drill in. Failures collapse to ResourceNotFoundError.
func (s *Server) mcpReadSpaceResource(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	uri := req.Params.URI
	id, ok := parseResourceID(uri, spaceResourceScheme)
	if !ok {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	u, k := mcpIdentity(req)
	if u == nil {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	sp, ae := s.getSpaceCore(ctx, u, k, id)
	if ae != nil {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, title FROM pages WHERE space_id = $1 AND deleted_at IS NULL ORDER BY position ASC, id ASC`, id)
	if err != nil {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	defer rows.Close()

	var b strings.Builder
	b.WriteString("# " + sp.Name + "\n\n")
	n := 0
	for rows.Next() {
		var pid int64
		var title string
		if err := rows.Scan(&pid, &title); err != nil {
			return nil, mcp.ResourceNotFoundError(uri)
		}
		b.WriteString("- [" + title + "](" + pageResourceURI(pid) + ")\n")
		n++
	}
	if err := rows.Err(); err != nil {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	if n == 0 {
		b.WriteString("_No pages yet._\n")
	}
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "text/markdown", Text: b.String()}},
	}, nil
}
