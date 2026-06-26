// Package store is the Postgres implementation of the atlas engine's persistence
// seam (engine.EngineStore). Standalone atlas backed this with SQLite + float32
// LE blobs; inside tela the run-scoped atlas_* tables (migration 0048) back it,
// vectors live in a pgvector(1024) column, and timestamps are TEXT in tela's
// canonical 'YYYY-MM-DD HH:MM:SS' UTC form. The method semantics — column
// mapping, ordering, JSON-blob columns, id-backfill on insert — mirror the
// reference store verbatim; only SQLite→Postgres and blob→pgvector change.
package store

import (
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/zcag/tela/backend/internal/atlas/core"
	"github.com/zcag/tela/backend/internal/atlas/engine"
)

// tsLayout is tela's canonical TEXT timestamp wire format (UTC), the same one
// the rest of the backend stores Go times as (see internal/api/share_links.go).
const tsLayout = "2006-01-02 15:04:05"

// Store implements engine.EngineStore over the atlas_* Postgres tables.
type Store struct{ db *sql.DB }

// New wraps a pgx-backed *sql.DB. The pool is owned by the caller.
func New(db *sql.DB) *Store { return &Store{db: db} }

// compile-time: *Store must satisfy the engine's persistence seam.
var _ engine.EngineStore = (*Store)(nil)

// fmtTime renders a Go time as a TEXT timestamp, mapping the zero value to ""
// (empty = unset, e.g. an unfinished run's finished_at).
func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(tsLayout)
}

// parseTime is the inverse of fmtTime: "" → zero, otherwise the parsed UTC time.
func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, _ := time.Parse(tsLayout, s)
	return t
}

// --- progress + run lifecycle ---

// AppendEvent persists one progress event (the durable side of the live stream).
func (s *Store) AppendEvent(e core.Event) error {
	_, err := s.db.Exec(
		`INSERT INTO atlas_run_events(run_id,stage,level,msg,cur,total,at) VALUES($1,$2,$3,$4,$5,$6,$7)`,
		e.RunID, e.Stage, e.Level, e.Msg, e.Cur, e.Total, fmtTime(e.At))
	return err
}

// UpdateRun writes back a run's mutable lifecycle fields.
func (s *Store) UpdateRun(r *core.Run) error {
	_, err := s.db.Exec(
		`UPDATE atlas_runs SET status=$1,stage=$2,err=$3,finished_at=$4 WHERE id=$5`,
		r.Status, r.Stage, r.Err, fmtTime(r.FinishedAt), r.ID)
	return err
}

const runCols = `id,source_id,kind,baseline_id,changeset_json,status,stage,err,coverage_json,stats_json,started_at,finished_at`

// GetRun returns one run by id, decoding its JSON-blob columns (changeset,
// coverage, stats) the way the reference store does.
func (s *Store) GetRun(id int64) (*core.Run, error) {
	var r core.Run
	var started, fin, cs, cov, stats string
	err := s.db.QueryRow(`SELECT `+runCols+` FROM atlas_runs WHERE id=$1`, id).Scan(
		&r.ID, &r.SourceID, &r.Kind, &r.BaselineID, &cs, &r.Status, &r.Stage, &r.Err, &cov, &stats, &started, &fin)
	if err != nil {
		return nil, err
	}
	if r.Kind == "" {
		r.Kind = core.RunFull
	}
	if cs != "" {
		var c core.ChangeSet
		if json.Unmarshal([]byte(cs), &c) == nil {
			r.ChangeSet = &c
		}
	}
	if cov != "" {
		var c core.Coverage
		if json.Unmarshal([]byte(cov), &c) == nil {
			r.Coverage = &c
		}
	}
	if stats != "" {
		var st core.RunStats
		if json.Unmarshal([]byte(stats), &st) == nil {
			r.Stats = &st
		}
	}
	r.StartedAt = parseTime(started)
	r.FinishedAt = parseTime(fin)
	return &r, nil
}

// SetSourceRef pins a source's last-seen commit/revision.
func (s *Store) SetSourceRef(id int64, ref string) error {
	_, err := s.db.Exec(`UPDATE atlas_sources SET ref=$1 WHERE id=$2`, ref, id)
	return err
}

// SaveRunCoverage persists a run's coverage audit (so the UI can render it
// without re-reading disk).
func (s *Store) SaveRunCoverage(runID int64, c core.Coverage) error {
	b, _ := json.Marshal(c)
	_, err := s.db.Exec(`UPDATE atlas_runs SET coverage_json=$1 WHERE id=$2`, string(b), runID)
	return err
}

// SaveRunStats persists a run's stats report (counts, tokens, cost).
func (s *Store) SaveRunStats(runID int64, st core.RunStats) error {
	b, _ := json.Marshal(st)
	_, err := s.db.Exec(`UPDATE atlas_runs SET stats_json=$1 WHERE id=$2`, string(b), runID)
	return err
}

// --- ingestion artifacts ---

// SaveFiles bulk-inserts a run's files and back-fills their IDs.
func (s *Store) SaveFiles(runID int64, files []core.File) error {
	return s.bulk(`INSERT INTO atlas_files(run_id,path,lang,size,lines,hash) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`,
		len(files), func(stmt *sql.Stmt, i int) error {
			return stmt.QueryRow(runID, files[i].Path, files[i].Lang, files[i].Size, files[i].Lines, files[i].Hash).Scan(&files[i].ID)
		})
}

// RunFiles returns a run's inventoried files (with content hashes).
func (s *Store) RunFiles(runID int64) ([]core.File, error) {
	rows, err := s.db.Query(`SELECT id,path,lang,size,lines,hash FROM atlas_files WHERE run_id=$1 ORDER BY id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.File
	for rows.Next() {
		var f core.File
		if err := rows.Scan(&f.ID, &f.Path, &f.Lang, &f.Size, &f.Lines, &f.Hash); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// SaveSpine bulk-inserts the surface inventory and back-fills IDs.
func (s *Store) SaveSpine(runID int64, items []core.SpineItem) error {
	return s.bulk(`INSERT INTO atlas_symbols(run_id,kind,name,file,line,detail) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`,
		len(items), func(stmt *sql.Stmt, i int) error {
			return stmt.QueryRow(runID, items[i].Kind, items[i].Name, items[i].File, items[i].Line, items[i].Detail).Scan(&items[i].ID)
		})
}

// RunSpine returns a run's extracted surface inventory, ordered by kind,name
// (mirrors the reference store's order).
func (s *Store) RunSpine(runID int64) ([]core.SpineItem, error) {
	rows, err := s.db.Query(`SELECT id,kind,name,file,line,detail FROM atlas_symbols WHERE run_id=$1 ORDER BY kind,name`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.SpineItem
	for rows.Next() {
		var it core.SpineItem
		if err := rows.Scan(&it.ID, &it.Kind, &it.Name, &it.File, &it.Line, &it.Detail); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// SaveChunks bulk-inserts chunks (without vectors) and back-fills their IDs.
// The embed stage later keys SaveVectors by these ids, so the back-fill is
// load-bearing.
func (s *Store) SaveChunks(runID int64, chunks []core.Chunk) error {
	return s.bulk(`INSERT INTO atlas_chunks(run_id,file,start_line,end_line,kind,symbol,text) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		len(chunks), func(stmt *sql.Stmt, i int) error {
			return stmt.QueryRow(runID, chunks[i].File, chunks[i].StartLine, chunks[i].EndLine, chunks[i].Kind, chunks[i].Symbol, chunks[i].Text).Scan(&chunks[i].ID)
		})
}

// SaveVectors persists each chunk's embedding by chunk id. The vector crosses
// database/sql as a pgvector text literal cast with ::vector (tela's house
// style — no driver-level vector type).
func (s *Store) SaveVectors(chunks []core.Chunk) error {
	return s.bulk(`UPDATE atlas_chunks SET embedding=$1::vector WHERE id=$2`, len(chunks),
		func(stmt *sql.Stmt, i int) error {
			_, err := stmt.Exec(vecLiteral(chunks[i].Vector), chunks[i].ID)
			return err
		})
}

// RunChunksWithVectors loads a run's chunks WITH their embedding (decoded from
// the pgvector column via ::text). It's the read side of resume + delta reuse;
// ordered by id for a stable order (mirrors the reference store).
func (s *Store) RunChunksWithVectors(runID int64) ([]core.Chunk, error) {
	rows, err := s.db.Query(
		`SELECT id,file,start_line,end_line,kind,symbol,text,embedding::text FROM atlas_chunks WHERE run_id=$1 ORDER BY id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.Chunk
	for rows.Next() {
		var c core.Chunk
		var vec sql.NullString
		if err := rows.Scan(&c.ID, &c.File, &c.StartLine, &c.EndLine, &c.Kind, &c.Symbol, &c.Text, &vec); err != nil {
			return nil, err
		}
		if vec.Valid {
			c.Vector = parseVec(vec.String)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CopyChunksToRun copies the baseline run's chunks (with their vectors) for the
// given files into toRunID, returning the number copied. The copy-forward half
// of delta reuse: unchanged files keep their already-embedded chunks rather than
// being re-chunked and re-embedded.
func (s *Store) CopyChunksToRun(fromRunID, toRunID int64, files []string) (int, error) {
	if len(files) == 0 {
		return 0, nil
	}
	src, err := s.RunChunksWithVectors(fromRunID)
	if err != nil {
		return 0, err
	}
	keep := make(map[string]bool, len(files))
	for _, f := range files {
		keep[f] = true
	}
	var carry []core.Chunk
	for _, c := range src {
		if keep[c.File] {
			carry = append(carry, c)
		}
	}
	err = s.bulk(`INSERT INTO atlas_chunks(run_id,file,start_line,end_line,kind,symbol,text,embedding) VALUES($1,$2,$3,$4,$5,$6,$7,$8::vector) RETURNING id`,
		len(carry), func(stmt *sql.Stmt, i int) error {
			c := carry[i]
			var vec any // NULL when the baseline chunk was never embedded
			if len(c.Vector) > 0 {
				vec = vecLiteral(c.Vector)
			}
			return stmt.QueryRow(toRunID, c.File, c.StartLine, c.EndLine, c.Kind, c.Symbol, c.Text, vec).Scan(&carry[i].ID)
		})
	if err != nil {
		return 0, err
	}
	return len(carry), nil
}

// --- pages ---

// SavePages bulk-inserts planned pages and back-fills their IDs. Topics and
// spine-kinds persist as JSON so a resumed run can rehydrate the page plan.
// The id back-fill is load-bearing: publish calls UpdatePageBody by page.ID.
func (s *Store) SavePages(runID int64, pages []core.Page) error {
	return s.bulk(`INSERT INTO atlas_pages(run_id,ord,kind,title,slug,summary,topics_json,spine_kinds_json,body) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		len(pages), func(stmt *sql.Stmt, i int) error {
			topics, _ := json.Marshal(pages[i].Topics)
			kinds, _ := json.Marshal(pages[i].SpineKinds)
			return stmt.QueryRow(runID, pages[i].Order, pages[i].Kind, pages[i].Title, pages[i].Slug, pages[i].Summary, string(topics), string(kinds), pages[i].Body).Scan(&pages[i].ID)
		})
}

// UpdatePageBody stores a page's generated markdown.
func (s *Store) UpdatePageBody(pageID int64, body string) error {
	_, err := s.db.Exec(`UPDATE atlas_pages SET body=$1 WHERE id=$2`, body, pageID)
	return err
}

// RunPagesFull returns a run's pages WITH bodies and full plan (topics +
// spine-kinds), ordered — used for delivery and resume rehydration.
func (s *Store) RunPagesFull(runID int64) ([]core.Page, error) {
	rows, err := s.db.Query(
		`SELECT id,ord,kind,title,slug,summary,topics_json,spine_kinds_json,body FROM atlas_pages WHERE run_id=$1 ORDER BY ord`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.Page
	for rows.Next() {
		var p core.Page
		var topics, kinds string
		if err := rows.Scan(&p.ID, &p.Order, &p.Kind, &p.Title, &p.Slug, &p.Summary, &topics, &kinds, &p.Body); err != nil {
			return nil, err
		}
		if topics != "" {
			_ = json.Unmarshal([]byte(topics), &p.Topics)
		}
		if kinds != "" {
			_ = json.Unmarshal([]byte(kinds), &p.SpineKinds)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// --- helpers ---

// bulk runs n prepared-statement operations inside one transaction. do receives
// the prepared *sql.Stmt and the row index; RETURNING-based inserts Scan the new
// id back onto the element (pgx has no LastInsertId — RETURNING is the idiom).
func (s *Store) bulk(query string, n int, do func(*sql.Stmt, int) error) error {
	if n == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(query)
	if err != nil {
		tx.Rollback()
		return err
	}
	for i := 0; i < n; i++ {
		if err := do(stmt, i); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// vecLiteral formats a float32 slice as a pgvector text literal ("[0.1,0.2]"),
// the same encoding tela's rag package uses. pgvector parses it on a ::vector
// cast, so the value crosses database/sql as a plain string — no driver type.
func vecLiteral(v []float32) string {
	var b strings.Builder
	b.Grow(len(v) * 8)
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}

// parseVec is the inverse of vecLiteral: a pgvector ::text rendering ("[..]")
// back into a float32 slice. The blob-decode equivalent of the SQLite store, in
// pgvector's text form.
func parseVec(s string) []float32 {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]float32, len(parts))
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return nil
		}
		out[i] = float32(f)
	}
	return out
}
