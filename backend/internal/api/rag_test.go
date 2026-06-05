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
