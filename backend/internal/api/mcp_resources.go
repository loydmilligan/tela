package api

import (
	"context"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const pageResourceScheme = "tela://page/"

// registerMCPResources wires the resource surface: a tela://page/{id} template
// that round-trips the wikilink scheme tela writes into page bodies, so hosts
// can @-mention / re-read the pages the agent surfaces. Registered as a template
// (not enumerated) — spaces hold thousands of pages, so there is no resources/list
// explosion; the host resolves a concrete id on demand.
func (s *Server) registerMCPResources(server *mcp.Server) {
	server.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:        "page",
		Title:       "Tela page",
		Description: "A tela page's markdown, addressed by numeric id — matches the tela://page/{id} wikilink scheme written into page bodies.",
		URITemplate: pageResourceScheme + "{id}",
		MIMEType:    "text/markdown",
	}, s.mcpReadPageResource)
}

// pageResourceURI is the canonical resource URI for a page id.
func pageResourceURI(id int64) string {
	return pageResourceScheme + strconv.FormatInt(id, 10)
}

// parsePageResourceURI extracts the page id from a tela://page/{id} URI. The SDK
// matches the template to pick this handler but does NOT hand us the captured
// variable, so we re-parse it ourselves.
func parsePageResourceURI(uri string) (int64, bool) {
	rest := strings.TrimPrefix(uri, pageResourceScheme)
	if rest == uri {
		return 0, false
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// pageResourceLink builds a resource-link content block pointing at a page, for
// inclusion in tool results so hosts render a click-through / @-mentionable chip
// that resolves back through the tela://page/{id} resource.
func pageResourceLink(id int64, title string) *mcp.ResourceLink {
	return &mcp.ResourceLink{
		URI:      pageResourceURI(id),
		Name:     title,
		Title:    title,
		MIMEType: "text/markdown",
	}
}

// mcpReadPageResource serves tela://page/{id}: re-parse the id, gate on
// membership via getPageCore, and return the page as markdown. Any failure
// collapses to ResourceNotFoundError so resource reads can't enumerate pages
// across membership boundaries (mirrors get_page's 403-collapse).
func (s *Server) mcpReadPageResource(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	uri := req.Params.URI
	id, ok := parsePageResourceURI(uri)
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
