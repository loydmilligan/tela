package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"hash/fnv"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

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
