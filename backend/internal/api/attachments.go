package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// attachments.go — the page-attachments surface over the unified space_files
// blob store (migration 0015). A page's attachments are the space_files parented
// to it (parent_page_id), which is exactly where a file rclone-synced into the
// page's folder lands — so synced files show up as attachments with no body edit.
//
// Two routes:
//
//   - GET /api/pages/{id}/attachments — session-authed (any space role reads).
//     Lists the parented files with a stable, content-addressed serve URL and an
//     `embedded` flag (does the body already reference this file's hash).
//
//   - GET /api/files/{space_id}/{file} — PUBLIC, no session (reachable from
//     share/public readers). Content-addressed by hash → immutable cache. Images
//     (raster) serve inline so `![](url)` renders; everything else is forced to
//     download (Content-Disposition: attachment) + nosniff, so an embedded
//     .html/.svg can't run as stored-XSS from our origin. /api/files/ is on
//     auth.IsPublicPath, same shape as /api/images/ and /api/diagrams/.

// inlineServeMimes are the only types served inline from /api/files; everything
// else downloads. Raster images only — SVG (image/svg+xml) is deliberately
// excluded (it can carry scripts).
var inlineServeMimes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true,
}

type attachmentOut struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Mime     string `json:"mime"`
	ByteSize int64  `json:"byte_size"`
	Hash     string `json:"hash"`
	URL      string `json:"url"`
	Embedded bool   `json:"embedded"` // body already references this file's hash
}

// spaceFileServeURL is the stable, rename-proof URL for a stored file: keyed by
// space + content hash (not path), so a sync rename/move never breaks a body
// embed. The extension is cosmetic (the stored mime is authoritative on serve).
func spaceFileServeURL(spaceID int64, name, hash string) string {
	ext := strings.ToLower(path.Ext(name)) // includes the dot, or ""
	return fmt.Sprintf("/api/files/%d/%s%s", spaceID, hash, ext)
}

// ListPageAttachments handles GET /api/pages/{id}/attachments.
func (s *Server) ListPageAttachments(w http.ResponseWriter, r *http.Request) {
	pageID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	page, err := selectPageByID(ctx, s.DB, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusForbidden, "forbidden", "not a member")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup page failed")
		return
	}
	if _, ok := s.requireMembership(w, r, page.SpaceID); !ok {
		return
	}

	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, name, mime, byte_size, content_hash
		  FROM space_files
		 WHERE parent_page_id = $1 AND deleted_at IS NULL
		 ORDER BY name ASC, id ASC`, pageID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list attachments failed")
		return
	}
	defer rows.Close()
	out := []attachmentOut{}
	for rows.Next() {
		var a attachmentOut
		if err := rows.Scan(&a.ID, &a.Name, &a.Mime, &a.ByteSize, &a.Hash); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan attachment failed")
			return
		}
		a.URL = spaceFileServeURL(page.SpaceID, a.Name, a.Hash)
		a.Embedded = strings.Contains(page.Body, a.Hash)
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list attachments failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"attachments": out})
}

// UploadPageAttachment handles POST /api/pages/{id}/attachments (multipart,
// field "file"). Editor+ on the page's space. Stores the bytes as a space_file
// parented to the page and returns its metadata + serve URL. This is the unified
// upload path the editor uses for BOTH inline images and other attachments.
func (s *Server) UploadPageAttachment(w http.ResponseWriter, r *http.Request) {
	pageID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	page, err := selectPageByID(ctx, s.DB, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusForbidden, "forbidden", "not a member")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup page failed")
		return
	}
	role, ok := s.requireMembership(w, r, page.SpaceID)
	if !ok {
		return
	}
	if !canEdit(role) {
		writeError(w, http.StatusForbidden, "viewer_no_write", "editor or owner role required")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, davFileMaxBytes())
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "expected a multipart 'file' part")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not read uploaded file (too large?)")
		return
	}
	if len(data) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "uploaded file is empty")
		return
	}
	name := sanitizeUploadName(hdr.Filename)

	sf, err := createPageUploadFile(ctx, s.DB, page.SpaceID, pageID, name, data)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "store attachment failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"attachment": attachmentOut{
		ID: sf.id, Name: sf.name, Mime: sf.mime, ByteSize: sf.size, Hash: sf.hash,
		URL:      spaceFileServeURL(page.SpaceID, sf.name, sf.hash),
		Embedded: strings.Contains(page.Body, sf.hash),
	}})
}

// DeletePageAttachment handles DELETE /api/pages/{id}/attachments/{file_id}.
// Editor+; soft-deletes a space_file that is parented to the page.
func (s *Server) DeletePageAttachment(w http.ResponseWriter, r *http.Request) {
	pageID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	fileID, ok := parseIDParam(w, r, "file_id")
	if !ok {
		return
	}
	ctx := r.Context()
	page, err := selectPageByID(ctx, s.DB, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusForbidden, "forbidden", "not a member")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup page failed")
		return
	}
	role, ok := s.requireMembership(w, r, page.SpaceID)
	if !ok {
		return
	}
	if !canEdit(role) {
		writeError(w, http.StatusForbidden, "viewer_no_write", "editor or owner role required")
		return
	}
	res, err := s.DB.ExecContext(ctx, `
		UPDATE space_files SET deleted_at = tela_now()
		 WHERE id = $1 AND parent_page_id = $2 AND deleted_at IS NULL`, fileID, pageID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "delete attachment failed")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "attachment not found on this page")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// sanitizeUploadName reduces a client-supplied filename to a safe basename (no
// path, no traversal), falling back to "file" when empty.
func sanitizeUploadName(name string) string {
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	return name
}

// createPageUploadFile stores an uploaded file parented to a page, content-aware
// so distinct uploads never clobber each other: identical bytes at the same name
// dedupe (idempotent), but DIFFERENT bytes that would collide on the name (e.g.
// two pasted "image.png") get a `-<hash8>` suffix so the first embed keeps
// working. The mime for a recognised raster image is taken from magic bytes (so
// the inline-serve path is trustworthy), else inferred from name/sniff.
func createPageUploadFile(ctx context.Context, db *sql.DB, spaceID, pageID int64, name string, data []byte) (spaceFile, error) {
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	mimeType := detectImageMime(data)
	if mimeType == "" {
		mimeType = detectFileMime(name, data)
	}
	finalName := name
	if h, found, err := liveFileHashAt(ctx, db, spaceID, pageID, finalName); err != nil {
		return spaceFile{}, err
	} else if found {
		if h == hash {
			return spaceFile{spaceID: spaceID, parentID: &pageID, name: finalName, hash: hash, mime: mimeType, size: int64(len(data))}, nil
		}
		ext := path.Ext(name)
		finalName = strings.TrimSuffix(name, ext) + "-" + hash[:8] + ext
		if h2, f2, err := liveFileHashAt(ctx, db, spaceID, pageID, finalName); err != nil {
			return spaceFile{}, err
		} else if f2 && h2 == hash {
			return spaceFile{spaceID: spaceID, parentID: &pageID, name: finalName, hash: hash, mime: mimeType, size: int64(len(data))}, nil
		}
	}
	var sf spaceFile
	err := db.QueryRowContext(ctx, `
		INSERT INTO space_files (space_id, parent_page_id, name, content_hash, mime, data, byte_size)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`,
		spaceID, pageID, finalName, hash, mimeType, data, int64(len(data))).Scan(&sf.id)
	if err != nil {
		return spaceFile{}, err
	}
	sf.spaceID, sf.parentID, sf.name, sf.hash, sf.mime, sf.size = spaceID, &pageID, finalName, hash, mimeType, int64(len(data))
	return sf, nil
}

// liveFileHashAt returns the content hash of the live file at (space, parent, name).
func liveFileHashAt(ctx context.Context, db *sql.DB, spaceID, pageID int64, name string) (hash string, found bool, err error) {
	err = db.QueryRowContext(ctx, `
		SELECT content_hash FROM space_files
		 WHERE space_id = $1 AND COALESCE(parent_page_id, 0) = $2 AND name = $3 AND deleted_at IS NULL`,
		spaceID, pageID, name).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return hash, true, nil
}

// ServeSpaceFile handles GET /api/files/{space_id}/{file}.
//
// Public — no session. Path values are validated before touching SQL; mismatches
// return 404 (not 400) so the route is not an enumeration oracle.
func (s *Server) ServeSpaceFile(w http.ResponseWriter, r *http.Request) {
	spaceID, err := strconv.ParseInt(r.PathValue("space_id"), 10, 64)
	if err != nil || spaceID <= 0 {
		writeError(w, http.StatusNotFound, "not_found", "file not found")
		return
	}
	// Go 1.22+ mux wildcards must end a segment, so the extension is part of
	// {file}. Strip from the first dot to recover the hash.
	file := r.PathValue("file")
	hash := file
	if dot := strings.IndexByte(file, '.'); dot >= 0 {
		hash = file[:dot]
	}
	if !pageImageHashRE.MatchString(hash) { // 64 lowercase hex — shared validator
		writeError(w, http.StatusNotFound, "not_found", "file not found")
		return
	}

	etag := `"` + hash + `"`
	if r.Header.Get("If-None-Match") == etag {
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.WriteHeader(http.StatusNotModified)
		return
	}

	var (
		data []byte
		mime string
		name string
	)
	// Any live row with these bytes serves them — identical content may exist at
	// several locations, but the bytes (and thus the response) are the same.
	err = s.DB.QueryRowContext(r.Context(), `
		SELECT data, mime, name FROM space_files
		 WHERE space_id = $1 AND content_hash = $2 AND deleted_at IS NULL
		 LIMIT 1`, spaceID, hash).Scan(&data, &mime, &name)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "file not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch file failed")
		return
	}

	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", etag)
	// Inline only for safe raster images (so `![](url)` renders); force download
	// for everything else so arbitrary content can't execute from our origin.
	if inlineServeMimes[mime] {
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", name))
	} else {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
