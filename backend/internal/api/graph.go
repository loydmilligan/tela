package api

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"

	"github.com/zcag/tela/backend/internal/auth"
)

// Graph view data: the pages the caller can see (nodes) plus the connections
// between them (links). Two edge kinds: "link" = a wikilink/reference from
// page_links, "tree" = a parent→child hierarchy edge. Only edges where BOTH
// endpoints are visible to the caller are returned — no cross-membership
// leakage, same scoping as ListAllPages / backlinks. Degree is left to the
// frontend (trivial to derive from links).

type graphNode struct {
	ID         int64    `json:"id"`
	SpaceID    int64    `json:"space_id"`
	SpaceName  string   `json:"space_name"`
	Title      string   `json:"title"`
	Breadcrumb []string `json:"breadcrumb"`
	UpdatedAt  string   `json:"updated_at"`
	// Count of outgoing wikilinks whose target page no longer exists (recorded
	// in page_links with a last_known_title). Powers broken-link surfacing.
	Broken int `json:"broken"`
}

type graphLink struct {
	Source int64  `json:"source"`
	Target int64  `json:"target"`
	Kind   string `json:"kind"` // "link" | "tree" | "semantic"
	// Cosine similarity in [0,1], present only on "semantic" edges (drives edge
	// weight/styling in the view). Omitted for authored link/tree edges.
	Similarity float64 `json:"similarity,omitempty"`
}

// GraphData backs GET /api/graph. A space-pinned bearer key narrows the graph to
// its own space (mirrors ListAllPages); otherwise it spans every space the user
// belongs to.
func (s *Server) GraphData(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	// Optional space pin: bearer key restriction, OR an explicit ?space_id= to
	// scope the view to one space from the UI.
	var pinSpace *int64
	if k, isBearer := auth.APIKeyFromContext(ctx); isBearer && k.SpaceID != nil {
		pinSpace = k.SpaceID
	}
	if q := r.URL.Query().Get("space_id"); q != "" {
		if id, err := strconv.ParseInt(q, 10, 64); err == nil && id > 0 {
			// A UI-supplied space pin can only narrow further, never widen past
			// the bearer pin.
			if pinSpace == nil || *pinSpace == id {
				pinSpace = &id
			}
		}
	}

	// --- nodes -------------------------------------------------------------
	var (
		nodeRows *sql.Rows
		err      error
	)
	if pinSpace != nil {
		nodeRows, err = s.DB.QueryContext(ctx, `
			SELECT p.id, p.space_id, s.name, p.title, p.updated_at
			  FROM pages p
			  JOIN spaces s ON s.id = p.space_id
			  JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = $1) sm ON sm.space_id = p.space_id
			 WHERE p.space_id = $2 AND p.deleted_at IS NULL`, u.ID, *pinSpace)
	} else {
		nodeRows, err = s.DB.QueryContext(ctx, `
			SELECT p.id, p.space_id, s.name, p.title, p.updated_at
			  FROM pages p
			  JOIN spaces s ON s.id = p.space_id
			  JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = $1) sm ON sm.space_id = p.space_id
			 WHERE p.deleted_at IS NULL`, u.ID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list graph nodes failed")
		return
	}
	defer nodeRows.Close()

	nodes := []graphNode{}
	for nodeRows.Next() {
		var n graphNode
		if err := nodeRows.Scan(&n.ID, &n.SpaceID, &n.SpaceName, &n.Title, &n.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan graph node failed")
			return
		}
		nodes = append(nodes, n)
	}
	if err := nodeRows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate graph nodes failed")
		return
	}

	// Broken outgoing-link counts: page_links rows whose target page no longer
	// exists. Scoped to visible source pages (+ pin); attached to the nodes.
	brokenSQL := `
		SELECT l.source_id, COUNT(*)
		  FROM page_links l
		  JOIN pages ps ON ps.id = l.source_id AND ps.deleted_at IS NULL
		  JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = $1) s1 ON s1.space_id = ps.space_id
		  LEFT JOIN pages pt ON pt.id = l.target_id AND pt.deleted_at IS NULL
		 WHERE pt.id IS NULL`
	var brokenRows *sql.Rows
	if pinSpace != nil {
		brokenRows, err = s.DB.QueryContext(ctx, brokenSQL+` AND ps.space_id = $2 GROUP BY l.source_id`, u.ID, *pinSpace)
	} else {
		brokenRows, err = s.DB.QueryContext(ctx, brokenSQL+` GROUP BY l.source_id`, u.ID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list broken links failed")
		return
	}
	defer brokenRows.Close()
	broken := map[int64]int{}
	for brokenRows.Next() {
		var src int64
		var n int
		if err := brokenRows.Scan(&src, &n); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan broken link failed")
			return
		}
		broken[src] = n
	}
	if err := brokenRows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate broken links failed")
		return
	}
	for i := range nodes {
		nodes[i].Broken = broken[nodes[i].ID]
		bc, err := pageBreadcrumb(ctx, s.DB, nodes[i].ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "build breadcrumb failed")
			return
		}
		nodes[i].Breadcrumb = bc
	}

	// --- link edges (page_links), both endpoints visible -------------------
	links := []graphLink{}
	linkRows, err := s.queryGraphEdges(ctx, u.ID, pinSpace, `
		SELECT l.source_id, l.target_id
		  FROM page_links l
		  JOIN pages ps ON ps.id = l.source_id AND ps.deleted_at IS NULL
		  JOIN pages pt ON pt.id = l.target_id AND pt.deleted_at IS NULL
		  JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = $1) s1 ON s1.space_id = ps.space_id
		  JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = $1) s2 ON s2.space_id = pt.space_id`,
		` WHERE ps.space_id = $2 AND pt.space_id = $2`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list graph links failed")
		return
	}
	defer linkRows.Close()
	for linkRows.Next() {
		var e graphLink
		if err := linkRows.Scan(&e.Source, &e.Target); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan graph link failed")
			return
		}
		e.Kind = "link"
		links = append(links, e)
	}
	if err := linkRows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate graph links failed")
		return
	}

	// --- tree edges (parent→child), both endpoints visible -----------------
	treeRows, err := s.queryGraphEdges(ctx, u.ID, pinSpace, `
		SELECT pp.id, p.id
		  FROM pages p
		  JOIN pages pp ON pp.id = p.parent_id AND pp.deleted_at IS NULL
		  JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = $1) s1 ON s1.space_id = p.space_id AND p.deleted_at IS NULL
		  JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = $1) s2 ON s2.space_id = pp.space_id`,
		` WHERE p.space_id = $2`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list graph tree failed")
		return
	}
	defer treeRows.Close()
	for treeRows.Next() {
		var e graphLink
		if err := treeRows.Scan(&e.Source, &e.Target); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan graph tree failed")
			return
		}
		e.Kind = "tree"
		links = append(links, e)
	}
	if err := treeRows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate graph tree failed")
		return
	}

	// --- semantic edges (embedding kNN), opt-in via ?semantic=1 ------------
	// Best-effort overlay: computed from stored vectors (no live embedder). On a
	// corpus with no embeddings it's simply empty, and any failure is swallowed so
	// the authored graph still renders — semantic edges are an enhancement, never a
	// precondition. k / threshold are tunable from the query for live calibration.
	if q := r.URL.Query(); q.Get("semantic") == "1" {
		k, _ := strconv.Atoi(q.Get("k"))
		threshold, _ := strconv.ParseFloat(q.Get("threshold"), 64)
		edges, serr := s.rag.SemanticEdges(ctx, u.ID, pinSpace, k, threshold, 0)
		if serr == nil {
			for _, e := range edges {
				links = append(links, graphLink{
					Source: e.Source, Target: e.Target,
					Kind: "semantic", Similarity: e.Similarity,
				})
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes, "links": links})
}

// queryGraphEdges runs an edge query, appending the space-pin clause + binding
// $2 when a pin is set. base must use $1 for the user id; pinClause must use $2.
func (s *Server) queryGraphEdges(ctx context.Context, userID int64, pin *int64, base, pinClause string) (*sql.Rows, error) {
	if pin != nil {
		return s.DB.QueryContext(ctx, base+pinClause, userID, *pin)
	}
	return s.DB.QueryContext(ctx, base, userID)
}
