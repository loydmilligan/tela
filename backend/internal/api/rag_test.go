package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/zcag/tela/backend/internal/auth"
	"github.com/zcag/tela/backend/internal/rag"
)

// fakeEmb is a deterministic 1024-d bag-of-words embedder (matches the
// page_chunks vector(1024) column) so RAG HTTP tests run without Ollama.
type fakeEmb struct{}

func (fakeEmb) Model() string { return "fake" }
func (fakeEmb) Embed(_ context.Context, text string) ([]float32, error) {
	v := make([]float32, 1024)
	for _, w := range strings.Fields(strings.ToLower(text)) {
		h := fnv.New32a()
		h.Write([]byte(w))
		v[h.Sum32()%1024]++
	}
	return v, nil
}

// newRagServer wires a server with an injected fake embedder so the RAG routes
// are Enabled() without a live Ollama.
func newRagServer(t *testing.T) (*httptest.Server, *sql.DB, *Server) {
	t.Helper()
	t.Setenv("TELA_SHARE_SECRET", "tela-test-share-secret-fixed-32-byte!")
	d := newAPITestDB(t)
	h, srv := HandlerWithServer(d)
	srv.rag = rag.NewServiceWithEmbedder(d, fakeEmb{})
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts, d, srv
}

// A request the client aborts mid-flight (canceled request context) must NOT be
// a 5xx. The frontend cancels a superseded search-as-you-type fetch; the backend
// must map that context.Canceled to 499, not 500, so normal typing never trips
// the "5xx spike" alert. Regression guard for the telawiki.com false alarm traced
// to /api/rag/search returning 500 on client aborts.
func TestRAGSearch_ClientCancelIs499(t *testing.T) {
	_, d, srv := newRagServer(t)
	uid := seedUser(t, d, "alice", "alicepw12", false)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // client went away before the search finished
	ctx = auth.WithUser(ctx, &auth.User{ID: uid})

	req := httptest.NewRequest(http.MethodGet, "/api/rag/search?q=anything", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	srv.RAGSearch(rec, req)

	if rec.Code != statusClientClosed {
		t.Fatalf("status = %d, want %d (client closed request) — a canceled request must not surface as a 5xx", rec.Code, statusClientClosed)
	}
}

// clientCanceled swallows ONLY a canceled request context; a live request or an
// unrelated error must still surface (and 5xx upstream).
func TestClientCanceled(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	cases := []struct {
		name string
		ctx  context.Context
		err  error
		want bool
		code int
	}{
		{"aborted request", canceled, context.Canceled, true, statusClientClosed},
		{"live request, canceled err", context.Background(), context.Canceled, false, http.StatusOK},
		{"aborted request, other err", canceled, errors.New("boom"), false, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/x", nil).WithContext(tc.ctx)
			w := httptest.NewRecorder()
			if got := clientCanceled(w, r, tc.err); got != tc.want {
				t.Fatalf("clientCanceled = %v, want %v", got, tc.want)
			}
			if w.Code != tc.code {
				t.Fatalf("status = %d, want %d", w.Code, tc.code)
			}
		})
	}
}

func TestRAG_DisabledReturns503(t *testing.T) {
	ts, d := newWiredServer(t) // default Handler → rag unconfigured
	_ = seedUser(t, d, "alice", "alicepw12", false)
	c := loginClient(t, ts, "alice", "alicepw12")
	resp, err := c.Get(ts.URL + "/api/rag/search?q=hi")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 when embedder unconfigured", resp.StatusCode)
	}
}

func TestRAG_ReindexThenSearchScoped(t *testing.T) {
	ts, d, _ := newRagServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)
	bob := seedUser(t, d, "bob", "bobpw1234", false)
	aSpace := seedSpace(t, d, "Alpha", "alpha", alice)
	bSpace := seedSpace(t, d, "Bravo", "bravo", bob)

	mustPage(t, d, aSpace, "Deploy Guide", "## Shipping\nrun make deploy to push the release to production")
	secret := mustPage(t, d, bSpace, "Secret", "## Plans\ndeployment release schedule for the quarter")

	c := loginClient(t, ts, "alice", "alicepw12")

	// Reindex alice's space (she's a member). Bob's space reindex would 403.
	resp, err := c.Post(ts.URL+"/api/rag/reindex?space_id="+strconv.FormatInt(aSpace, 10), "application/json", nil)
	if err != nil {
		t.Fatalf("reindex: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reindex status = %d", resp.StatusCode)
	}

	// Index bob's space too (directly via the service) so we prove the SEARCH
	// scope — not the reindex membership gate — is what hides it from alice.
	if _, _, err := rag.NewServiceWithEmbedder(d, fakeEmb{}).ReindexSpace(context.Background(), bSpace); err != nil {
		t.Fatalf("seed bob index: %v", err)
	}

	resp, err = c.Get(ts.URL + "/api/rag/search?q=deploy+release")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("search status = %d: %s", resp.StatusCode, b)
	}
	var out struct {
		Results []rag.Hit `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Results) == 0 {
		t.Fatal("expected results")
	}
	for _, h := range out.Results {
		if h.PageID == secret {
			t.Fatalf("LEAK: alice's search returned bob's page %d", secret)
		}
		if h.SpaceID != aSpace {
			t.Errorf("result from unexpected space %d", h.SpaceID)
		}
	}
}

func TestRAG_ReadChunkScoped(t *testing.T) {
	ts, d, _ := newRagServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)
	bob := seedUser(t, d, "bob", "bobpw1234", false)
	aSpace := seedSpace(t, d, "Alpha", "alpha", alice)
	bSpace := seedSpace(t, d, "Bravo", "bravo", bob)
	mustPage(t, d, aSpace, "Guide", "## Shipping\nrun make deploy to push the release")
	mustPage(t, d, bSpace, "Secret", "## Plans\nthe secret deployment roadmap")

	svc := rag.NewServiceWithEmbedder(d, fakeEmb{})
	if _, _, err := svc.ReindexSpace(context.Background(), aSpace); err != nil {
		t.Fatalf("index a: %v", err)
	}
	if _, _, err := svc.ReindexSpace(context.Background(), bSpace); err != nil {
		t.Fatalf("index b: %v", err)
	}

	c := loginClient(t, ts, "alice", "alicepw12")

	// Search → take a chunk_id → read it back in full.
	resp, _ := c.Get(ts.URL + "/api/rag/search?q=deploy+release")
	var sr struct {
		Results []rag.Hit `json:"results"`
	}
	json.NewDecoder(resp.Body).Decode(&sr)
	resp.Body.Close()
	if len(sr.Results) == 0 {
		t.Fatal("no search results")
	}
	cid := sr.Results[0].ChunkID

	resp, err := c.Get(ts.URL + "/api/rag/chunk?chunk_id=" + strconv.FormatInt(cid, 10))
	if err != nil {
		t.Fatalf("read chunk: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("read chunk status = %d", resp.StatusCode)
	}
	var rr struct {
		Chunk rag.ChunkRead `json:"chunk"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rr.Chunk.Content == "" || rr.Chunk.SpaceID != aSpace {
		t.Errorf("unexpected chunk: %+v", rr.Chunk)
	}

	// A bob chunk id must 404 for alice (find it by indexing bob's space and
	// scanning a high id range is overkill — instead assert a known-out-of-scope
	// id behaves as not-found via a probe of bob's chunk).
	var bobChunk int64
	if err := d.QueryRow(`SELECT pc.id FROM page_chunks pc JOIN pages p ON p.id=pc.page_id WHERE p.space_id=$1 LIMIT 1`, bSpace).Scan(&bobChunk); err != nil {
		t.Fatalf("find bob chunk: %v", err)
	}
	resp2, _ := c.Get(ts.URL + "/api/rag/chunk?chunk_id=" + strconv.FormatInt(bobChunk, 10))
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Fatalf("LEAK: alice read bob's chunk %d → status %d (want 404)", bobChunk, resp2.StatusCode)
	}
}

func TestRAG_Freshness(t *testing.T) {
	ts, d, _ := newRagServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)
	aSpace := seedSpace(t, d, "Alpha", "alpha", alice)
	indexed := mustPage(t, d, aSpace, "Indexed", "## A\nreal content here")
	_ = mustPage(t, d, aSpace, "Unindexed", "## B\nnot indexed yet")

	if _, err := rag.NewServiceWithEmbedder(d, fakeEmb{}).ReindexPage(context.Background(), indexed); err != nil {
		t.Fatalf("index: %v", err)
	}

	c := loginClient(t, ts, "alice", "alicepw12")
	resp, err := c.Get(ts.URL + "/api/rag/freshness")
	if err != nil {
		t.Fatalf("freshness: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var out struct {
		Enabled bool                 `json:"enabled"`
		Spaces  []rag.SpaceFreshness `json:"spaces"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !out.Enabled {
		t.Error("expected enabled=true (fake embedder injected)")
	}
	var f *rag.SpaceFreshness
	for i := range out.Spaces {
		if out.Spaces[i].SpaceID == aSpace {
			f = &out.Spaces[i]
		}
	}
	if f == nil {
		t.Fatal("alpha space missing from freshness")
	}
	if f.Pages != 2 || f.IndexedPages != 1 || f.StalePages != 1 {
		t.Errorf("got pages=%d indexed=%d stale=%d, want 2/1/1", f.Pages, f.IndexedPages, f.StalePages)
	}
}

// mustFile seeds a space_files row (an attachment) under a parent page. Returns
// the file id. content_hash is just the name (unique enough for a test).
// TestDeletePage_TearsDownAttachmentIndex pins the orphan fix: deleting a page
// must soft-delete its attachments and clear their RAG index, so a deleted page's
// files stop surfacing in search and stop citing a now-deleted page.
func TestDeletePage_TearsDownAttachmentIndex(t *testing.T) {
	ts, d, srv := newRagServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)
	aSpace := seedSpace(t, d, "Alpha", "alpha", alice)
	page := mustPage(t, d, aSpace, "Folder", "## x\nplain page body")
	fileID := mustFile(t, d, aSpace, page, "doc.md", "text/markdown",
		"# Doc\n\nThe Quibblesnatch protocol streams parcels through the Vornblat hub.")
	if _, err := srv.rag.ReindexFile(context.Background(), fileID); err != nil {
		t.Fatalf("reindex: %v", err)
	}
	var n int
	d.QueryRow(`SELECT count(*) FROM file_chunks WHERE space_file_id=$1`, fileID).Scan(&n)
	if n == 0 {
		t.Fatal("file not indexed before delete")
	}

	c := loginClient(t, ts, "alice", "alicepw12")
	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/pages/%d", ts.URL, page), nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}

	// Chunks gone, file soft-deleted, no longer retrievable.
	d.QueryRow(`SELECT count(*) FROM file_chunks WHERE space_file_id=$1`, fileID).Scan(&n)
	if n != 0 {
		t.Errorf("file chunks survived page delete: %d", n)
	}
	var del sql.NullString
	d.QueryRow(`SELECT deleted_at FROM space_files WHERE id=$1`, fileID).Scan(&del)
	if !del.Valid {
		t.Error("attachment not soft-deleted with its page")
	}
	hits, err := srv.rag.Search(context.Background(), alice, "Quibblesnatch Vornblat parcels", nil, 5, "hybrid")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	for _, h := range hits {
		if h.FileID == fileID {
			t.Errorf("deleted page's attachment still retrievable (chunk %d)", h.ChunkID)
		}
	}
}

func mustFile(t *testing.T, d *sql.DB, spaceID, parentPageID int64, name, mime, body string) int64 {
	t.Helper()
	var id int64
	if err := d.QueryRowContext(context.Background(), `INSERT INTO space_files
		(space_id, parent_page_id, name, content_hash, mime, data, byte_size)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		spaceID, parentPageID, name, name+"hash", mime, []byte(body), len(body)).Scan(&id); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	return id
}

// TestRAG_FileChunks_SearchAndRead exercises the full HTTP retrieval path for
// attachments: a reindexed file surfaces in /api/rag/search with a file citation
// (source_kind, file_name, parent page_id, an absolute download_url), and its
// chunk reads back through /api/rag/chunk.
func TestRAG_FileChunks_SearchAndRead(t *testing.T) {
	ts, d, srv := newRagServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)
	aSpace := seedSpace(t, d, "Alpha", "alpha", alice)
	parent := mustPage(t, d, aSpace, "Vendor", "## Notes\nvendor context")
	fileID := mustFile(t, d, aSpace, parent, "msa.md", "text/markdown",
		"# Master Service Agreement\n\nThe indemnification liability cap is two million dollars.")

	if _, err := srv.rag.ReindexFile(context.Background(), fileID); err != nil {
		t.Fatalf("reindex file: %v", err)
	}

	c := loginClient(t, ts, "alice", "alicepw12")
	resp, err := c.Get(ts.URL + "/api/rag/search?q=indemnification+liability+cap")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	defer resp.Body.Close()
	var out struct {
		Results []rag.Hit `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var fh *rag.Hit
	for i := range out.Results {
		if out.Results[i].SourceKind == "file" {
			fh = &out.Results[i]
		}
	}
	if fh == nil {
		t.Fatalf("no file hit in results: %+v", out.Results)
	}
	if fh.FileID != fileID || fh.FileName != "msa.md" || fh.PageID != parent {
		t.Errorf("file citation wrong: %+v", *fh)
	}
	if fh.DownloadURL == "" || !strings.Contains(fh.DownloadURL, "/api/files/"+strconv.FormatInt(aSpace, 10)+"/") {
		t.Errorf("file hit missing/wrong download_url: %q", fh.DownloadURL)
	}

	// read_chunk routes the file chunk id to file_chunks and cites the file.
	resp2, err := c.Get(ts.URL + "/api/rag/chunk?chunk_id=" + strconv.FormatInt(fh.ChunkID, 10))
	if err != nil {
		t.Fatalf("read chunk: %v", err)
	}
	defer resp2.Body.Close()
	var rr struct {
		Chunk rag.ChunkRead `json:"chunk"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&rr); err != nil {
		t.Fatalf("decode chunk: %v", err)
	}
	if rr.Chunk.SourceKind != "file" || rr.Chunk.FileID != fileID || rr.Chunk.Content == "" {
		t.Errorf("file chunk read wrong: %+v", rr.Chunk)
	}
	if rr.Chunk.DownloadURL == "" {
		t.Errorf("file chunk read missing download_url")
	}
}

func mustPage(t *testing.T, d *sql.DB, spaceID int64, title, body string) int64 {
	t.Helper()
	var id int64
	if err := d.QueryRowContext(context.Background(),
		`INSERT INTO pages (space_id, title, body) VALUES ($1, $2, $3) RETURNING id`,
		spaceID, title, body).Scan(&id); err != nil {
		t.Fatalf("insert page: %v", err)
	}
	return id
}
