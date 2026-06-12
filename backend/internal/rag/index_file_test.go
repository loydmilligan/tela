package rag

import (
	"context"
	"database/sql"
	"testing"

	"github.com/zcag/tela/backend/internal/testdb"
)

func seedRagFile(t *testing.T, d *sql.DB, spaceID int64, name, mime string, data []byte) int64 {
	t.Helper()
	var id int64
	if err := d.QueryRow(`INSERT INTO space_files
		(space_id, parent_page_id, name, content_hash, mime, data, byte_size)
		VALUES ($1, NULL, $2, $2, $3, $4, $5) RETURNING id`,
		spaceID, name, mime, data, len(data)).Scan(&id); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	return id
}

func TestReindexFile(t *testing.T) {
	d := testdb.New(t)
	var spaceID int64
	if err := d.QueryRow(`INSERT INTO spaces (name, slug) VALUES ('S','s') RETURNING id`).Scan(&spaceID); err != nil {
		t.Fatal(err)
	}
	body := []byte("# Project Notes\n\nThe Kafka streaming pipeline ingests events. " +
		"The latency budget is 200ms. The quick brown fox jumps over the lazy dog.")
	fileID := seedRagFile(t, d, spaceID, "notes.md", "text/markdown", body)

	emb := &fakeEmbedder{}
	svc := NewServiceWithEmbedder(d, emb)

	n, err := svc.ReindexFile(context.Background(), fileID)
	if err != nil {
		t.Fatalf("ReindexFile: %v", err)
	}
	if n == 0 {
		t.Fatalf("expected chunks for a text file, got 0")
	}
	var cnt, withVec, withTsv int
	d.QueryRow(`SELECT count(*) FROM file_chunks WHERE space_file_id=$1`, fileID).Scan(&cnt)
	d.QueryRow(`SELECT count(*) FROM file_chunks WHERE space_file_id=$1 AND embedding IS NOT NULL`, fileID).Scan(&withVec)
	d.QueryRow(`SELECT count(*) FROM file_chunks WHERE space_file_id=$1 AND content_tsv IS NOT NULL`, fileID).Scan(&withTsv)
	if cnt != n || withVec != n || withTsv != n {
		t.Fatalf("rows=%d vec=%d tsv=%d want %d each", cnt, withVec, withTsv, n)
	}

	// Idempotent: a second reindex reuses cached vectors (no new embed calls).
	before := emb.calls
	if _, err := svc.ReindexFile(context.Background(), fileID); err != nil {
		t.Fatal(err)
	}
	if emb.calls != before {
		t.Fatalf("reindex re-embedded %d chunks; cache not reused", emb.calls-before)
	}

	// Not text-extractable → zero chunks.
	imgID := seedRagFile(t, d, spaceID, "pic.png", "image/png", []byte("\x89PNG not really text"))
	if n, err := svc.ReindexFile(context.Background(), imgID); err != nil || n != 0 {
		t.Fatalf("image ReindexFile n=%d err=%v want 0,nil", n, err)
	}

	// Soft-delete clears the file's chunks.
	d.Exec(`UPDATE space_files SET deleted_at = tela_now() WHERE id = $1`, fileID)
	if _, err := svc.ReindexFile(context.Background(), fileID); err != nil {
		t.Fatal(err)
	}
	d.QueryRow(`SELECT count(*) FROM file_chunks WHERE space_file_id=$1`, fileID).Scan(&cnt)
	if cnt != 0 {
		t.Fatalf("soft-deleted file still has %d chunks", cnt)
	}
}
