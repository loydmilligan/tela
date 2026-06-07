// Package mdimport parses a bundle of markdown files (GFM + Obsidian flavor)
// and inserts them as pages in a tela space. The on-wire path component is
// `import` but the package name avoids the Go keyword.
//
// Hierarchy rules (locked by PO Q39, 2026-05-19):
//   - Every directory referenced by an imported markdown file becomes an index
//     page. A `README.md` directly inside the dir (case-insensitive basename)
//     becomes the index — title is the directory's basename (overriding any
//     frontmatter title / H1 inside the README); otherwise an empty index
//     page is synthesized.
//   - Files at the upload root attach directly to the request's parent_id.
//   - Title priority for non-index files: frontmatter `title:` → first H1 →
//     filename without extension.
//   - Sibling-title conflicts (against both in-batch peers and existing DB
//     rows) get a `(N)` suffix appended until unique.
//   - Wikilinks `[[X]]` and relative `[…](./y.md)` links are NOT rewritten —
//     v0 leaves them as broken text per Q39 #3 (deferred).
//
// Flatten-root pre-pass (locked by PO Q40 C + Q42 B, 2026-05-19):
//   - When every markdown path in the upload shares a single non-empty top-
//     level directory (rootDir), a pre-pass runs before the regular hierarchy
//     resolution. `<input webkitdirectory>` always prepends the picked folder
//     name to every relative path, so without this pass the user-visible
//     result is a wrapping page they did not intend.
//   - With root README (`rootDir/README.md`, case-insensitive basename): one
//     wrapper page is created at the request's parent_id, titled by rootDir's
//     basename and bodied by the README (frontmatter-stripped). The README is
//     consumed; all other paths have their `rootDir/` prefix stripped and
//     nest under the wrapper. Behavior is identical whether parent_id is the
//     space root or a real page (Q42 B).
//   - Without root README: pure flatten — strip the `rootDir/` prefix from
//     all paths so files formerly directly inside rootDir attach to the
//     request's parent_id and the rest of the pipeline runs unchanged.
//   - When the upload has multiple top-level dirs (or only files at root),
//     the pre-pass is a no-op and today's behavior is preserved.
package mdimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/zcag/tela/backend/internal/pagemd"
)

// propsJSON marshals a props bag to a JSON string for a JSONB column, falling
// back to an empty object. Bound with a ::jsonb cast at the insert site (pgx
// encodes a Go string as text, which jsonb input parses).
func propsJSON(props map[string]any) string {
	if len(props) == 0 {
		return "{}"
	}
	b, err := json.Marshal(props)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// ImportFile is one entry in a markdown import payload — the relative path
// under the uploaded root plus its raw bytes.
type ImportFile struct {
	Path    string
	Content []byte
}

// ImportedPage describes a single created (or, for dry-run, planned) page.
type ImportedPage struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	ParentID *int64 `json:"parent_id"`
	Path     string `json:"path"`
}

// SkippedFile records a non-fatal exclusion from the import (e.g. non-markdown
// extension). The import continues; the row reports what was dropped and why.
type SkippedFile struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// ErrorFile records a soft per-file error that did not roll the batch back.
// Hard errors (DB constraint, internal failures) propagate up as a returned
// error and the caller rolls the tx back.
type ErrorFile struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Summary tallies the three counts surfaced in the response envelope.
type Summary struct {
	Created          int `json:"created"`
	Skipped          int `json:"skipped"`
	ConflictsRenamed int `json:"conflicts_renamed"`
}

// Result is the response envelope returned by Import.
type Result struct {
	Summary Summary        `json:"summary"`
	Pages   []ImportedPage `json:"pages"`
	Skipped []SkippedFile  `json:"skipped"`
	Errors  []ErrorFile    `json:"errors"`
}

// mdEntry is one accepted markdown file inside the importer's working set —
// `consumed` marks it as claimed by a directory index page so Pass 2 skips it.
type mdEntry struct {
	path     string
	content  string
	consumed bool
}

// Import processes a list of markdown files and inserts them as pages under
// spaceID/parentID. The caller owns the transaction lifecycle: tx.Commit on
// success, tx.Rollback on hard error or when dryRun=true.
//
// On dryRun the function returns the planned tree with negative placeholder
// ids (-1, -2, …) and still consults the live database for sibling-title
// conflict detection so the preview matches the eventual real run.
func Import(
	ctx context.Context,
	tx *sql.Tx,
	spaceID int64,
	parentID *int64,
	authorID int64,
	files []ImportFile,
	dryRun bool,
) (*Result, error) {
	result := &Result{
		Pages:   []ImportedPage{},
		Skipped: []SkippedFile{},
		Errors:  []ErrorFile{},
	}

	mdFiles := make([]mdEntry, 0, len(files))
	for _, f := range files {
		clean, err := sanitizePath(f.Path)
		if err != nil {
			result.Errors = append(result.Errors, ErrorFile{Path: f.Path, Reason: err.Error()})
			continue
		}
		ext := strings.ToLower(path.Ext(clean))
		if ext != ".md" && ext != ".markdown" {
			result.Skipped = append(result.Skipped, SkippedFile{Path: clean, Reason: "not_markdown"})
			continue
		}
		mdFiles = append(mdFiles, mdEntry{path: clean, content: string(f.Content)})
	}

	// M14.5 pre-pass: detect a single top-level directory and flatten it (or
	// wrap when a root README exists). See package doc-comment for the
	// locked Q40 C + Q42 B spec.
	var (
		havePendingWrapper bool
		wrapperTitle       string
		wrapperBody        string
		wrapperImportPath  string
		wrapperProps       map[string]any
	)
	if rootDir := singleTopLevelDir(mdFiles); rootDir != "" {
		if idx := findRootReadme(mdFiles, rootDir); idx >= 0 {
			rawBody, _, rawProps := pagemd.Decode(mdFiles[idx].content)
			havePendingWrapper = true
			wrapperTitle = path.Base(rootDir)
			wrapperBody = rawBody
			wrapperProps = rawProps
			wrapperImportPath = mdFiles[idx].path
			mdFiles = append(mdFiles[:idx], mdFiles[idx+1:]...)
		}
		prefix := rootDir + "/"
		for i := range mdFiles {
			mdFiles[i].path = strings.TrimPrefix(mdFiles[i].path, prefix)
		}
	}

	sort.Slice(mdFiles, func(i, j int) bool { return mdFiles[i].path < mdFiles[j].path })

	dirs := map[string]struct{}{}
	for _, e := range mdFiles {
		for d := path.Dir(e.path); d != "." && d != "/" && d != ""; d = path.Dir(d) {
			dirs[d] = struct{}{}
		}
	}
	sortedDirs := make([]string, 0, len(dirs))
	for d := range dirs {
		sortedDirs = append(sortedDirs, d)
	}
	sort.Slice(sortedDirs, func(i, j int) bool {
		di, dj := strings.Count(sortedDirs[i], "/"), strings.Count(sortedDirs[j], "/")
		if di != dj {
			return di < dj
		}
		return sortedDirs[i] < sortedDirs[j]
	})

	// Per-parent state. siblingsAtParent caches the existing DB titles + the
	// in-batch titles inserted so far, used for conflict-rename. positionsAt
	// caches MAX(position)+1 per parent — lazily filled on first insert.
	siblingsAtParent := map[int64]map[string]struct{}{}
	positionsAt := map[int64]int64{}

	// dirMap holds the pageID for each imported directory. For dry-run these
	// are negative placeholders (-1, -2, …); for live runs they are real
	// INSERT ... RETURNING id values. nextPlaceholder is the running counter.
	dirMap := map[string]int64{}
	nextPlaceholder := int64(-1)

	resolveParent := func(filePath string) *int64 {
		d := path.Dir(filePath)
		if d == "." || d == "" || d == "/" {
			return parentID
		}
		v := dirMap[d]
		return &v
	}

	// preloadSiblings populates siblingsAtParent[pid] with the existing DB
	// page titles under pid (parent_id IS NULL when pid == 0/nil).
	preloadSiblings := func(parent *int64) error {
		key := int64(0)
		if parent != nil {
			key = *parent
		}
		if _, ok := siblingsAtParent[key]; ok {
			return nil
		}
		set := map[string]struct{}{}
		var rows *sql.Rows
		var err error
		if parent == nil {
			rows, err = tx.QueryContext(ctx,
				`SELECT title FROM pages WHERE space_id = $1 AND parent_id IS NULL`, spaceID)
		} else {
			rows, err = tx.QueryContext(ctx,
				`SELECT title FROM pages WHERE space_id = $1 AND parent_id = $2`, spaceID, *parent)
		}
		if err != nil {
			return fmt.Errorf("preload siblings: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var t string
			if err := rows.Scan(&t); err != nil {
				return fmt.Errorf("scan sibling title: %w", err)
			}
			set[t] = struct{}{}
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate siblings: %w", err)
		}
		siblingsAtParent[key] = set
		return nil
	}

	// preloadPosition fills positionsAt[parent] with the next-position int.
	preloadPosition := func(parent *int64) error {
		key := int64(0)
		if parent != nil {
			key = *parent
		}
		if _, ok := positionsAt[key]; ok {
			return nil
		}
		var maxPos sql.NullInt64
		var err error
		if parent == nil {
			err = tx.QueryRowContext(ctx,
				`SELECT MAX(position) FROM pages WHERE space_id = $1 AND parent_id IS NULL`, spaceID).Scan(&maxPos)
		} else {
			err = tx.QueryRowContext(ctx,
				`SELECT MAX(position) FROM pages WHERE space_id = $1 AND parent_id = $2`, spaceID, *parent).Scan(&maxPos)
		}
		if err != nil {
			return fmt.Errorf("preload position: %w", err)
		}
		if maxPos.Valid {
			positionsAt[key] = maxPos.Int64 + 1
		} else {
			positionsAt[key] = 0
		}
		return nil
	}

	// resolveTitle applies the conflict rule against the cached sibling set.
	// Mutates the cache by inserting the resolved title.
	resolveTitle := func(parent *int64, base string) (resolved string, renamed bool) {
		key := int64(0)
		if parent != nil {
			key = *parent
		}
		set := siblingsAtParent[key]
		if _, clash := set[base]; !clash {
			set[base] = struct{}{}
			return base, false
		}
		for n := 2; ; n++ {
			cand := fmt.Sprintf("%s (%d)", base, n)
			if _, clash := set[cand]; !clash {
				set[cand] = struct{}{}
				return cand, true
			}
		}
	}

	// insertPage handles both the live INSERT and the dry-run placeholder
	// allocation. Returns the assigned pageID.
	insertPage := func(parent *int64, title, body, importPath string, props map[string]any) (int64, error) {
		if err := preloadPosition(parent); err != nil {
			return 0, err
		}
		key := int64(0)
		if parent != nil {
			key = *parent
		}
		pos := positionsAt[key]
		positionsAt[key] = pos + 1

		propsStr := propsJSON(props)

		var pageID int64
		if dryRun {
			pageID = nextPlaceholder
			nextPlaceholder--
		} else {
			var parentArg any
			if parent != nil {
				parentArg = *parent
			}
			err := tx.QueryRowContext(ctx,
				`INSERT INTO pages (space_id, parent_id, title, body, position, props)
				 VALUES ($1, $2, $3, $4, $5, $6::jsonb) RETURNING id`,
				spaceID, parentArg, title, body, pos, propsStr).Scan(&pageID)
			if err != nil {
				return 0, fmt.Errorf("insert page %q: %w", importPath, err)
			}

			// Seed page-history so the imported page already has an entry
			// from t=0. Source 'import' distinguishes from 'manual' saves.
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO page_revisions (page_id, body, title, props, author_id, source, byte_size, created_at)
				VALUES ($1, $2, $3, $4::jsonb, $5, 'import', $6, tela_now())`,
				pageID, body, title, propsStr, authorID, int64(len(body))); err != nil {
				return 0, fmt.Errorf("seed page_revision for %q: %w", importPath, err)
			}
		}

		result.Pages = append(result.Pages, ImportedPage{
			ID:       pageID,
			Title:    title,
			ParentID: parent,
			Path:     importPath,
		})
		result.Summary.Created++
		return pageID, nil
	}

	// Materialize the pending wrapper page (if the pre-pass set one up).
	// The wrapper is inserted at the request's parent_id and then becomes
	// the new effective parent for the rest of the import — every stripped
	// path nests under it.
	if havePendingWrapper {
		if err := preloadSiblings(parentID); err != nil {
			return nil, err
		}
		resolved, renamed := resolveTitle(parentID, wrapperTitle)
		if renamed {
			result.Summary.ConflictsRenamed++
		}
		wrapperID, err := insertPage(parentID, resolved, wrapperBody, wrapperImportPath, wrapperProps)
		if err != nil {
			return nil, err
		}
		ref := wrapperID
		parentID = &ref
	}

	// Pass 1: create directory index pages (shallow → deep). Each is parented
	// at either request.parent_id (top-level dir) or the parent dir's id.
	for _, d := range sortedDirs {
		parent := parentID
		if pd := path.Dir(d); pd != "." && pd != "/" && pd != "" {
			pid := dirMap[pd]
			parent = &pid
		}
		if err := preloadSiblings(parent); err != nil {
			return nil, err
		}

		base := path.Base(d)

		// Search for README.md (case-insensitive) directly inside this dir.
		var readmeIdx = -1
		for i := range mdFiles {
			if mdFiles[i].consumed {
				continue
			}
			if path.Dir(mdFiles[i].path) != d {
				continue
			}
			if strings.EqualFold(path.Base(mdFiles[i].path), "README.md") {
				readmeIdx = i
				break
			}
		}

		var body string
		var importPath string
		var props map[string]any
		if readmeIdx >= 0 {
			// README props attach to the dir-index page; the dir basename still
			// wins the title (so the frontmatter title is intentionally ignored).
			rawBody, _, rawProps := pagemd.Decode(mdFiles[readmeIdx].content)
			body = rawBody
			props = rawProps
			importPath = mdFiles[readmeIdx].path
			mdFiles[readmeIdx].consumed = true
		} else {
			body = ""
			importPath = d + "/"
		}

		resolvedTitle, renamed := resolveTitle(parent, base)
		if renamed {
			result.Summary.ConflictsRenamed++
		}
		pageID, err := insertPage(parent, resolvedTitle, body, importPath, props)
		if err != nil {
			return nil, err
		}
		dirMap[d] = pageID
	}

	// Pass 2: insert remaining (non-consumed) markdown files.
	for i := range mdFiles {
		if mdFiles[i].consumed {
			continue
		}
		parent := resolveParent(mdFiles[i].path)
		if err := preloadSiblings(parent); err != nil {
			return nil, err
		}

		body, fmTitle, fmProps := pagemd.Decode(mdFiles[i].content)
		title := TitleFor(fmTitle, body, mdFiles[i].path)

		resolvedTitle, renamed := resolveTitle(parent, title)
		if renamed {
			result.Summary.ConflictsRenamed++
		}
		if _, err := insertPage(parent, resolvedTitle, body, mdFiles[i].path, fmProps); err != nil {
			return nil, err
		}
	}

	result.Summary.Skipped = len(result.Skipped)
	return result, nil
}

// singleTopLevelDir returns the shared first path-segment when every entry
// in files lives below the same non-empty top-level directory. Returns ""
// when files is empty, when any entry sits at the upload root (no slash),
// or when entries disagree on the first segment.
func singleTopLevelDir(files []mdEntry) string {
	if len(files) == 0 {
		return ""
	}
	var rootDir string
	for _, f := range files {
		idx := strings.IndexByte(f.path, '/')
		if idx <= 0 {
			return ""
		}
		seg := f.path[:idx]
		if rootDir == "" {
			rootDir = seg
		} else if rootDir != seg {
			return ""
		}
	}
	return rootDir
}

// findRootReadme returns the index of the README directly inside rootDir
// (case-insensitive basename match on `README.md`), or -1 when absent. The
// directory prefix itself is matched case-sensitively — only the basename
// is case-insensitive.
func findRootReadme(files []mdEntry, rootDir string) int {
	for i, f := range files {
		if path.Dir(f.path) != rootDir {
			continue
		}
		if strings.EqualFold(path.Base(f.path), "README.md") {
			return i
		}
	}
	return -1
}

// sanitizePath normalises a relative path from the multipart filename. It
// rejects absolute paths and `..` segments so a malicious upload cannot point
// outside its own subtree. Returns the cleaned forward-slash path.
func sanitizePath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("empty_path")
	}
	// Multipart filenames are canonical forward-slash. Defensive: normalise
	// any stray backslashes some clients use on Windows.
	p = strings.ReplaceAll(p, "\\", "/")
	if strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("invalid_path")
	}
	clean := path.Clean(p)
	if clean == "." || clean == "/" {
		return "", fmt.Errorf("invalid_path")
	}
	for _, seg := range strings.Split(clean, "/") {
		if seg == ".." {
			return "", fmt.Errorf("invalid_path")
		}
	}
	return clean, nil
}
