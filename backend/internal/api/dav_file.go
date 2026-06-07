package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/zcag/tela/backend/internal/models"
	"github.com/zcag/tela/backend/internal/pagemd"
	"golang.org/x/net/webdav"
)

// dav_file.go: the webdav.File and os.FileInfo implementations the davFS hands
// back. Reads serve pagemd.Encode bytes; writes buffer and flush through
// ApplyFileSync on Close/Stat; directories enumerate the page tree.

// davTimeLayout is tela's canonical TEXT datetime ('YYYY-MM-DD HH:MM:SS' UTC,
// the SQLite-era format kept across the Postgres move). Parsed for getlastmodified.
const davTimeLayout = "2006-01-02 15:04:05"

func davModTime(ts string) time.Time {
	t, err := time.Parse(davTimeLayout, ts)
	if err != nil {
		return time.Unix(0, 0).UTC()
	}
	return t.UTC()
}

// davInfo is the os.FileInfo (plus webdav.ETager) for a node. For a page file it
// carries the page so Size/ETag/ModTime derive from the canonical Encode output;
// for directories and for the lightweight entries Readdir emits, page is nil and
// only Name/IsDir are meaningful (walkFS re-Stats each child for the real props).
type davInfo struct {
	name string
	dir  bool
	mod  time.Time
	page *models.Page
	enc  []byte // memoised Encode(page) for Size; nil until first needed
}

func (i *davInfo) Name() string { return i.name }
func (i *davInfo) IsDir() bool  { return i.dir }
func (i *davInfo) Sys() any     { return nil }

func (i *davInfo) Mode() os.FileMode {
	if i.dir {
		return os.ModeDir | 0o755
	}
	return 0o644
}

func (i *davInfo) ModTime() time.Time {
	if i.page != nil {
		return davModTime(i.page.UpdatedAt)
	}
	return i.mod
}

func (i *davInfo) Size() int64 {
	if i.page == nil {
		return 0
	}
	if i.enc == nil {
		i.enc = pagemd.Encode(*i.page, publicBaseURL())
	}
	return int64(len(i.enc))
}

// ETag is a strong validator: the page id paired with its updated_at. Any write
// (body, title, props, move) bumps updated_at, and the served bytes are a pure
// function of the page, so this changes iff the content changes — letting rclone
// skip unchanged transfers without re-hashing. Non-page nodes fall back to
// webdav's ModTime/Size etag.
func (i *davInfo) ETag(_ context.Context) (string, error) {
	if i.page == nil {
		return "", webdav.ErrNotImplemented
	}
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, i.page.UpdatedAt)
	return `"p` + strconv.FormatInt(i.page.ID, 10) + `-` + digits + `"`, nil
}

// pageFileInfo builds a file FileInfo for a page under the given on-disk name
// (the deduped `<slug>.md` the resolver already computed for that sibling group).
func pageFileInfo(name string, p models.Page) *davInfo {
	cp := p
	return &davInfo{name: name, page: &cp}
}

func dirInfo(name string, mod time.Time) *davInfo {
	return &davInfo{name: name, dir: true, mod: mod}
}

// davReadFile serves a page's canonical markdown for GET. The bytes are the
// pagemd.Encode output (frontmatter view + pure body); a bytes.Reader gives the
// io.Seeker http.ServeContent needs for Range requests.
type davReadFile struct {
	info *davInfo
	rd   *bytes.Reader
}

func newDavReadFile(name string, p models.Page) *davReadFile {
	enc := pagemd.Encode(p, publicBaseURL())
	cp := p
	info := &davInfo{name: name, page: &cp, enc: enc}
	return &davReadFile{info: info, rd: bytes.NewReader(enc)}
}

func (f *davReadFile) Read(p []byte) (int, error)                { return f.rd.Read(p) }
func (f *davReadFile) Seek(off int64, whence int) (int64, error) { return f.rd.Seek(off, whence) }
func (f *davReadFile) Stat() (os.FileInfo, error)                { return f.info, nil }
func (f *davReadFile) Write([]byte) (int, error)                 { return 0, os.ErrInvalid }
func (f *davReadFile) Close() error                              { return nil }
func (f *davReadFile) Readdir(int) ([]os.FileInfo, error) {
	return nil, errors.New("not a directory")
}

// davDir is a collection node (root, a space folder, or a page-as-folder). Its
// children are precomputed at open time; Readdir just hands them out.
type davDir struct {
	info     *davInfo
	children []os.FileInfo
	off      int
}

func (d *davDir) Read([]byte) (int, error)       { return 0, os.ErrInvalid }
func (d *davDir) Seek(int64, int) (int64, error) { return 0, os.ErrInvalid }
func (d *davDir) Write([]byte) (int, error)      { return 0, os.ErrInvalid }
func (d *davDir) Close() error                   { return nil }
func (d *davDir) Stat() (os.FileInfo, error)     { return d.info, nil }

// Readdir mirrors os.File: count<=0 returns every remaining entry; count>0
// returns the next batch and io.EOF once exhausted. walkFS calls Readdir(0).
func (d *davDir) Readdir(count int) ([]os.FileInfo, error) {
	if count <= 0 {
		rest := d.children[d.off:]
		d.off = len(d.children)
		return rest, nil
	}
	if d.off >= len(d.children) {
		return nil, io.EOF
	}
	end := d.off + count
	if end > len(d.children) {
		end = len(d.children)
	}
	batch := d.children[d.off:end]
	d.off = end
	return batch, nil
}

// davWriteFile buffers a PUT body and resolves it through ApplyFileSync — the
// shared id-binding sync kernel — when the handler finishes. handlePut calls
// Stat() BEFORE Close(), so the flush is triggered by whichever runs first and
// memoised: Stat returns the resulting page's info (correct ETag for the PUT
// response), Close is then a no-op. A flush error surfaces as the PUT failing.
type davWriteFile struct {
	fs       *davFS
	ctx      context.Context
	spaceID  int64
	parentID *int64
	filename string

	buf     bytes.Buffer
	flushed bool
	result  models.Page
	err     error
}

func (f *davWriteFile) Write(p []byte) (int, error) { return f.buf.Write(p) }

func (f *davWriteFile) flush() error {
	if f.flushed {
		return f.err
	}
	f.flushed = true
	pr := davPrincipalFrom(f.ctx)
	p, _, ae := f.fs.s.ApplyFileSync(f.ctx, pr.u, pr.k, f.spaceID, f.parentID, f.filename, f.buf.Bytes())
	if ae != nil {
		f.err = ae
		return ae
	}
	f.result = p
	return nil
}

func (f *davWriteFile) Stat() (os.FileInfo, error) {
	if err := f.flush(); err != nil {
		return nil, err
	}
	// Name is unused by handlePut (it only reads the ETag); the page's own slug
	// is the best-effort display name.
	return pageFileInfo(mdSlugOr(f.result.Title, "page")+".md", f.result), nil
}

func (f *davWriteFile) Close() error                       { return f.flush() }
func (f *davWriteFile) Read([]byte) (int, error)           { return 0, os.ErrInvalid }
func (f *davWriteFile) Seek(int64, int) (int64, error)     { return 0, os.ErrInvalid }
func (f *davWriteFile) Readdir(int) ([]os.FileInfo, error) { return nil, os.ErrInvalid }

// davDiscardFile swallows a write to a path tela does not persist (a non-.md
// junk file — .DS_Store, ._*, *.swp, editor temp). Accepting it keeps OS-native
// clients from erroring, while creating no junk page (sync §14 hygiene). rclone
// should still exclude these via filters; this is the defensive backstop.
type davDiscardFile struct {
	name string
	n    int64
}

func (f *davDiscardFile) Write(p []byte) (int, error) { f.n += int64(len(p)); return len(p), nil }
func (f *davDiscardFile) Close() error                { return nil }
func (f *davDiscardFile) Read([]byte) (int, error)    { return 0, os.ErrInvalid }
func (f *davDiscardFile) Seek(int64, int) (int64, error) {
	return 0, os.ErrInvalid
}
func (f *davDiscardFile) Readdir(int) ([]os.FileInfo, error) { return nil, os.ErrInvalid }
func (f *davDiscardFile) Stat() (os.FileInfo, error) {
	return &davInfo{name: f.name, mod: time.Unix(0, 0).UTC()}, nil
}
