package api

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerMCPTools wires the tela tool surface onto the MCP server. Each tool
// reads identity from the request (mcpIdentity), calls the shared xCore that
// also backs the REST route, and returns a typed Out so the SDK emits an output
// schema + structured content.
//
// Phase 0 spike: list_spaces only — proves transport + auth threading + typed
// structured output end-to-end. The remaining tools land in Phase 1.
func (s *Server) registerMCPTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_spaces",
		Description: "List every space the API key can access (id, name, slug).",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, s.mcpListSpaces)
}

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
