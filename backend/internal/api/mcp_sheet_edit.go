package api

// edit_sheet — the ONE structured-editing tool for sheets.
//
// A sheet body is Defter markdown (compact GFM tables + a defter-style block).
// Editing it well means A1-addressing, inserting/deleting rows/cols, styling,
// freezing, multi-sheet — a dozen distinct operations. Exposing each as its own
// MCP tool would bloat tela's top-level tool list and drown the non-sheet tools.
// So the whole op vocabulary rides through a SINGLE dispatch tool: the caller
// passes a structured `op` (or a batch via `ops`) whose `kind` selects the
// operation. tela's tool surface grows by exactly one.
//
// The op vocabulary + the reference-rewriting logic (insert a row → every
// formula below shifts) live in @defterjs/core's applyOp, NOT here — so anyone
// building their own MCP server on defter reuses the same primitives. We reach
// it through the deck/sheet render sidecar's /apply endpoint (the formula engine
// is TypeScript), then persist the returned canonical markdown through the
// normal update_page core so the lint gate, revision snapshot, RAG reindex, and
// Yjs collab reset all fire — an edit_sheet write is indistinguishable from a
// human edit downstream.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zcag/tela/backend/internal/sheetproj"
)

// editSheetIn is the edit_sheet input. Exactly one of Op / Ops carries the work
// (Ops wins if both are set). The op JSON shape is documented in the tool
// Description and, in full, by the sheet_authoring_guide tool.
type editSheetIn struct {
	PageID int64            `json:"page_id" jsonschema:"the sheet page id to edit (a page with sheet=true)"`
	Op     map[string]any   `json:"op,omitempty" jsonschema:"a single SheetOp object with a 'kind' field. Provide this OR ops."`
	Ops    []map[string]any `json:"ops,omitempty" jsonschema:"an ordered batch of SheetOps applied atomically (all-or-nothing; the sheet is only rewritten if every op succeeds). Provide this OR op."`
}

func (s *Server) mcpEditSheet(ctx context.Context, req *mcp.CallToolRequest, in editSheetIn) (*mcp.CallToolResult, getPageOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), getPageOut{}, nil
	}
	if ae := mcpRequireWrite(k); ae != nil {
		return mcpErr(ae), getPageOut{}, nil
	}

	p, ae := s.getPageCore(ctx, u, k, in.PageID)
	if ae != nil {
		return mcpErr(ae), getPageOut{}, nil
	}
	if !isSheetBag(p.Props) {
		return mcpErr(&apiErr{http.StatusBadRequest, "not_a_sheet",
			"edit_sheet only applies to a sheet (a page with sheet=true); use update_page or patch_page for a prose page"}), getPageOut{}, nil
	}

	ops := in.Ops
	if len(ops) == 0 {
		if in.Op == nil {
			return mcpErr(&apiErr{http.StatusBadRequest, "no_op",
				"provide an op (single SheetOp) or ops (a batch); see sheet_authoring_guide for the op vocabulary"}), getPageOut{}, nil
		}
		ops = []map[string]any{in.Op}
	}

	newBody, opErr, err := applySheetOps(ctx, p.Body, ops)
	if err != nil {
		return mcpErr(&apiErr{http.StatusBadGateway, "sheet_apply_unavailable",
			"the sheet-editing backend is unavailable: " + err.Error()}), getPageOut{}, nil
	}
	if opErr != "" {
		// A rejected op (bad A1 ref, unknown sheet, out-of-range) — surface the
		// engine's own message so the agent can correct and retry.
		return mcpErr(&apiErr{http.StatusUnprocessableEntity, "bad_sheet_op", opErr}), getPageOut{}, nil
	}

	// Persist through the shared update core (agentWrite=true) so the sheet lint
	// gate, revision snapshot, RAG reindex, and Yjs collab-overlay reset all fire.
	up, ae := s.updatePageCore(ctx, u, k, in.PageID, pageUpdateRequest{Body: &newBody}, true)
	if ae != nil {
		return mcpErr(ae), getPageOut{}, nil
	}

	// Echo the sheet back as COMPUTED values (formulas resolved), so the agent
	// sees the result of its edit rather than the source it just wrote.
	up.Body = sheetproj.Project(ctx, up.Body)
	out := getPageOut{Page: mcpPage{Page: up, URL: s.mcpPageURL(ctx, up)}}
	return nil, out, nil
}

// applySheetOps sends the current body + ops to the sidecar /apply endpoint and
// returns the new canonical Defter markdown. The three-valued return separates a
// rejected op (opErr set — a user-fixable 422 from the engine) from a transport/
// config failure (err set). Both empty ⇒ success. There is no in-process Go
// fallback: applyOp's reference-rewriting is the TypeScript engine's, so an
// unset/unreachable sidecar is a hard error (same as decks).
func applySheetOps(ctx context.Context, body string, ops []map[string]any) (newBody, opErr string, err error) {
	base := strings.TrimRight(os.Getenv("TELA_DECK_URL"), "/")
	if base == "" {
		return "", "", fmt.Errorf("sheet render sidecar not configured (TELA_DECK_URL)")
	}
	payload, err := json.Marshal(map[string]any{"body": body, "ops": ops})
	if err != nil {
		return "", "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/apply", bytes.NewReader(payload))
	if err != nil {
		return "", "", err
	}
	r.Header.Set("content-type", "application/json")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", "", err
	}
	var out struct {
		Body  string `json:"body"`
		Error string `json:"error"`
	}
	if e := json.Unmarshal(raw, &out); e != nil {
		return "", "", fmt.Errorf("sheet apply sidecar %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if resp.StatusCode == http.StatusUnprocessableEntity {
		if out.Error == "" {
			out.Error = "sheet op rejected"
		}
		return "", out.Error, nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("sheet apply sidecar %d: %s", resp.StatusCode, out.Error)
	}
	return out.Body, "", nil
}
