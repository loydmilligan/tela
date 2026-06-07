package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/zcag/tela/backend/internal/models"
	"github.com/zcag/tela/backend/internal/pagemd"
)

// Public-space read API. A space with visibility='public' is readable by anyone
// with no login — the blog surface (docs/public-spaces.md). These routes live
// under /api/public/ and are on auth.IsPublicPath, so the session middleware
// skips them; each handler self-authenticates by checking the space is public.
//
// READ-ONLY by construction: every handler is a GET that only ever selects, and
// publicness adds nobody to space_access — so writes stay gated on membership in
// the normal /api/ routes and can never flow through here. The projection is
// deliberately narrow (title/body/props/tree) — no comments, history, members,
// or cross-space data leak out.

const (
	spaceVisibilityPrivate = "private"
	spaceVisibilityPublic  = "public"
)

// publicSpaceDTO is the minimal space envelope a logged-out reader gets.
type publicSpaceDTO struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Slug       string `json:"slug"`
	Visibility string `json:"visibility"`
}

// publicPageDTO is the read-only page projection for a public space — body +
// the public-by-design frontmatter (props), nothing internal.
type publicPageDTO struct {
	ID        int64          `json:"id"`
	Title     string         `json:"title"`
	Body      string         `json:"body"`
	Props     map[string]any `json:"props,omitempty"`
	UpdatedAt string         `json:"updated_at"`
}

// publicTreeNode is one slim nav entry for the public space's page tree. Carries
// updated_at so the front-page index can show post dates.
type publicTreeNode struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	ParentID  *int64 `json:"parent_id"`
	Position  int64  `json:"position"`
	UpdatedAt string `json:"updated_at"`
}

// requirePublicSpace loads the space and returns it only when it is public.
// A private or missing space is reported identically (404) so the endpoint never
// reveals whether a private space id exists.
func (s *Server) requirePublicSpace(w http.ResponseWriter, r *http.Request, id int64) (models.Space, bool) {
	sp, err := selectSpaceByID(r.Context(), s.DB, id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && sp.Visibility != spaceVisibilityPublic) {
		writeError(w, http.StatusNotFound, "not_found", "no such public space")
		return models.Space{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup space failed")
		return models.Space{}, false
	}
	return sp, true
}

// GetPublicSpace — GET /api/public/spaces/{id}. Space envelope for a public
// space; 404 otherwise.
func (s *Server) GetPublicSpace(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	sp, ok := s.requirePublicSpace(w, r, id)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"space": publicSpaceDTO{ID: sp.ID, Name: sp.Name, Slug: sp.Slug, Visibility: sp.Visibility},
	})
}

// GetPublicSpaceTree — GET /api/public/spaces/{id}/tree. The whole space's page
// tree as a flat array (id, title, parent_id, position) for a public sidebar.
func (s *Server) GetPublicSpaceTree(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	if _, ok := s.requirePublicSpace(w, r, id); !ok {
		return
	}
	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT id, title, parent_id, position, updated_at
		   FROM pages
		  WHERE space_id = $1 AND deleted_at IS NULL
		  ORDER BY position ASC, id ASC`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "load tree failed")
		return
	}
	defer rows.Close()
	nodes := []publicTreeNode{}
	for rows.Next() {
		var n publicTreeNode
		if err := rows.Scan(&n.ID, &n.Title, &n.ParentID, &n.Position, &n.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan tree row failed")
			return
		}
		nodes = append(nodes, n)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate tree failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pages": nodes})
}

// publicSpacePage loads a page that belongs to a public space, or reports 404.
// Centralises the "space public + page in space + not deleted" gate shared by
// the JSON and markdown reads.
func (s *Server) publicSpacePage(w http.ResponseWriter, r *http.Request) (models.Page, bool) {
	spaceID, ok := parseIDParam(w, r, "id")
	if !ok {
		return models.Page{}, false
	}
	if _, ok := s.requirePublicSpace(w, r, spaceID); !ok {
		return models.Page{}, false
	}
	pageID, ok := parseIDParam(w, r, "page_id")
	if !ok {
		return models.Page{}, false
	}
	page, err := selectPageByID(r.Context(), s.DB, pageID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && page.SpaceID != spaceID) {
		// Page missing, deleted, or in a different space — never confirm it
		// exists outside the public space being read.
		writeError(w, http.StatusNotFound, "not_found", "no such page in this space")
		return models.Page{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup page failed")
		return models.Page{}, false
	}
	return page, true
}

// GetPublicSpacePage — GET /api/public/spaces/{id}/pages/{page_id}. Read-only
// page body + public frontmatter for a public space.
func (s *Server) GetPublicSpacePage(w http.ResponseWriter, r *http.Request) {
	page, ok := s.publicSpacePage(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"page": publicPageDTO{
			ID:        page.ID,
			Title:     page.Title,
			Body:      page.Body,
			Props:     page.Props,
			UpdatedAt: page.UpdatedAt,
		},
	})
}

// ExportPublicSpacePageMarkdown — GET /api/public/spaces/{id}/pages/{page_id}/md.
// The page's full canonical markdown (frontmatter + body, via pagemd.Encode),
// served inline (not an attachment) so it reads in a browser/agent. Public,
// no login — the "add .md to a public page's address" affordance.
func (s *Server) ExportPublicSpacePageMarkdown(w http.ResponseWriter, r *http.Request) {
	page, ok := s.publicSpacePage(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Inline, slugged filename so a manual save is sensibly named without
	// forcing a download on a normal browse.
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", mdSlugOr(page.Title, "page")+".md"))
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(pagemd.Encode(page, publicBaseURL()))
}
