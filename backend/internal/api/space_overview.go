package api

import (
	"context"
	"net/http"
	"time"

	"github.com/zcag/tela/backend/internal/auth"
	"github.com/zcag/tela/backend/internal/rag"
)

// Per-space view (GET /api/spaces/{id}/overview): a content-first space hub plus a
// maintenance/health rollup, behind one read so the tabbed landing fetches once.
// Read-only and access-gated like GetSpace. All of it is derived from data tela
// already keeps (page tree, page_links, page_agreement, props, embeddings).

type spaceTopPage struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Children  int    `json:"children"`
	UpdatedAt string `json:"updated_at"`
}

type spaceMiniPage struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type spaceDisputed struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	N     int    `json:"n"`
}

type spaceReviewPage struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	AgeDays int    `json:"age_days"`
	Every   int    `json:"every"`
}

type spaceHealth struct {
	Disputed      []spaceDisputed   `json:"disputed"`
	ReviewOverdue []spaceReviewPage `json:"review_overdue"`
	Orphans       []spaceMiniPage   `json:"orphans"`
	Duplicates    []rag.OverlapPair `json:"duplicates"`
}

type spaceOverviewOut struct {
	Pages    int             `json:"pages"`
	TopLevel []spaceTopPage  `json:"top_level"`
	Recent   []spaceMiniPage `json:"recent"`
	Health   spaceHealth     `json:"health"`
}

// SpaceOverview handles GET /api/spaces/{id}/overview.
func (s *Server) SpaceOverview(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	k, _ := auth.APIKeyFromContext(r.Context())
	if _, ae := s.getSpaceCore(r.Context(), u, k, id); ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	ctx := r.Context()
	out := spaceOverviewOut{TopLevel: []spaceTopPage{}, Recent: []spaceMiniPage{}}

	_ = s.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM pages WHERE space_id = $1 AND deleted_at IS NULL`, id).Scan(&out.Pages)

	// Contents — top-level pages with their child counts (the "what's in here").
	if rows, err := s.DB.QueryContext(ctx, `
		SELECT p.id, p.title, p.updated_at,
		       (SELECT count(*) FROM pages c WHERE c.parent_id = p.id AND c.deleted_at IS NULL)
		  FROM pages p
		 WHERE p.space_id = $1 AND p.parent_id IS NULL AND p.deleted_at IS NULL
		 ORDER BY p.position, p.id`, id); err == nil {
		for rows.Next() {
			var t spaceTopPage
			if rows.Scan(&t.ID, &t.Title, &t.UpdatedAt, &t.Children) == nil {
				out.TopLevel = append(out.TopLevel, t)
			}
		}
		rows.Close()
	}

	// Recently updated.
	if rows, err := s.DB.QueryContext(ctx, `
		SELECT id, title, updated_at FROM pages
		 WHERE space_id = $1 AND deleted_at IS NULL
		 ORDER BY updated_at DESC LIMIT 8`, id); err == nil {
		for rows.Next() {
			var p spaceMiniPage
			if rows.Scan(&p.ID, &p.Title, &p.UpdatedAt) == nil {
				out.Recent = append(out.Recent, p)
			}
		}
		rows.Close()
	}

	out.Health = s.spaceHealth(ctx, u.ID, id)
	writeJSON(w, http.StatusOK, out)
}

// spaceHealth gathers the per-space maintenance worklist.
func (s *Server) spaceHealth(ctx context.Context, userID, spaceID int64) spaceHealth {
	h := spaceHealth{Disputed: []spaceDisputed{}, ReviewOverdue: []spaceReviewPage{}, Orphans: []spaceMiniPage{}, Duplicates: []rag.OverlapPair{}}

	// Disputed — pages the agreement worker flagged (cached, clean rows only).
	if rows, err := s.DB.QueryContext(ctx, `
		SELECT p.id, p.title, a.dispute
		  FROM page_agreement a
		  JOIN pages p ON p.id = a.page_id AND p.deleted_at IS NULL
		 WHERE p.space_id = $1 AND a.last_error = '' AND a.dispute > 0
		 ORDER BY a.dispute DESC, p.title LIMIT 50`, spaceID); err == nil {
		for rows.Next() {
			var d spaceDisputed
			if rows.Scan(&d.ID, &d.Title, &d.N) == nil {
				h.Disputed = append(h.Disputed, d)
			}
		}
		rows.Close()
	}

	// Needs review — pages with a review_every_days cadence they're now past.
	// Filter the candidates in SQL (cheap: few pages declare it), compute the age
	// in Go to dodge timestamp-arithmetic fragility.
	if rows, err := s.DB.QueryContext(ctx, `
		SELECT id, title, updated_at, (props->>'review_every_days')
		  FROM pages
		 WHERE space_id = $1 AND deleted_at IS NULL
		   AND props ? 'review_every_days'
		   AND (props->>'review_every_days') ~ '^[0-9]+$'`, spaceID); err == nil {
		for rows.Next() {
			var p spaceReviewPage
			var updated, every string
			if rows.Scan(&p.ID, &p.Title, &updated, &every) != nil {
				continue
			}
			p.Every = atoiSafe(every)
			p.AgeDays = ageDaysFromTs(updated)
			if p.Every > 0 && p.AgeDays > p.Every {
				h.ReviewOverdue = append(h.ReviewOverdue, p)
			}
		}
		rows.Close()
	}

	// Orphans — truly isolated pages: no wikilink in/out, no parent, no children
	// (matches the graph's orphan definition).
	if rows, err := s.DB.QueryContext(ctx, `
		SELECT p.id, p.title FROM pages p
		 WHERE p.space_id = $1 AND p.deleted_at IS NULL AND p.parent_id IS NULL
		   AND NOT EXISTS (SELECT 1 FROM page_links l WHERE l.source_id = p.id OR l.target_id = p.id)
		   AND NOT EXISTS (SELECT 1 FROM pages c WHERE c.parent_id = p.id AND c.deleted_at IS NULL)
		 ORDER BY p.updated_at DESC LIMIT 50`, spaceID); err == nil {
		for rows.Next() {
			var p spaceMiniPage
			if rows.Scan(&p.ID, &p.Title) == nil {
				h.Orphans = append(h.Orphans, p)
			}
		}
		rows.Close()
	}

	// Possible duplicates — near-dup page pairs within this space (best-effort;
	// needs stored embeddings, empty otherwise).
	if dups, err := s.rag.FindOverlaps(ctx, userID, &spaceID, 0, 10); err == nil {
		h.Duplicates = dups
	}
	return h
}

func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func ageDaysFromTs(ts string) int {
	t, err := time.Parse("2006-01-02 15:04:05", ts)
	if err != nil {
		return 0
	}
	d := int(time.Since(t).Hours() / 24)
	if d < 0 {
		return 0
	}
	return d
}
