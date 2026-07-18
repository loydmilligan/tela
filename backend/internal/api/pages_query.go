package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/zcag/tela/backend/internal/auth"
)

// Props query block. A ` ```query ` block in a page body renders a live table of
// pages filtered by their props (a Dataview analog). The frontend parses the
// block's YAML spec and POSTs it here; this returns the matching pages, or — for
// query v2 — an aggregate rollup over them.
//
// Storage is ready-made: pages.props is JSONB with a GIN jsonb_path_ops index
// (migration 0005), so `props @> $1::jsonb` containment is indexed — no schema
// change.
//
// v1 was equality/containment only, a whitelisted sort, a clamped limit. v2 adds
// (all still parameterized — see SECURITY):
//   - operator filters: >, <, >=, <=, !=, contains, exists
//   - sort by any prop, asc/desc, multi-key
//   - aggregation SUM/COUNT/AVG/MIN/MAX + optional GROUP BY
//
// SECURITY (the load-bearing invariant): every query — list OR aggregate — is
// built from ONE shared FROM/WHERE (buildFilteredFrom), which JOINs the caller's
// space_access (docs/access-model.md invariant 4). A page from a space the caller
// can't read never enters the result set, so it can neither be listed NOR
// contribute to a SUM/COUNT (an aggregate value would otherwise leak private data
// even without returning the row). Every user-supplied identifier — prop keys,
// filter values, group key — is a BIND PARAMETER, never interpolated. The only
// things interpolated are fixed whitelist fragments (comparison operators, sort
// direction, aggregate function names), each rejected if not in its map.

type pagesQueryRequest struct {
	// Where is the props containment filter (props @> where). Empty → all pages.
	// Back-compat: v1 blocks send only this.
	Where map[string]any `json:"where"`
	// Filters are operator conditions (v2), ANDed with Where and each other.
	Filters []queryFilter `json:"filters"`
	// Space scopes the query: "here" (resolve from PageID), a space id (JSON
	// number or numeric string), or absent/"all"/null → every readable space.
	Space any `json:"space"`
	// PageID is the page the block lives on — used only to resolve Space="here".
	PageID *int64 `json:"page_id"`
	// Sort is the v1 whitelisted key: (-)updated | (-)created | (-)title. Used
	// only when Order is empty (back-compat).
	Sort string `json:"sort"`
	// Order is the v2 multi-key sort (sort by any prop). Takes precedence over Sort.
	Order []querySort `json:"order"`
	// Limit caps rows on the LIST path; clamped to [1, queryMaxLimit]. Ignored for
	// aggregates.
	Limit int `json:"limit"`
	// Aggregate, when present, switches the response from a page list to a rollup.
	Aggregate *aggregateSpec `json:"aggregate"`
}

// queryFilter is one operator condition, e.g. {key:"cost", op:"gt", value:100}.
type queryFilter struct {
	Key   string `json:"key"`
	Op    string `json:"op"`
	Value any    `json:"value"`
}

// querySort is one sort term: a field (a special column or a prop key) + a
// direction. Fields: title | created | updated | space, or any prop key.
type querySort struct {
	Field string `json:"field"`
	Dir   string `json:"dir"` // asc | desc
}

// aggregateSpec is the rollup: a set of functions, optionally grouped by a prop.
type aggregateSpec struct {
	Fns     []aggFn `json:"fns"`
	GroupBy string  `json:"group_by"`
}

// aggFn is one aggregation: fn over a prop key, surfaced under alias As.
type aggFn struct {
	Fn  string `json:"fn"`  // sum | count | avg | min | max
	Key string `json:"key"` // prop key (ignored for count)
	As  string `json:"as"`  // output alias (returned as a JSON key, never in SQL)
}

type queryPageRow struct {
	ID        int64          `json:"id"`
	SpaceID   int64          `json:"space_id"`
	SpaceName string         `json:"space_name"`
	Title     string         `json:"title"`
	Props     map[string]any `json:"props"`
	CreatedAt string         `json:"created_at"`
	UpdatedAt string         `json:"updated_at"`
}

// queryGroupRow is one aggregate output row: the group value (null when there is
// no GROUP BY — a single whole-set rollup) and the aggregate values by alias.
type queryGroupRow struct {
	Key    any            `json:"key"`
	Values map[string]any `json:"values"`
}

const (
	queryDefaultLimit = 50
	queryMaxLimit     = 200
)

// querySortSQL whitelists the v1 sort key → a fixed ORDER BY fragment. A "-"
// prefix is descending. Unknown key → 400, so the fragment is never built from
// user input. The id tiebreak keeps paging stable.
var querySortSQL = map[string]string{
	"updated":  "p.updated_at ASC, p.id ASC",
	"-updated": "p.updated_at DESC, p.id DESC",
	"created":  "p.created_at ASC, p.id ASC",
	"-created": "p.created_at DESC, p.id DESC",
	"title":    "p.title ASC, p.id ASC",
	"-title":   "p.title DESC, p.id DESC",
}

// cmpOpSQL whitelists a comparison operator → its SQL token. Interpolating the
// token is safe precisely because it can only ever be one of these values.
var cmpOpSQL = map[string]string{"gt": ">", "lt": "<", "gte": ">=", "lte": "<="}

// aggFnSQL whitelists an aggregate function name → its SQL name (count handled
// separately). Same safety argument as cmpOpSQL.
var aggFnSQL = map[string]string{"sum": "sum", "avg": "avg", "min": "min", "max": "max"}

// sortFieldSQL maps a special sort field to its real column. A field not here is
// treated as a prop key (bound, never interpolated).
var sortFieldSQL = map[string]string{
	"title":   "p.title",
	"created": "p.created_at",
	"updated": "p.updated_at",
	"space":   "s.name",
}

// QueryPages handles POST /api/pages/query — session-gated; the core re-gates
// every row through space_access.
func (s *Server) QueryPages(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req pagesQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not parse request body")
		return
	}
	k, _ := auth.APIKeyFromContext(r.Context())
	if req.Aggregate != nil {
		res, ae := s.aggregatePagesCore(r.Context(), u, k, req)
		if ae != nil {
			writeError(w, ae.Status, ae.Code, ae.Message)
			return
		}
		writeJSON(w, http.StatusOK, res)
		return
	}
	rows, ae := s.queryPagesCore(r.Context(), u, k, req)
	if ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pages": rows})
}

// buildFilteredFrom assembles the shared "FROM pages … WHERE …" — the
// space_access join plus every filter, space scope, and PAT confinement — used
// identically by the list and aggregate paths. It seeds and appends to *args and
// returns the SQL fragment beginning with " FROM". The caller has already put
// u.ID at $1; this appends the containment where at $2 and everything after.
func (s *Server) buildFilteredFrom(ctx context.Context, req pagesQueryRequest, k *auth.APIKey, args *[]any) (string, *apiErr) {
	// props @> where containment. Empty where marshals to "{}", matching every
	// page (containment of the empty object is always true).
	*args = append(*args, propsJSON(req.Where))
	whereIdx := len(*args)

	var b strings.Builder
	fmt.Fprintf(&b, ` FROM pages p
	        JOIN spaces s ON s.id = p.space_id
	        JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = $1) sm
	          ON sm.space_id = p.space_id
	       WHERE p.deleted_at IS NULL
	         AND p.props @> $%d::jsonb`, whereIdx)

	// Operator filters (v2), each ANDed. Every key/value is bound.
	for _, f := range req.Filters {
		if ae := appendFilterSQL(f, args, &b); ae != nil {
			return "", ae
		}
	}

	// Space scope.
	spaceFilterID, ae := s.resolveQuerySpace(ctx, req)
	if ae != nil {
		return "", ae
	}
	if spaceFilterID != nil {
		*args = append(*args, *spaceFilterID)
		fmt.Fprintf(&b, " AND p.space_id = $%d", len(*args))
	}
	// An API-key-scoped caller (MCP PAT) is confined to its one space.
	if k != nil && k.SpaceID != nil {
		*args = append(*args, *k.SpaceID)
		fmt.Fprintf(&b, " AND p.space_id = $%d", len(*args))
	}
	return b.String(), nil
}

// appendFilterSQL renders one operator condition into b, binding its key and
// value. The operator token itself is whitelist-mapped, never interpolated raw.
func appendFilterSQL(f queryFilter, args *[]any, b *strings.Builder) *apiErr {
	key := strings.TrimSpace(f.Key)
	if key == "" {
		return &apiErr{http.StatusBadRequest, "bad_request", "filter needs a key"}
	}
	switch f.Op {
	case "exists":
		*args = append(*args, key)
		fmt.Fprintf(b, " AND p.props ? $%d", len(*args))
		return nil

	case "ne":
		// Type-aware inequality over the raw jsonb value: a number != a number, a
		// string != a string, and a present key != an absent one, all correctly.
		vb, err := json.Marshal(f.Value)
		if err != nil {
			return &apiErr{http.StatusBadRequest, "bad_request", "invalid filter value"}
		}
		*args = append(*args, key)
		ki := len(*args)
		*args = append(*args, string(vb))
		vi := len(*args)
		fmt.Fprintf(b, " AND p.props -> $%d IS DISTINCT FROM $%d::jsonb", ki, vi)
		return nil

	case "contains":
		sv, ok := f.Value.(string)
		if !ok {
			return &apiErr{http.StatusBadRequest, "bad_request", "contains value must be a string"}
		}
		*args = append(*args, key)
		ki := len(*args)
		*args = append(*args, sv)
		vi := len(*args)
		// Array prop → element membership (jsonb ?); string prop → substring. The
		// stored type decides, so `contains` reads naturally on tags or on text.
		fmt.Fprintf(b, " AND ((jsonb_typeof(p.props -> $%d) = 'array' AND p.props -> $%d ? $%d)"+
			" OR (jsonb_typeof(p.props -> $%d) = 'string' AND p.props ->> $%d ILIKE '%%' || $%d || '%%'))",
			ki, ki, vi, ki, ki, vi)
		return nil

	case "gt", "lt", "gte", "lte":
		op := cmpOpSQL[f.Op]
		switch v := f.Value.(type) {
		case float64:
			*args = append(*args, key)
			ki := len(*args)
			*args = append(*args, v)
			vi := len(*args)
			// Numeric compare, guarded so a non-numeric value at this key simply
			// doesn't match (no cast error).
			fmt.Fprintf(b, " AND jsonb_typeof(p.props -> $%d) = 'number' AND (p.props ->> $%d)::numeric %s $%d", ki, ki, op, vi)
			return nil
		case string:
			*args = append(*args, key)
			ki := len(*args)
			*args = append(*args, v)
			vi := len(*args)
			// Text compare. ISO date strings ('YYYY-MM-DD') order chronologically.
			fmt.Fprintf(b, " AND p.props ->> $%d %s $%d", ki, op, vi)
			return nil
		default:
			return &apiErr{http.StatusBadRequest, "bad_request", "comparison value must be a number or string"}
		}

	default:
		return &apiErr{http.StatusBadRequest, "bad_request", fmt.Sprintf("unsupported filter op %q", f.Op)}
	}
}

// buildOrderBy returns the ORDER BY fragment (without the "ORDER BY" keyword) for
// the list path. v2 Order (any prop, multi-key) takes precedence; otherwise the
// v1 Sort whitelist. Prop keys are bound; only the direction (a whitelisted
// keyword) is interpolated.
func buildOrderBy(req pagesQueryRequest, args *[]any) (string, *apiErr) {
	if len(req.Order) == 0 {
		sortKey := strings.TrimSpace(req.Sort)
		if sortKey == "" {
			sortKey = "-updated"
		}
		frag, ok := querySortSQL[sortKey]
		if !ok {
			return "", &apiErr{http.StatusBadRequest, "bad_request", "unsupported sort key"}
		}
		return frag, nil
	}

	var terms []string
	for _, srt := range req.Order {
		dir := "ASC"
		switch strings.ToLower(strings.TrimSpace(srt.Dir)) {
		case "", "asc":
			dir = "ASC"
		case "desc":
			dir = "DESC"
		default:
			return "", &apiErr{http.StatusBadRequest, "bad_request", "sort dir must be asc or desc"}
		}
		field := strings.TrimSpace(srt.Field)
		if col, ok := sortFieldSQL[field]; ok {
			terms = append(terms, col+" "+dir)
			continue
		}
		if field == "" {
			return "", &apiErr{http.StatusBadRequest, "bad_request", "sort needs a field"}
		}
		// A prop key: sort by the raw jsonb value, which orders correctly within a
		// consistently-typed prop (numbers numerically, strings lexically). The key
		// is bound; NULLS LAST keeps pages missing the prop at the end.
		*args = append(*args, field)
		terms = append(terms, fmt.Sprintf("p.props -> $%d %s NULLS LAST", len(*args), dir))
	}
	terms = append(terms, "p.id ASC") // stable tiebreak
	return strings.Join(terms, ", "), nil
}

func (s *Server) queryPagesCore(ctx context.Context, u *auth.User, k *auth.APIKey, req pagesQueryRequest) ([]queryPageRow, *apiErr) {
	limit := req.Limit
	if limit <= 0 {
		limit = queryDefaultLimit
	}
	if limit > queryMaxLimit {
		limit = queryMaxLimit
	}

	args := []any{u.ID}
	fromWhere, ae := s.buildFilteredFrom(ctx, req, k, &args)
	if ae != nil {
		return nil, ae
	}
	orderBy, ae := buildOrderBy(req, &args)
	if ae != nil {
		return nil, ae
	}

	q := "SELECT p.id, p.space_id, s.name, p.title, p.props, p.created_at, p.updated_at" + fromWhere +
		" ORDER BY " + orderBy
	args = append(args, limit)
	q += fmt.Sprintf(" LIMIT $%d", len(args))

	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, &apiErr{http.StatusInternalServerError, "internal", "query pages failed"}
	}
	defer rows.Close()

	out := []queryPageRow{}
	for rows.Next() {
		var row queryPageRow
		var propsRaw []byte
		if err := rows.Scan(&row.ID, &row.SpaceID, &row.SpaceName, &row.Title, &propsRaw, &row.CreatedAt, &row.UpdatedAt); err != nil {
			return nil, &apiErr{http.StatusInternalServerError, "internal", "scan query row failed"}
		}
		row.Props = map[string]any{}
		if len(propsRaw) > 0 {
			if err := json.Unmarshal(propsRaw, &row.Props); err != nil {
				return nil, &apiErr{http.StatusInternalServerError, "internal", "decode props failed"}
			}
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, &apiErr{http.StatusInternalServerError, "internal", "query pages iterate failed"}
	}
	return out, nil
}

// aggregatePagesCore runs the rollup over the SAME access-filtered set as the
// list path. Output is a set of group rows (one whole-set row when no GROUP BY),
// plus a count of rows excluded from a numeric aggregate because their value at
// an aggregated key was not a number — surfaced, never silent.
func (s *Server) aggregatePagesCore(ctx context.Context, u *auth.User, k *auth.APIKey, req pagesQueryRequest) (map[string]any, *apiErr) {
	agg := req.Aggregate
	if len(agg.Fns) == 0 {
		return nil, &apiErr{http.StatusBadRequest, "bad_request", "aggregate needs at least one function"}
	}

	args := []any{u.ID}
	fromWhere, ae := s.buildFilteredFrom(ctx, req, k, &args)
	if ae != nil {
		return nil, ae
	}

	// SELECT list. Column 0 is the group key (NULL when no GROUP BY). Columns
	// 1..N are the aggregates, in Fns order, mapped back to aliases in Go — the
	// alias never touches SQL, so it needs no sanitizing.
	sel := []string{}
	groupBy := strings.TrimSpace(agg.GroupBy)
	if groupBy != "" {
		args = append(args, groupBy)
		sel = append(sel, fmt.Sprintf("(p.props ->> $%d)", len(args)))
	} else {
		sel = append(sel, "NULL::text")
	}

	aliases := make([]string, len(agg.Fns))
	var numericKeys []string // keys of numeric aggregates, for the skipped count
	for i, fn := range agg.Fns {
		alias := strings.TrimSpace(fn.As)
		if alias == "" {
			alias = fn.Fn // default alias; still returned as a map key, never SQL
		}
		aliases[i] = alias

		if fn.Fn == "count" {
			// count = rows in the group. The key is intentionally ignored.
			sel = append(sel, "count(*)")
			continue
		}
		sqlFn, ok := aggFnSQL[fn.Fn]
		if !ok {
			return nil, &apiErr{http.StatusBadRequest, "bad_request", fmt.Sprintf("unsupported aggregate fn %q", fn.Fn)}
		}
		key := strings.TrimSpace(fn.Key)
		if key == "" {
			return nil, &apiErr{http.StatusBadRequest, "bad_request", fmt.Sprintf("%s needs a key", fn.Fn)}
		}
		numericKeys = append(numericKeys, key)
		args = append(args, key)
		ki := len(args)
		// FILTER to numeric values so a stray non-number never errors the cast; the
		// excluded rows are counted separately below.
		sel = append(sel, fmt.Sprintf("%s((p.props ->> $%d)::numeric) FILTER (WHERE jsonb_typeof(p.props -> $%d) = 'number')", sqlFn, ki, ki))
	}

	q := "SELECT " + strings.Join(sel, ", ") + fromWhere
	if groupBy != "" {
		q += " GROUP BY 1 ORDER BY 1 NULLS LAST"
	}

	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, &apiErr{http.StatusInternalServerError, "internal", "aggregate query failed"}
	}
	defer rows.Close()

	groups := []queryGroupRow{}
	for rows.Next() {
		// Scan the group key + one nullable float per aggregate.
		vals := make([]any, len(agg.Fns)+1)
		var groupKey sql.NullString
		vals[0] = &groupKey
		floats := make([]sql.NullFloat64, len(agg.Fns))
		for i := range floats {
			vals[i+1] = &floats[i]
		}
		if err := rows.Scan(vals...); err != nil {
			return nil, &apiErr{http.StatusInternalServerError, "internal", "scan aggregate row failed"}
		}
		gr := queryGroupRow{Values: map[string]any{}}
		if groupKey.Valid {
			gr.Key = groupKey.String
		} else {
			gr.Key = nil
		}
		for i, alias := range aliases {
			if floats[i].Valid {
				gr.Values[alias] = floats[i].Float64
			} else {
				gr.Values[alias] = nil
			}
		}
		groups = append(groups, gr)
	}
	if err := rows.Err(); err != nil {
		return nil, &apiErr{http.StatusInternalServerError, "internal", "aggregate iterate failed"}
	}

	skipped, ae := s.countNonNumeric(ctx, u, k, req, numericKeys)
	if ae != nil {
		return nil, ae
	}
	return map[string]any{"groups": groups, "skipped_non_numeric": skipped}, nil
}

// countNonNumeric counts pages in the same access-filtered set that carry a
// present-but-non-numeric value under at least one aggregated key. This is the
// honest denominator: "N of your rows had non-numeric data where a number was
// expected and were left out of those aggregates." Returns 0 when there are no
// numeric aggregates.
func (s *Server) countNonNumeric(ctx context.Context, u *auth.User, k *auth.APIKey, req pagesQueryRequest, keys []string) (int, *apiErr) {
	if len(keys) == 0 {
		return 0, nil
	}
	args := []any{u.ID}
	fromWhere, ae := s.buildFilteredFrom(ctx, req, k, &args)
	if ae != nil {
		return 0, ae
	}
	var conds []string
	for _, key := range keys {
		args = append(args, key)
		ki := len(args)
		conds = append(conds, fmt.Sprintf("(p.props ? $%d AND jsonb_typeof(p.props -> $%d) <> 'number')", ki, ki))
	}
	q := "SELECT count(*)" + fromWhere + " AND (" + strings.Join(conds, " OR ") + ")"
	var n int
	if err := s.DB.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
		return 0, &apiErr{http.StatusInternalServerError, "internal", "count non-numeric failed"}
	}
	return n, nil
}

// resolveQuerySpace turns the request's Space field into an optional space-id
// filter: nil means "every readable space" (the space_access join still gates).
func (s *Server) resolveQuerySpace(ctx context.Context, req pagesQueryRequest) (*int64, *apiErr) {
	switch v := req.Space.(type) {
	case nil:
		return nil, nil
	case float64: // JSON numbers decode to float64
		id := int64(v)
		return &id, nil
	case int64: // an in-process caller (MCP query_pages) passes a native id
		return &v, nil
	case string:
		sv := strings.TrimSpace(strings.ToLower(v))
		switch {
		case sv == "" || sv == "all":
			return nil, nil
		case sv == "here":
			if req.PageID == nil {
				return nil, &apiErr{http.StatusBadRequest, "bad_request", "space: here needs a page context"}
			}
			var sid int64
			err := s.DB.QueryRowContext(ctx,
				`SELECT space_id FROM pages WHERE id = $1 AND deleted_at IS NULL`, *req.PageID).Scan(&sid)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, &apiErr{http.StatusNotFound, "not_found", "page not found"}
			}
			if err != nil {
				return nil, &apiErr{http.StatusInternalServerError, "internal", "resolve space failed"}
			}
			return &sid, nil
		default:
			if id, perr := strconv.ParseInt(sv, 10, 64); perr == nil {
				return &id, nil
			}
			return nil, &apiErr{http.StatusBadRequest, "bad_request", "invalid space"}
		}
	default:
		return nil, &apiErr{http.StatusBadRequest, "bad_request", "invalid space"}
	}
}
