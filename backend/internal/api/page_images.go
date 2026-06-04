package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// Image-upload sidecar. Mirrors page_diagrams.go's hybrid storage (researcher
// #98): content-addressed BLOBs in SQLite, public immutable serve.
//
// Two routes:
//
//   - POST /api/pages/{id}/images — session-authed editor+ multipart upload
//     (form field "file"). The server detects the image type from magic bytes
//     (never trusts the client), hashes the bytes (sha256), upserts, and
//     returns {url} = /api/images/{page_id}/{hash}.{ext}. The editor inserts
//     that as `![](url)` so the page body stays canonical markdown.
//
//   - GET /api/images/{page_id}/{file} — PUBLIC, no session (reachable from
//     share-mode). Content-addressed by hash → Cache-Control: immutable +
//     strong ETag. The /api/images/ prefix is on auth.IsPublicPath, same
//     shape as /api/diagrams/.

const (
	// 10 MiB cap on the uploaded file. Generous for wiki screenshots; large
	// enough to avoid surprising users, small enough to bound memory.
	pageImageMaxUploadBytes = 10 << 20
)

// pageImageHashRE validates the content_hash path value: 64 lowercase hex
// chars (full sha256). Also guards the GET route against path traversal.
var pageImageHashRE = regexp.MustCompile(`^[a-f0-9]{64}$`)

// detectImageMime returns the canonical mime for a supported raster image, or
// "" if the bytes are not one of png / jpeg / gif / webp. Detection is by
// magic bytes — the client's declared Content-Type is never trusted. SVG is
// deliberately excluded (it can carry scripts → stored-XSS when served from
// our origin).
func detectImageMime(b []byte) string {
	switch {
	case len(b) >= 8 && b[0] == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G':
		return "image/png"
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return "image/jpeg"
	case len(b) >= 6 && string(b[0:4]) == "GIF8":
		return "image/gif"
	case len(b) >= 12 && string(b[0:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "image/webp"
	default:
		return ""
	}
}

func imageExtForMime(mime string) string {
	switch mime {
	case "image/png":
		return "png"
	case "image/jpeg":
		return "jpg"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	default:
		return "bin"
	}
}

type pageImageUploadResponse struct {
	ID       int64  `json:"id"`
	PageID   int64  `json:"page_id"`
	Hash     string `json:"hash"`
	Mime     string `json:"mime"`
	ByteSize int64  `json:"byte_size"`
	URL      string `json:"url"`
}

// UploadPageImage handles POST /api/pages/{id}/images.
//
// Membership gate: editor+ on the page's space. Body is multipart/form-data
// with a "file" part. Idempotent on (page_id, sha256(bytes)).
func (s *Server) UploadPageImage(w http.ResponseWriter, r *http.Request) {
	pageID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
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
	if !enforceAPIKeySpaceScope(w, r, page.SpaceID) {
		return
	}
	role, err := spaceRole(ctx, s.DB, u.ID, page.SpaceID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusForbidden, "forbidden", "not a member")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup membership failed")
		return
	}
	if !canEdit(role) {
		writeError(w, http.StatusForbidden, "viewer_no_write", "editor or owner role required")
		return
	}

	// Cap the whole request body before parsing so a malicious client can't
	// pin a huge buffer. MaxBytesReader makes ParseMultipartForm fail cleanly.
	r.Body = http.MaxBytesReader(w, r.Body, pageImageMaxUploadBytes)
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "expected a multipart 'file' part")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not read uploaded file")
		return
	}
	if len(data) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "uploaded file is empty")
		return
	}

	mime := detectImageMime(data)
	if mime == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "unsupported image type (png, jpeg, gif, webp only)")
		return
	}

	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])

	// Idempotent upsert — same bytes on the same page is a no-op; we re-query
	// for the canonical row id + mime regardless.
	if _, err := s.DB.ExecContext(ctx, `
		INSERT INTO page_images (page_id, content_hash, mime, data, byte_size)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (page_id, content_hash) DO NOTHING`,
		pageID, hash, mime, data, int64(len(data))); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "store image failed")
		return
	}

	var (
		rowID    int64
		rowMime  string
		byteSize int64
	)
	if err := s.DB.QueryRowContext(ctx, `
		SELECT id, mime, byte_size FROM page_images
		 WHERE page_id = $1 AND content_hash = $2`,
		pageID, hash).Scan(&rowID, &rowMime, &byteSize); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch image failed")
		return
	}

	writeJSON(w, http.StatusOK, pageImageUploadResponse{
		ID:       rowID,
		PageID:   pageID,
		Hash:     hash,
		Mime:     rowMime,
		ByteSize: byteSize,
		URL: "/api/images/" + strconv.FormatInt(pageID, 10) + "/" +
			hash + "." + imageExtForMime(rowMime),
	})
}

// ServePageImage handles GET /api/images/{page_id}/{file}.
//
// Public — no session. Validates path values before touching SQL; mismatches
// return 404 (not 400) so the route is not an enumeration oracle. The stored
// mime is authoritative for Content-Type (the {ext} in the URL is cosmetic).
func (s *Server) ServePageImage(w http.ResponseWriter, r *http.Request) {
	pageID, err := strconv.ParseInt(r.PathValue("page_id"), 10, 64)
	if err != nil || pageID <= 0 {
		writeError(w, http.StatusNotFound, "not_found", "image not found")
		return
	}
	// Go 1.22+ mux wildcards must end a segment, so the extension is part of
	// {file}. Strip everything from the first dot to recover the hash.
	file := r.PathValue("file")
	hash := file
	if dot := strings.IndexByte(file, '.'); dot >= 0 {
		hash = file[:dot]
	}
	if !pageImageHashRE.MatchString(hash) {
		writeError(w, http.StatusNotFound, "not_found", "image not found")
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
	)
	err = s.DB.QueryRowContext(r.Context(), `
		SELECT data, mime FROM page_images
		 WHERE page_id = $1 AND content_hash = $2
		 LIMIT 1`, pageID, hash).Scan(&data, &mime)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "image not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch image failed")
		return
	}

	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
