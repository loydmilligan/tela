package api

import (
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// Legacy image sidecar — SERVE ONLY. Uploads now go through the unified
// space_files attachment store (migration 0015, attachments.go), which the
// editor uses for both inline images and other files. The old upload route
// (POST /api/pages/{id}/images) is retired; this serve route stays so images
// already embedded in historical page bodies as /api/images/{page}/{hash}
// keep resolving.
//
//   - GET /api/images/{page_id}/{file} — PUBLIC, no session (reachable from
//     share-mode). Content-addressed by hash → Cache-Control: immutable +
//     strong ETag. The /api/images/ prefix is on auth.IsPublicPath, same
//     shape as /api/diagrams/.
//
// detectImageMime stays here because the live attachment path
// (createPageUploadFile) reuses it to trust an image's mime by magic bytes.

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
