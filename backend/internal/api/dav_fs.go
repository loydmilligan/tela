package api

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/zcag/tela/backend/internal/models"
)

// dav_fs.go — the webdav.FileSystem over tela pages. The tree is virtual: a
// space is a top-level folder, a page is `<slug>.md`, and a page with children
// is ALSO a `<slug>/` folder (the sibling-folder layout in pagetree.go, shared
// with export.zip). Identity is never the path: writes flow through
// ApplyFileSync, which binds by frontmatter `id` (path is only placement).
//
// All per-op state (the authed principal + a memoised per-space tree cache) rides
// the request context as a *davReqState, so a single PROPFIND of a space is one
// indexed query, not one-per-node (the perf bar). The davFS value itself is
// stateless and shared across requests.

type davFS struct{ s *Server }

var (
	_ os.FileInfo = (*davInfo)(nil)
)

// davSplit cleans a prefix-stripped webdav name into path segments, rejecting
// traversal and empty interior segments. The root ("/" or "") yields nil, true.
func davSplit(name string) (segs []string, ok bool) {
	name = strings.Trim(name, "/")
	if name == "" {
		return nil, true
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return nil, false
		}
		segs = append(segs, seg)
	}
	return segs, true
}

func isMarkdownName(name string) bool {
	l := strings.ToLower(name)
	return strings.HasSuffix(l, ".md") || strings.HasSuffix(l, ".markdown")
}

// davTitleFromName strips a markdown extension to recover the human title a file
// rename implies. Used only by Rename (a `.md` rename = retitle).
func davTitleFromName(name string) string {
	l := strings.ToLower(name)
	for _, ext := range []string{".md", ".markdown"} {
		if strings.HasSuffix(l, ext) {
			return name[:len(name)-len(ext)]
		}
	}
	return name
}

func parentKey(parentID *int64) int64 {
	if parentID == nil {
		return rootParentKey
	}
	return *parentID
}

// davMapErr turns a page-core *apiErr into the error shape webdav maps to a
// status: not-found → 404/409, forbidden/unauthorized → permission, else a
// generic 500. nil passes through.
func davMapErr(ae *apiErr) error {
	if ae == nil {
		return nil
	}
	switch ae.Status {
	case http.StatusNotFound:
		return os.ErrNotExist
	case http.StatusForbidden, http.StatusUnauthorized:
		return os.ErrPermission
	default:
		return errors.New(ae.Message)
	}
}

// space resolves a top-level folder name to a space the principal can reach.
func (fs *davFS) space(ctx context.Context, folder string) (davSpace, bool, error) {
	spaces, err := davStateFrom(ctx).spaces(ctx, fs.s.DB)
	if err != nil {
		return davSpace{}, false, err
	}
	for _, s := range spaces {
		if s.folder == folder {
			return s, true, nil
		}
	}
	return davSpace{}, false, nil
}

func (fs *davFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	segs, ok := davSplit(name)
	if !ok {
		return nil, os.ErrNotExist
	}
	if len(segs) == 0 {
		return dirInfo("/", time.Unix(0, 0).UTC()), nil
	}
	sp, found, err := fs.space(ctx, segs[0])
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, os.ErrNotExist
	}
	if len(segs) == 1 {
		return dirInfo(sp.folder, davModTime(sp.updatedAt)), nil
	}
	t, err := davStateFrom(ctx).tree(ctx, fs.s.DB, sp.id)
	if err != nil {
		return nil, err
	}
	p, isFile, found := t.resolve(segs[1:])
	if !found {
		return nil, os.ErrNotExist
	}
	leaf := segs[len(segs)-1]
	if isFile {
		return pageFileInfo(leaf, p), nil
	}
	// Directory form: a page-as-collection is statable for ANY page (so MKCOL /
	// PUT into a leaf that is gaining its first child resolves), independent of
	// whether it currently has children.
	return dirInfo(leaf, davModTime(p.UpdatedAt)), nil
}

func (fs *davFS) OpenFile(ctx context.Context, name string, flag int, _ os.FileMode) (webdavFile, error) {
	segs, ok := davSplit(name)
	if !ok {
		return nil, os.ErrNotExist
	}
	if flag&(os.O_WRONLY|os.O_RDWR) != 0 {
		return fs.openWrite(ctx, segs)
	}
	return fs.openRead(ctx, segs)
}

func (fs *davFS) openRead(ctx context.Context, segs []string) (webdavFile, error) {
	st := davStateFrom(ctx)
	if len(segs) == 0 { // root: spaces as folders
		spaces, err := st.spaces(ctx, fs.s.DB)
		if err != nil {
			return nil, err
		}
		kids := make([]os.FileInfo, 0, len(spaces))
		for _, s := range spaces {
			kids = append(kids, dirInfo(s.folder, davModTime(s.updatedAt)))
		}
		return &davDir{info: dirInfo("/", time.Unix(0, 0).UTC()), children: kids}, nil
	}
	sp, found, err := fs.space(ctx, segs[0])
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, os.ErrNotExist
	}
	t, err := st.tree(ctx, fs.s.DB, sp.id)
	if err != nil {
		return nil, err
	}
	if len(segs) == 1 { // space folder: its root pages
		return fs.dirFromChildren(sp.folder, davModTime(sp.updatedAt), t, rootParentKey), nil
	}
	p, isFile, found := t.resolve(segs[1:])
	if !found {
		return nil, os.ErrNotExist
	}
	leaf := segs[len(segs)-1]
	if isFile {
		// The client is downloading this page → it becomes its merge base for the
		// next edit (spec §5). Best-effort; a failed base write only costs the next
		// sync a re-establish.
		if pr := davPrincipalFrom(ctx); pr.k != nil {
			if err := upsertSyncBase(ctx, fs.s.DB, pr.k.ID, p.ID, p.Title, p.Body, p.Props); err != nil {
				log.Printf("dav: sync base upsert on read (page %d): %v", p.ID, err)
			}
		}
		return newDavReadFile(leaf, p), nil
	}
	return fs.dirFromChildren(leaf, davModTime(p.UpdatedAt), t, p.ID), nil
}

// dirFromChildren builds a collection whose entries are the page tree's children
// of parentKey: a `<slug>.md` file per child, plus a `<slug>/` folder for each
// child that itself has children. The file entries are lightweight (walkFS
// re-Stats every node for the authoritative props).
func (fs *davFS) dirFromChildren(name string, mod time.Time, t *spaceTree, parentKey int64) *davDir {
	group := t.children[parentKey]
	kids := make([]os.FileInfo, 0, len(group))
	for _, c := range group {
		slug := t.slug[c.ID]
		kids = append(kids, &davInfo{name: slug + ".md"})
		if t.hasChildren(c.ID) {
			kids = append(kids, &davInfo{name: slug, dir: true, mod: davModTime(c.UpdatedAt)})
		}
	}
	return &davDir{info: dirInfo(name, mod), children: kids}
}

func (fs *davFS) openWrite(ctx context.Context, segs []string) (webdavFile, error) {
	// A page must live inside a space (≥2 segments: space + filename). A write
	// above that is junk we swallow rather than error on.
	if len(segs) < 2 {
		return &davDiscardFile{name: lastSeg(segs)}, nil
	}
	filename := segs[len(segs)-1]
	if !isMarkdownName(filename) {
		// .DS_Store, ._*, *.swp, editor temp — accept-and-drop (sync §14).
		return &davDiscardFile{name: filename}, nil
	}
	sp, found, err := fs.space(ctx, segs[0])
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, os.ErrNotExist
	}
	var parentID *int64
	if len(segs) > 2 {
		t, err := davStateFrom(ctx).tree(ctx, fs.s.DB, sp.id)
		if err != nil {
			return nil, err
		}
		parent, isFile, ok := t.resolve(segs[1 : len(segs)-1])
		if !ok || isFile { // missing or non-collection parent → 409 conflict
			return nil, os.ErrNotExist
		}
		parentID = &parent.ID
	}
	return &davWriteFile{fs: fs, ctx: ctx, spaceID: sp.id, parentID: parentID, filename: filename}, nil
}

func (fs *davFS) Mkdir(ctx context.Context, name string, _ os.FileMode) error {
	segs, ok := davSplit(name)
	if !ok {
		return os.ErrNotExist
	}
	if len(segs) == 0 {
		return os.ErrExist // root
	}
	sp, found, err := fs.space(ctx, segs[0])
	if err != nil {
		return err
	}
	if !found {
		// A new top-level folder would be a new space — not creatable over WebDAV.
		return os.ErrPermission
	}
	if len(segs) == 1 {
		return os.ErrExist // the space folder already exists
	}
	st := davStateFrom(ctx)
	t, err := st.tree(ctx, fs.s.DB, sp.id)
	if err != nil {
		return err
	}
	var parentID *int64
	if len(segs) > 2 {
		parent, isFile, ok := t.resolve(segs[1 : len(segs)-1])
		if !ok || isFile {
			return os.ErrNotExist
		}
		parentID = &parent.ID
	}
	target := segs[len(segs)-1]
	// If a page already occupies this folder name, MKCOL is a no-op success — the
	// page simply becomes (or already is) a child container. Otherwise mint an
	// empty container page; a later PUT of its `<name>.md` index binds to it (via
	// ApplyFileSync's path-fallback) and fills in the body/title.
	if _, exists := t.childBySlug(parentKey(parentID), target); exists {
		return nil
	}
	pr := davPrincipalFrom(ctx)
	_, ae := fs.s.createPageCore(ctx, pr.u, pr.k, pageCreateRequest{
		SpaceID: sp.id, ParentID: parentID, Title: target,
	})
	return davMapErr(ae)
}

func (fs *davFS) RemoveAll(ctx context.Context, name string) error {
	segs, ok := davSplit(name)
	if !ok {
		return os.ErrNotExist
	}
	if len(segs) <= 1 {
		return os.ErrPermission // never delete the root or a whole space via WebDAV
	}
	sp, found, err := fs.space(ctx, segs[0])
	if err != nil {
		return err
	}
	if !found {
		return os.ErrNotExist
	}
	t, err := davStateFrom(ctx).tree(ctx, fs.s.DB, sp.id)
	if err != nil {
		return err
	}
	p, _, ok := t.resolve(segs[1:]) // file or folder form both name the page
	if !ok {
		return os.ErrNotExist
	}
	pr := davPrincipalFrom(ctx)
	return davMapErr(fs.s.deletePageCore(ctx, pr.u, pr.k, p.ID))
}

func (fs *davFS) Rename(ctx context.Context, oldName, newName string) error {
	oseg, ok := davSplit(oldName)
	if !ok || len(oseg) <= 1 {
		return os.ErrPermission // can't move the root or a space
	}
	nseg, ok := davSplit(newName)
	if !ok || len(nseg) <= 1 {
		return os.ErrPermission
	}
	st := davStateFrom(ctx)
	osp, found, err := fs.space(ctx, oseg[0])
	if err != nil {
		return err
	}
	if !found {
		return os.ErrNotExist
	}
	ot, err := st.tree(ctx, fs.s.DB, osp.id)
	if err != nil {
		return err
	}
	page, _, ok := ot.resolve(oseg[1:])
	if !ok {
		return os.ErrNotExist
	}
	nsp, found, err := fs.space(ctx, nseg[0])
	if err != nil {
		return err
	}
	if !found {
		return os.ErrNotExist
	}
	var newParentID *int64
	if len(nseg) > 2 {
		nt := ot
		if nsp.id != osp.id {
			if nt, err = st.tree(ctx, fs.s.DB, nsp.id); err != nil {
				return err
			}
		}
		parent, isFile, ok := nt.resolve(nseg[1 : len(nseg)-1])
		if !ok || isFile {
			return os.ErrNotExist
		}
		newParentID = &parent.ID
	}
	// Only a file-form rename (`<old>.md` → `<new>.md`) retitles; a folder rename
	// reparents/relocates without clobbering the title with a slug.
	newTitle := page.Title
	if isMarkdownName(oseg[len(oseg)-1]) && isMarkdownName(nseg[len(nseg)-1]) {
		newTitle = davTitleFromName(nseg[len(nseg)-1])
	}
	return fs.renameApply(ctx, page, nsp.id, newParentID, newTitle)
}

// renameApply commits a WebDAV MOVE as a retitle and/or reparent in one tx,
// reusing the shared in-tx page primitives (the same ones sync + REST use).
func (fs *davFS) renameApply(ctx context.Context, page models.Page, spaceID int64, parentID *int64, newTitle string) error {
	titleChanged := strings.TrimSpace(newTitle) != "" && newTitle != page.Title
	placementChanged := page.SpaceID != spaceID || !sameParent(page.ParentID, parentID)
	if !titleChanged && !placementChanged {
		return nil
	}
	pr := davPrincipalFrom(ctx)
	tx, err := fs.s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	cur, err := selectPageByIDTx(ctx, tx, page.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return os.ErrNotExist
	}
	if err != nil {
		return err
	}
	if ae := fs.s.requireEditTx(ctx, tx, pr.u, pr.k, cur.SpaceID); ae != nil {
		return davMapErr(ae)
	}
	p := cur
	if titleChanged {
		up, ae := applyUpdateTx(ctx, tx, cur.ID, pageUpdateRequest{Title: &newTitle})
		if ae != nil {
			return davMapErr(ae)
		}
		p = up
	}
	if placementChanged {
		mp, ae := fs.s.applyMoveTx(ctx, tx, pr.u, pr.k, p, syncMoveParams(cur, spaceID, parentID))
		if ae != nil {
			return davMapErr(ae)
		}
		p = mp
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	fs.s.afterPageWrite(ctx, cur, p, false, true, pr.u.ID, "sync")
	return nil
}

func lastSeg(segs []string) string {
	if len(segs) == 0 {
		return ""
	}
	return segs[len(segs)-1]
}
