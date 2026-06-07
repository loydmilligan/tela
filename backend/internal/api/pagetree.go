package api

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/zcag/tela/backend/internal/models"
)

// pagetree is the shared file-layout model for a space's page tree: the
// sibling-folder convention used identically by the zip export (md_export.go)
// and the WebDAV FileSystem (dav_fs.go). A page is `<slug>.md`; a page that has
// children is ALSO a `<slug>/` directory holding them. Keeping this in one place
// is what guarantees `export.zip` and the live WebDAV surface lay bytes out the
// same way — a file pulled over rclone round-trips to the same page.

// siblingSlugs assigns each page in a sibling group its on-disk filename slug
// (without the .md extension), deduplicating collisions deterministically with
// -2, -3… in the slice's order. The slice MUST already be in stable sibling
// order (position ASC, id ASC) so the assignment is reproducible across the
// export walk, a PROPFIND listing, and the reverse path→page resolution.
func siblingSlugs(siblings []models.Page) map[int64]string {
	used := make(map[string]bool, len(siblings))
	out := make(map[int64]string, len(siblings))
	for _, p := range siblings {
		base := mdSlugOr(p.Title, fmt.Sprintf("page-%d", p.ID))
		slug := base
		for n := 2; used[slug]; n++ {
			slug = fmt.Sprintf("%s-%d", base, n)
		}
		used[slug] = true
		out[p.ID] = slug
	}
	return out
}

// spaceTree is an in-memory view of one space's live page tree, built from a
// single query, that answers the two questions the WebDAV layer asks per
// request: "what is the on-disk name of each page" (siblingSlugs, per group)
// and "which page does this path name". Loading the whole space once and
// walking in memory keeps PROPFIND a single indexed query with no N+1.
type spaceTree struct {
	spaceID  int64
	children map[int64][]models.Page // parent id → ordered children; rootParentKey = roots
	slug     map[int64]string        // page id → deduped filename slug (within its sibling group)
}

// rootParentKey is the children-map key for a space's top-level pages (those
// with a NULL parent_id). 0 is never a real page id, so it can't collide.
const rootParentKey int64 = 0

func newSpaceTree(ctx context.Context, db *sql.DB, spaceID int64) (*spaceTree, error) {
	pages, err := loadSpacePages(ctx, db, spaceID)
	if err != nil {
		return nil, err
	}
	t := &spaceTree{spaceID: spaceID, children: map[int64][]models.Page{}, slug: map[int64]string{}}
	for _, p := range pages {
		key := rootParentKey
		if p.ParentID != nil {
			key = *p.ParentID
		}
		t.children[key] = append(t.children[key], p)
	}
	// loadSpacePages already orders by position, id, so each group is in stable
	// sibling order; assign deduped slugs per group.
	for _, group := range t.children {
		for id, s := range siblingSlugs(group) {
			t.slug[id] = s
		}
	}
	return t, nil
}

// childBySlug finds the child of parent whose deduped filename slug matches
// slug. parentKey is rootParentKey for the space root.
func (t *spaceTree) childBySlug(parentKey int64, slug string) (models.Page, bool) {
	for _, c := range t.children[parentKey] {
		if t.slug[c.ID] == slug {
			return c, true
		}
	}
	return models.Page{}, false
}

func (t *spaceTree) hasChildren(pageID int64) bool { return len(t.children[pageID]) > 0 }

// resolve walks a slash-split path tail (the segments BELOW the space dir) to
// the page it names. A trailing segment ending in `.md` is a page FILE; any
// other trailing segment, and every interior segment, is a page DIRECTORY (the
// page-as-collection). isFile distinguishes the `<slug>.md` body resource from
// the `<slug>/` children collection — both name the same page. A `.md` segment
// anywhere but the end is malformed and resolves to !ok.
func (t *spaceTree) resolve(segs []string) (page models.Page, isFile, ok bool) {
	parent := rootParentKey
	for i, seg := range segs {
		last := i == len(segs)-1
		name, file := seg, false
		if strings.HasSuffix(seg, ".md") {
			if !last {
				return models.Page{}, false, false // a file cannot have children
			}
			name, file = strings.TrimSuffix(seg, ".md"), true
		}
		p, found := t.childBySlug(parent, name)
		if !found {
			return models.Page{}, false, false
		}
		if last {
			return p, file, true
		}
		parent = p.ID
	}
	return models.Page{}, false, false // empty segs: caller handles the space root
}
