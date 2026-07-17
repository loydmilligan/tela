package api

import (
	"context"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The agent front door for page properties. PRs 1 & 2 shipped the `field` and
// `query` blocks with a HUMAN front door (the read-view widgets) but no agent
// one: the only MCP path to a prop was update_page(props=…), which REPLACES the
// whole bag — omit a key, wipe a key. That is actively dangerous now that shipped
// blocks read those props.
//
// Both tools are thin wrappers: they resolve identity, gate scope, and call the
// same xCore the REST route calls (docs/access-model.md invariant 4 — one
// resolution path). No logic lives here.

type setPropIn struct {
	PageID int64  `json:"page_id" jsonschema:"id of the page whose property to set"`
	Key    string `json:"key" jsonschema:"the single property key to set (reserved keys like id/title/slug/created are rejected)"`
	Value  any    `json:"value" jsonschema:"the value to store — string, number, boolean, null, or a nested object/array; stored verbatim as JSON so props containment filters stay predictable"`
}

// setPropInputSchema is set_prop's input schema, written out BY HAND instead of
// reflected from setPropIn. That is the whole bug fix, so it is worth the words.
//
// `Value any` reflects to a schema with a description and NO `type` — an empty
// constraint. The tool then stated its type rule ("string, number, boolean,
// null, or a nested object/array") only in that description: prose, which a
// client cannot validate or serialize against. Handed an untyped field, a client
// has to guess how to encode the value, and it guessed string — EVERY time, for
// every non-string value. Observed on prod: ["tela","agents"] stored as
// "[\"tela\",\"agents\"]", 42 as "42", true as "true".
//
// The damage is silent and total: a string-typed value can never match
// `props @> {"tags":["tela"]}`, never compare numerically, never sort. So the
// query block, query_pages, and every dashboard built on containment quietly
// return fewer rows than the truth — no error, no warning. set_prop could not do
// the one thing its description promised, and never could.
//
// The server was never at fault: setPagePropCore marshals `value any` through
// json.Marshal with no coercion, and stores it verbatim. The fault was a schema
// that looked like a contract and was structurally incapable of constraining
// anything. Declaring the union is the ONLY real remedy — no server-side test can
// reach a client outside this codebase, but a schema that cannot lie needs none.
// mcp_props_query_test.go pins this schema; keep them together.
//
// The sibling fields prove the SDK emits unions perfectly well when told to:
// query_pages' space_id reflects to ["null","integer"]. This declaration was
// simply never written.
var setPropInputSchema = &jsonschema.Schema{
	Type: "object",
	Properties: map[string]*jsonschema.Schema{
		"page_id": {Type: "integer", Description: "id of the page whose property to set"},
		"key":     {Type: "string", Description: "the single property key to set (reserved keys like id/title/slug/created are rejected)"},
		"value": {
			// The union, in the field a machine actually reads.
			Types:       []string{"string", "number", "boolean", "object", "array", "null"},
			Description: "the value to store — string, number, boolean, null, or a nested object/array. Send it as its NATIVE JSON type, never JSON-encoded into a string: 42 not \"42\", [\"a\"] not \"[\\\"a\\\"]\". It is stored verbatim, so a stringified value is silently unqueryable — props containment, numeric compare and sort will all miss it.",
		},
	},
	Required: []string{"page_id", "key", "value"},
	// Mirrors what reflection produced, so the only schema change is the type.
	AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}},
}

type setPropOut struct {
	PageID int64 `json:"page_id"`
	// Props is the FULL bag after the merge — the caller's sibling keys are still
	// there, which is the observable difference from update_page.
	Props map[string]any `json:"props"`
}

// errSetPropOut is the Out for an error return. The SDK validates the typed Out
// against the generated output schema even when IsError is set, and a nil map
// serializes to null where the schema demands an object — so error paths must
// still hand back a well-formed (empty) bag.
func errSetPropOut() setPropOut { return setPropOut{Props: map[string]any{}} }

func (s *Server) mcpSetProp(ctx context.Context, req *mcp.CallToolRequest, in setPropIn) (*mcp.CallToolResult, setPropOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), errSetPropOut(), nil
	}
	if ae := mcpRequireWrite(k); ae != nil {
		return mcpErr(ae), errSetPropOut(), nil
	}
	props, ae := s.setPagePropCore(ctx, u, k, in.PageID, in.Key, in.Value)
	if ae != nil {
		return mcpErr(ae), errSetPropOut(), nil
	}
	return nil, setPropOut{PageID: in.PageID, Props: props}, nil
}

type queryPagesIn struct {
	Where map[string]any `json:"where,omitempty" jsonschema:"exact property containment filter, e.g. {\"type\":\"incident\"}; omit or {} matches every readable page"`
	// space_id is the only scoping arg exposed to agents. The core also accepts
	// "here", but that resolves against the page a query BLOCK lives on — there is
	// no such page context in a tool call, so it isn't offered.
	SpaceID *int64 `json:"space_id,omitempty" jsonschema:"optional space id to restrict results to; omit to search every space you can read"`
	Sort    string `json:"sort,omitempty" jsonschema:"sort key: updated | -updated | created | -created | title | -title (a '-' prefix is descending; default -updated)"`
	Limit   int    `json:"limit,omitempty" jsonschema:"max rows (default 50, max 200)"`
}

type queryPagesOut struct {
	Pages []queryPageRow `json:"pages"`
}

func (s *Server) mcpQueryPages(ctx context.Context, req *mcp.CallToolRequest, in queryPagesIn) (*mcp.CallToolResult, queryPagesOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), queryPagesOut{}, nil
	}
	// Space stays nil unless the caller scoped it — nil means "every space I can
	// read", and the core's space_access join is what actually gates the rows.
	cr := pagesQueryRequest{Where: in.Where, Sort: in.Sort, Limit: in.Limit}
	if in.SpaceID != nil {
		cr.Space = *in.SpaceID
	}
	rows, ae := s.queryPagesCore(ctx, u, k, cr)
	if ae != nil {
		return mcpErr(ae), queryPagesOut{}, nil
	}
	return nil, queryPagesOut{Pages: rows}, nil
}

type queryCommentsIn struct {
	Where map[string]any `json:"where,omitempty" jsonschema:"exact property containment filter over comment props, e.g. {\"type\":\"change\"}; omit or {} matches every readable comment"`
	// page_id is how the per-page changelog is built: every change-comment on
	// one page, newest first.
	PageID          *int64 `json:"page_id,omitempty" jsonschema:"optional page id — return only comments on this page (e.g. one page's changelog)"`
	SpaceID         *int64 `json:"space_id,omitempty" jsonschema:"optional space id to restrict results to; omit to search every space you can read"`
	IncludeResolved bool   `json:"include_resolved,omitempty" jsonschema:"include resolved comments (default false — resolved are hidden)"`
	Sort            string `json:"sort,omitempty" jsonschema:"sort key: created | -created | updated | -updated (default -created, newest first)"`
	Limit           int    `json:"limit,omitempty" jsonschema:"max rows (default 50, max 200)"`
}

type queryCommentsOut struct {
	Comments []queryCommentRow `json:"comments"`
}

func (s *Server) mcpQueryComments(ctx context.Context, req *mcp.CallToolRequest, in queryCommentsIn) (*mcp.CallToolResult, queryCommentsOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), queryCommentsOut{}, nil
	}
	cr := commentsQueryRequest{
		Where:           in.Where,
		IncludeResolved: in.IncludeResolved,
		Sort:            in.Sort,
		Limit:           in.Limit,
	}
	// page_id from a tool call is a SCOPE ("this page's changelog"), not the
	// block-context field — an agent has no ambient page.
	if in.PageID != nil {
		cr.Page = *in.PageID
	}
	if in.SpaceID != nil {
		cr.Space = *in.SpaceID
	}
	rows, ae := s.queryCommentsCore(ctx, u, k, cr)
	if ae != nil {
		return mcpErr(ae), queryCommentsOut{}, nil
	}
	return nil, queryCommentsOut{Comments: rows}, nil
}
