package api

import (
	"context"
	_ "embed"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcp_import.go — agent-facing guidance for bulk-importing an existing folder of
// files into a space. The import ACTION is the REST endpoint
// POST /api/spaces/{id}/import (multipart; file bytes can't ride MCP's 4 MiB
// request-body cap), so this surface is guidance ONLY: it tells an agent with
// shell access how to convert non-markdown files locally (Office → pages/sheets,
// PDFs/images → attachments) and drive the importer's dry-run → confirm → commit
// loop. Without it, import is invisible over MCP (no tool references it) and an
// agent naively hand-creates pages one file at a time — the same
// capability-not-disclosed failure the block manifest exists to prevent.
//
// Mirrors the sheet/deck-authoring guide pattern: a tool (hosts that call tools
// but don't read tela:// resources still get it) + a resource + a one-paragraph
// pointer folded into the server Instructions.

const importGuideURI = "tela://import-guide"

//go:embed import_guide.md
var importGuideMarkdownText string

// importGuideText returns the bulk-import recipe as markdown.
func importGuideText() string { return importGuideMarkdownText }

// importInstructionsSnippet is appended to the server Instructions so every
// connected host learns, on initialize, that bulk import exists and where the
// recipe lives.
func importInstructionsSnippet() string {
	return "\n\n## Importing existing files\n" +
		"To bring an existing folder of documents (Word docs, spreadsheets, PDFs, notes) into a space as a page tree — rather than hand-creating pages one at a time — call the `import_guide` tool (or read the `tela://import-guide` resource). It covers converting Office files locally (docx → pages, spreadsheets → live sheets), attaching PDFs/images, and the import endpoint's dry-run → confirm → commit contract.\n"
}

type importGuideOut struct {
	Guide string `json:"guide"` // markdown: the bulk-import recipe + endpoint contract
}

// mcpImportGuide is the TOOL form of the import guide (many hosts can call tools
// but not read tela:// resources). Same content as the resource.
func (s *Server) mcpImportGuide(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, importGuideOut, error) {
	if u, _ := mcpIdentity(req); u == nil {
		return mcpUnauthErr(), importGuideOut{}, nil
	}
	return nil, importGuideOut{Guide: importGuideText()}, nil
}

// mcpReadImportGuide serves tela://import-guide.
func (s *Server) mcpReadImportGuide(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{
			URI:      req.Params.URI,
			MIMEType: "text/markdown",
			Text:     importGuideText(),
		}},
	}, nil
}
