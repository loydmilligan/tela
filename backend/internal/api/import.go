package api

import (
	"database/sql"
	"errors"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"

	"github.com/zcag/tela/backend/internal/mdimport"
)

const importMaxMemory = 32 << 20 // 32 MiB in-memory; larger parts spool to disk.

// ImportSpace handles POST /api/spaces/{id}/import — bulk-creates pages from a
// markdown bundle. Editor-or-owner role required on the target space. The
// multipart body carries (parent_id, dry_run, files[…]) where each file part's
// Content-Disposition `filename` is the relative path under the upload root.
func (s *Server) ImportSpace(w http.ResponseWriter, r *http.Request) {
	spaceID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}

	if err := r.ParseMultipartForm(importMaxMemory); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not parse multipart form")
		return
	}

	var parentID *int64
	if raw := r.FormValue("parent_id"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v <= 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "parent_id must be a positive integer")
			return
		}
		parentID = &v
	}

	dryRun := false
	if raw := r.FormValue("dry_run"); raw == "true" || raw == "1" {
		dryRun = true
	}

	fileHeaders := r.MultipartForm.File["files"]
	if len(fileHeaders) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "no files in upload")
		return
	}

	files := make([]mdimport.ImportFile, 0, len(fileHeaders))
	for _, fh := range fileHeaders {
		f, err := fh.Open()
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "could not open uploaded file")
			return
		}
		content, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "could not read uploaded file")
			return
		}
		files = append(files, mdimport.ImportFile{Path: rawFilename(fh), Content: content})
	}

	ctx := r.Context()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "begin tx failed")
		return
	}
	defer tx.Rollback()

	if err := verifySpaceExistsTx(ctx, tx, spaceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "space_not_found", "space not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "lookup space failed")
		return
	}
	if !enforceAPIKeySpaceScope(w, r, spaceID) {
		return
	}

	role, err := spaceRoleTx(ctx, tx, u.ID, spaceID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusForbidden, "forbidden", "not a member")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup membership failed")
		return
	}
	if !canEdit(role) {
		writeError(w, http.StatusForbidden, "viewer_no_write", "viewers cannot import")
		return
	}

	if parentID != nil {
		var parentSpaceID int64
		err := tx.QueryRowContext(ctx,
			`SELECT space_id FROM pages WHERE id = $1 AND deleted_at IS NULL`, *parentID).Scan(&parentSpaceID)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusBadRequest, "parent_not_found", "parent page does not exist")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "lookup parent failed")
			return
		}
		if parentSpaceID != spaceID {
			writeError(w, http.StatusBadRequest, "parent_space_mismatch", "parent page is in a different space")
			return
		}
	}

	result, err := mdimport.Import(ctx, tx, spaceID, parentID, u.ID, files, dryRun)
	if err != nil {
		log.Printf("import space %d: %v", spaceID, err)
		writeError(w, http.StatusInternalServerError, "internal", "import failed")
		return
	}

	if !dryRun {
		if err := tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "commit failed")
			return
		}
		// Index every imported page (debounced, async; no-op when RAG is off).
		for _, p := range result.Pages {
			if p.ID > 0 {
				s.rag.QueueReindex(p.ID)
			}
		}
	}
	writeJSON(w, http.StatusOK, result)
}

// rawFilename returns the relative path as written by the client in the
// Content-Disposition header. We cannot use FileHeader.Filename directly:
// multipart.Reader runs the value through filepath.Base before stowing it
// on the FileHeader, which strips the leading directories that the FE
// encodes via file.webkitRelativePath.
func rawFilename(fh *multipart.FileHeader) string {
	raw := fh.Header.Get("Content-Disposition")
	if raw == "" {
		return fh.Filename
	}
	_, params, err := mime.ParseMediaType(raw)
	if err != nil {
		return fh.Filename
	}
	if v := params["filename"]; v != "" {
		return v
	}
	return fh.Filename
}
