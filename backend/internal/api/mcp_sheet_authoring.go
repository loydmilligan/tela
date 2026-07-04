package api

import (
	"context"
	_ "embed"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Agent-facing SHEET authoring guide. A tela sheet is a page with property
// `sheet: true` whose body is Defter markdown (compact GFM tables + an optional
// fenced ```defter-style block) — the format the @defterjs packages render as a
// live spreadsheet. Defter ships its own agent contract (docs/AGENTS.md); we
// vendor it verbatim (see `make sheets-gen`, sourced from the published
// @defterjs/core package) and frame it with a short tela preamble.
//
// Unlike the deck guide (fetched at runtime from the tahta sidecar), sheets have
// NO render sidecar — the grid renders client-side — so the guide is embedded at
// build time. Keep it fresh by re-running `make sheets-gen` after bumping the
// @defterjs dependency; the single source of truth is the defter package.

const sheetAuthoringGuideURI = "tela://sheet-authoring-guide"

//go:embed sheet_authoring.md
var sheetAuthoringContract string

// sheetAuthoringToolHint is the sheet sentence appended to create_page/update_page.
func sheetAuthoringToolHint() string {
	return " When asked for a spreadsheet, a table of data with formulas/totals, a budget, a tracker, or any grid that computes — not a prose doc — set the page property sheet=true and write the body as Defter markdown (compact GFM tables + an optional ```defter-style block); call the sheet_authoring_guide tool (or read the tela://sheet-authoring-guide resource) for the format, formulas, and styling."
}

// telaSheetPreamble frames defter's contract for tela: how to MAKE a sheet here.
// Defter's own doc is generic ("a sheet is a markdown document"); this says how a
// sheet is created and edited within tela specifically.
func telaSheetPreamble() string {
	var b strings.Builder
	b.WriteString("# Authoring tela spreadsheets (sheets)\n\n")
	b.WriteString("Use this whenever someone asks for a **spreadsheet, a data table with formulas/totals, a budget, an invoice, a tracker, or any computing grid** (any wording) — not a prose doc. A tela **sheet** is a page whose body is **Defter markdown**: compact GFM tables (the content) plus an optional fenced ```defter-style block (fills, number formats, merges, charts, conditional formatting). It renders as a live, editable spreadsheet.\n")
	b.WriteString("\n## How sheets work in tela (read first)\n")
	b.WriteString("- Make a page a sheet: set the page property `sheet: true` (e.g. `create_page` with `props: {\"sheet\": true}`).\n")
	b.WriteString("- The body is stored **verbatim** — write canonical Defter markdown directly. Do NOT hand-align table columns with padding spaces (defter stores compact, one row per line, and aligns at render time); do NOT put page frontmatter in the body.\n")
	b.WriteString("- **Formulas are computed at read time and never stored** — always write the formula (`=SUM(D2:D9)`), never a baked number. Row 1 is the header; the `|---|` delimiter row does not count; the first data row is row 2.\n")
	b.WriteString("- **To EDIT an existing sheet, use `edit_sheet`, not `update_page`.** Pass a structured op (`setCells`, `insertRows`/`deleteRows`, `insertCols`/`deleteCols`, `setStyle`, `setFreeze`, `addSheet`/`renameSheet`/`deleteSheet`) and tela rewrites the Defter markdown correctly — inserting a row shifts every formula below it, so you never corrupt the grid by editing text by hand. It also touches only what you change, so you don't clobber a collaborator (sheets are live-collaborative). Reserve `update_page` for a full-body rewrite or the initial content.\n")
	b.WriteString("\nEverything below is defter's own authoring contract (the full format, coordinate, formula, and styling reference) — apply it verbatim.\n\n---\n\n")
	return b.String()
}

// sheetAuthoringGuideText = tela preamble + defter's AGENTS.md verbatim.
func sheetAuthoringGuideText() string {
	return telaSheetPreamble() + sheetAuthoringContract
}

// mcpReadSheetAuthoringGuide serves tela://sheet-authoring-guide.
func (s *Server) mcpReadSheetAuthoringGuide(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{
			URI:      req.Params.URI,
			MIMEType: "text/markdown",
			Text:     sheetAuthoringGuideText(),
		}},
	}, nil
}

type sheetGuideOut struct {
	Guide string `json:"guide"` // markdown: defter format, formulas, styling
}

// mcpSheetAuthoringGuide is the TOOL form of the sheet guide (many hosts can call
// tools but not read tela:// resources). Same content as the resource.
func (s *Server) mcpSheetAuthoringGuide(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, sheetGuideOut, error) {
	if u, _ := mcpIdentity(req); u == nil {
		return mcpUnauthErr(), sheetGuideOut{}, nil
	}
	return nil, sheetGuideOut{Guide: sheetAuthoringGuideText()}, nil
}
