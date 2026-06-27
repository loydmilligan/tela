package rag

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zcag/tela/backend/internal/testdb"
)

// queryEmbFake records whether the asymmetric query path was taken.
type queryEmbFake struct {
	fakeEmbedder
	usedEmbedQuery bool
	lastQuery      string
}

func (q *queryEmbFake) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	q.usedEmbedQuery = true
	q.lastQuery = query
	return q.fakeEmbedder.Embed(ctx, query)
}

// TestEmbedQuery_InstructionPrefix proves the Ollama embedder wraps a SEARCH
// query in the asymmetric instruction prefix (and passes it through raw when the
// instruction is disabled), while passages embed bare.
func TestEmbedQuery_InstructionPrefix(t *testing.T) {
	var gotInput string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Input string `json:"input"`
		}
		_ = json.Unmarshal(body, &req)
		gotInput = req.Input
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{{0.1, 0.2}}})
	}))
	defer srv.Close()

	emb := NewOllamaEmbedder(srv.URL, "qwen3-embedding:0.6b", "")
	if _, err := emb.EmbedQuery(context.Background(), "how do we deploy"); err != nil {
		t.Fatalf("EmbedQuery: %v", err)
	}
	if !strings.HasPrefix(gotInput, "Instruct: ") || !strings.Contains(gotInput, "\nQuery:how do we deploy") {
		t.Fatalf("query not instruction-wrapped: %q", gotInput)
	}

	// A blank instruction disables the prefix (raw passthrough — e.g. mxbai).
	emb.instruct = ""
	if _, err := emb.EmbedQuery(context.Background(), "raw query"); err != nil {
		t.Fatalf("EmbedQuery (no instruct): %v", err)
	}
	if gotInput != "raw query" {
		t.Fatalf("expected raw query with instruct disabled, got %q", gotInput)
	}
}

// TestSearch_UsesAsymmetricQueryPath proves semantic search routes the query
// through EmbedQuery (the instructed path), not the passage Embed.
func TestSearch_UsesAsymmetricQueryPath(t *testing.T) {
	d := testdb.New(t)
	ctx := context.Background()
	u := newUser(t, d, "alice")
	sp := newSpace(t, d, "alpha", u)
	newPage(t, d, sp, "Doc", "## A\nsome searchable content here")

	emb := &queryEmbFake{}
	svc := NewServiceWithEmbedder(d, emb)
	if _, _, err := svc.ReindexSpace(ctx, sp); err != nil {
		t.Fatalf("reindex: %v", err)
	}
	if _, err := svc.Search(ctx, u, "content", nil, 5, "semantic"); err != nil {
		t.Fatalf("search: %v", err)
	}
	if !emb.usedEmbedQuery {
		t.Fatal("semantic search did not route through EmbedQuery (asymmetric path)")
	}
}

// TestChunkContents_ScopedToAccess proves the full-chunk fetch used to ground
// "ask your docs" honors space_access — bob can't read a chunk in alice's space.
func TestChunkContents_ScopedToAccess(t *testing.T) {
	d := testdb.New(t)
	ctx := context.Background()
	alice := newUser(t, d, "alice")
	bob := newUser(t, d, "bob")
	sp := newSpace(t, d, "alpha", alice) // alice's space; bob has no access
	page := newPage(t, d, sp, "Secret", "## A\nconfidential content")

	svc := NewServiceWithEmbedder(d, &fakeEmbedder{})
	if _, err := svc.ReindexPage(ctx, page); err != nil {
		t.Fatalf("index: %v", err)
	}
	var chunkID int64
	if err := d.QueryRow(`SELECT id FROM page_chunks WHERE page_id=$1 LIMIT 1`, page).Scan(&chunkID); err != nil {
		t.Fatalf("chunk id: %v", err)
	}

	owner, err := svc.ChunkContents(ctx, alice, []int64{chunkID}, nil)
	if err != nil {
		t.Fatalf("owner fetch: %v", err)
	}
	if owner[chunkID] == "" {
		t.Fatal("owner could not read her own chunk content")
	}

	leaked, err := svc.ChunkContents(ctx, bob, []int64{chunkID}, nil)
	if err != nil {
		t.Fatalf("bob fetch: %v", err)
	}
	if _, ok := leaked[chunkID]; ok {
		t.Fatal("LEAK: bob read content of a chunk in a space he can't access")
	}
}

// TestPublicSpace_SoftDemotedBelowMember proves the ranking penalty: when a user
// is a member of one space and another (public) space matches the SAME query just
// as well, the member page ranks first — public content can't dilute a near-tie.
func TestPublicSpace_SoftDemotedBelowMember(t *testing.T) {
	d := testdb.New(t)
	ctx := context.Background()
	owner := newUser(t, d, "owner")   // owns the public space, but...
	reader := newUser(t, d, "reader") // ...is a member of `mine` only, not `pub`
	pub := newSpace(t, d, "pub", owner)
	if _, err := d.Exec(`UPDATE spaces SET visibility='public' WHERE id=$1`, pub); err != nil {
		t.Fatalf("publish: %v", err)
	}
	mine := newSpace(t, d, "mine", reader)

	// Near-identical bodies so relevance is a wash and the penalty is the decider.
	pubPage := newPage(t, d, pub, "Kafka (public)", "## A\nkafka kafka kafka broker topic")
	minePage := newPage(t, d, mine, "Kafka (mine)", "## A\nkafka kafka kafka broker topic")
	svc := NewServiceWithEmbedder(d, &fakeEmbedder{})
	for _, p := range []int64{pubPage, minePage} {
		if _, err := svc.ReindexPage(ctx, p); err != nil {
			t.Fatalf("index: %v", err)
		}
	}

	hits, err := svc.Search(ctx, reader, "kafka", nil, 20, "hybrid")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var minePos, pubPos = -1, -1
	for i, h := range hits {
		if h.PageID == minePage {
			minePos = i
		}
		if h.PageID == pubPage {
			pubPos = i
		}
	}
	if minePos < 0 || pubPos < 0 {
		t.Fatalf("expected both pages in results, got mine=%d pub=%d", minePos, pubPos)
	}
	if minePos > pubPos {
		t.Errorf("member page should outrank the public near-tie: mine at %d, public at %d", minePos, pubPos)
	}
}

// TestPublicSpace_RetrievableByNonMember proves a published (public) space joins
// the ask/search corpus for a user who is NOT a member — the tela-Docs case where
// any signed-in user can ask the product docs — while a private space stays
// invisible even when its content matches the query (the anti-leak invariant).
func TestPublicSpace_RetrievableByNonMember(t *testing.T) {
	d := testdb.New(t)
	ctx := context.Background()
	alice := newUser(t, d, "alice")
	stranger := newUser(t, d, "stranger") // member of nothing

	pub := newSpace(t, d, "docs", alice)
	if _, err := d.Exec(`UPDATE spaces SET visibility='public' WHERE id=$1`, pub); err != nil {
		t.Fatalf("publish: %v", err)
	}
	priv := newSpace(t, d, "private", alice)

	pubPage := newPage(t, d, pub, "Comments", "## Adding\nUse the comment tool to attach a comment to any page.")
	privPage := newPage(t, d, priv, "Secret", "## A\nconfidential comment internals not for outsiders")

	svc := NewServiceWithEmbedder(d, &fakeEmbedder{})
	for _, p := range []int64{pubPage, privPage} {
		if _, err := svc.ReindexPage(ctx, p); err != nil {
			t.Fatalf("index: %v", err)
		}
	}

	// Search: the stranger finds the public page; the private page matches the
	// same term but is filtered out by access.
	hits, err := svc.Search(ctx, stranger, "comment", nil, 20, "hybrid")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var sawPub, sawPriv bool
	for _, h := range hits {
		switch h.PageID {
		case pubPage:
			sawPub = true
		case privPage:
			sawPriv = true
		}
	}
	if !sawPub {
		t.Error("stranger could not search a public space")
	}
	if sawPriv {
		t.Error("LEAK: stranger searched a private space")
	}

	// ReadChunk: the public chunk is readable, the private chunk is not.
	chunkOf := func(page int64) int64 {
		var id int64
		if err := d.QueryRow(`SELECT id FROM page_chunks WHERE page_id=$1 LIMIT 1`, page).Scan(&id); err != nil {
			t.Fatalf("chunk id for %d: %v", page, err)
		}
		return id
	}
	if _, err := svc.ReadChunk(ctx, stranger, chunkOf(pubPage), nil); err != nil {
		t.Errorf("stranger could not read a public chunk: %v", err)
	}
	if _, err := svc.ReadChunk(ctx, stranger, chunkOf(privPage), nil); !errors.Is(err, ErrChunkNotFound) {
		t.Errorf("expected ErrChunkNotFound for a private chunk, got %v", err)
	}
}

// TestHubPages_TitleHubRanksFirst proves the hub probe surfaces the page TITLED
// after the topic and ranks it by body density — the aggregate-question case
// ("which services use kafka") that AND-matching the whole question misses. The
// "Kafka" page is titled for the topic and mentions it in every chunk; the
// other page only shares a common word.
func TestHubPages_TitleHubRanksFirst(t *testing.T) {
	d := testdb.New(t)
	ctx := context.Background()
	alice := newUser(t, d, "alice")
	sp := newSpace(t, d, "alpha", alice)
	hub := newPage(t, d, sp, "Kafka",
		"## Brokers\nThe kafka broker cluster runs on three nodes and every service connects to kafka for its event stream and config bus over the kafka protocol here.\n\n"+
			"## Services Using Kafka\nThe following services consume from kafka: alpha, bravo, and charlie each subscribe to kafka topics and process kafka messages continuously.\n\n"+
			"## Topics\nKafka topics are organised by domain; each kafka topic has a producer and a consumer group reading from the kafka log in order to stay current.")
	other := newPage(t, d, sp, "Services Directory",
		"## List\nA plain directory of services and their owners and on-call rotation, with contact details and team names, containing no messaging or event infrastructure at all.")

	svc := NewServiceWithEmbedder(d, &fakeEmbedder{})
	for _, p := range []int64{hub, other} {
		if _, err := svc.ReindexPage(ctx, p); err != nil {
			t.Fatalf("index: %v", err)
		}
	}

	hubs, err := svc.HubPages(ctx, alice, "which services use kafka", nil, 8)
	if err != nil {
		t.Fatalf("HubPages: %v", err)
	}
	if len(hubs) == 0 || hubs[0].PageID != hub {
		t.Fatalf("expected the Kafka page as top hub, got %+v", hubs)
	}
	if hubs[0].Count < 2 {
		t.Errorf("kafka hub should match multiple chunks (OR over terms), got count=%d", hubs[0].Count)
	}
}

// TestHubPages_ScopedToAccess — the hub probe must honor space_access too.
func TestHubPages_ScopedToAccess(t *testing.T) {
	d := testdb.New(t)
	ctx := context.Background()
	alice := newUser(t, d, "alice")
	bob := newUser(t, d, "bob")
	sp := newSpace(t, d, "alpha", alice)
	page := newPage(t, d, sp, "Kafka", "## A\nkafka kafka kafka")
	svc := NewServiceWithEmbedder(d, &fakeEmbedder{})
	if _, err := svc.ReindexPage(ctx, page); err != nil {
		t.Fatalf("index: %v", err)
	}
	hubs, err := svc.HubPages(ctx, bob, "kafka", nil, 8)
	if err != nil {
		t.Fatalf("HubPages: %v", err)
	}
	if len(hubs) != 0 {
		t.Fatalf("LEAK: bob saw a hub page in a space he can't access: %+v", hubs)
	}
}
