package api

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// M13.2 RichView: Excalidraw PNG sidecar (HYBRID storage, researcher #98).
//
// Two routes:
//
//   - PUT /api/pages/{id}/diagrams — session-authed editor+ upload. JSON body
//     {scene_hash, png_base64}. The hash is computed client-side from the
//     fenced ` ```excalidraw\n{json}\n``` ` block; the PNG is the rendered
//     snapshot. Idempotent upsert via UNIQUE(page_id, scene_hash) — a second
//     PUT with the same hash is a no-op.
//
//   - GET /api/diagrams/{page_id}/{hash}.png — PUBLIC, no session. Reachable
//     from any context including share-mode. Content-addressed by hash, so the
//     response carries Cache-Control: immutable + a strong ETag and respects
//     If-None-Match (Cloudflare in front of tela.cagdas.io will revalidate).
//
// The public GET prefix /api/diagrams/ is added to auth.IsPublicPath in the
// same shape as /p/* (M11.0) and /api/share/* (M15.0) — page-derived image
// served under an opaque content-addressed URL. Putting it on its own
// /api/diagrams/* prefix avoids regex special-casing under /api/pages/*
// (which is session-gated as a whole).

const (
	pageDiagramMaxUploadBytes = 8 << 20 // 8 MiB cap on PUT body
)

// pageDiagramHashRE is the application-side validation for the scene_hash:
// 8–64 lowercase hex chars (sha256 truncations / fingerprints). Validating
// here keeps the SQL clean and the error message useful; the GET route also
// uses this to defend against path-traversal in the {hash} path value.
var pageDiagramHashRE = regexp.MustCompile(`^[a-f0-9]{8,64}$`)

// pngMagic is the canonical 4-byte PNG signature prefix. We do not verify the
// IHDR/IEND chunks — just the header — because (a) image/png decode would
// double the work and (b) clients always render with @excalidraw/excalidraw's
// exportToBlob which emits standard PNG.
var pngMagic = []byte{0x89, 'P', 'N', 'G'}

type pageDiagramUploadRequest struct {
	SceneHash string `json:"scene_hash"`
	PNGBase64 string `json:"png_base64"`
}

type pageDiagramUploadResponse struct {
	ID        int64  `json:"id"`
	PageID    int64  `json:"page_id"`
	SceneHash string `json:"scene_hash"`
	ByteSize  int64  `json:"byte_size"`
	URL       string `json:"url"`
}

// UploadPageDiagram handles PUT /api/pages/{id}/diagrams.
//
// Membership gate: editor+ on the page's space. Body must be JSON with a
// well-formed scene_hash and a base64-encoded PNG within the 8 MiB cap.
// Idempotent: a second PUT with the same scene_hash returns the original
// row's id.
func (s *Server) UploadPageDiagram(w http.ResponseWriter, r *http.Request) {
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

	// MaxBytesReader caps the request body so a malicious or buggy client
	// can't pin a multi-GiB buffer in memory. 8 MiB is generous — a typical
	// Excalidraw scene PNG is well under 1 MiB.
	var req pageDiagramUploadRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, pageDiagramMaxUploadBytes)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not parse request body")
		return
	}

	if !pageDiagramHashRE.MatchString(req.SceneHash) {
		writeError(w, http.StatusBadRequest, "bad_request", "scene_hash must be 8-64 lowercase hex chars")
		return
	}

	pngBytes, err := base64.StdEncoding.DecodeString(req.PNGBase64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "png_base64 must be valid base64")
		return
	}
	if len(pngBytes) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "png_base64 decoded to empty")
		return
	}
	if !bytes.HasPrefix(pngBytes, pngMagic) {
		writeError(w, http.StatusBadRequest, "bad_request", "decoded bytes are not a PNG")
		return
	}

	// Idempotent upsert: ON CONFLICT DO NOTHING leaves the existing row
	// alone; we re-query for the canonical id either way. Updating the PNG
	// on conflict would defeat the content-addressing assumption (same hash
	// must always map to the same bytes).
	if _, err := s.DB.ExecContext(ctx, `
		INSERT INTO page_diagrams (page_id, scene_hash, png, byte_size)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(page_id, scene_hash) DO NOTHING`,
		pageID, req.SceneHash, pngBytes, int64(len(pngBytes))); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "upsert diagram failed")
		return
	}

	var (
		rowID    int64
		byteSize int64
	)
	if err := s.DB.QueryRowContext(ctx, `
		SELECT id, byte_size FROM page_diagrams
		 WHERE page_id = ? AND scene_hash = ?`,
		pageID, req.SceneHash).Scan(&rowID, &byteSize); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch diagram failed")
		return
	}

	writeJSON(w, http.StatusOK, pageDiagramUploadResponse{
		ID:        rowID,
		PageID:    pageID,
		SceneHash: req.SceneHash,
		ByteSize:  byteSize,
		URL:       "/api/diagrams/" + strconv.FormatInt(pageID, 10) + "/" + req.SceneHash + ".png",
	})
}

// ServePageDiagramPNG handles GET /api/diagrams/{page_id}/{hash}.png.
//
// Public — no session required. Validates path values against the hex regex
// before touching SQL; mismatches return 404 (not 400) to avoid offering an
// enumeration oracle. Sends Cache-Control: immutable because the URL is
// content-addressed; the same hash MUST always map to the same bytes.
func (s *Server) ServePageDiagramPNG(w http.ResponseWriter, r *http.Request) {
	pageIDRaw := r.PathValue("page_id")
	pageID, err := strconv.ParseInt(pageIDRaw, 10, 64)
	if err != nil || pageID <= 0 {
		writeError(w, http.StatusNotFound, "not_found", "diagram not found")
		return
	}
	// Go 1.22+ mux wildcards must end a segment, so .png is part of {file}
	// rather than a literal pattern suffix. Strip-and-validate here; a path
	// without .png or with a non-hex prefix collapses to the same 404 as a
	// missing row so the response cannot serve as an enumeration oracle.
	file := r.PathValue("file")
	hash, ok := strings.CutSuffix(file, ".png")
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "diagram not found")
		return
	}
	if !pageDiagramHashRE.MatchString(hash) {
		writeError(w, http.StatusNotFound, "not_found", "diagram not found")
		return
	}

	etag := `"` + hash + `"`
	if r.Header.Get("If-None-Match") == etag {
		// Headers must be set before WriteHeader. Cache-Control is repeated
		// here so a 304 stays revalidatable from the same origin response.
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.WriteHeader(http.StatusNotModified)
		return
	}

	var png []byte
	err = s.DB.QueryRowContext(r.Context(), `
		SELECT png FROM page_diagrams
		 WHERE page_id = ? AND scene_hash = ?
		 LIMIT 1`, pageID, hash).Scan(&png)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "diagram not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch diagram failed")
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Length", strconv.Itoa(len(png)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(png)
}
