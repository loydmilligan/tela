package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/zcag/tela/backend/internal/auth"
	"github.com/zcag/tela/backend/internal/llm"
	"github.com/zcag/tela/backend/internal/rag"
)

// fakeCompleter returns a canned answer and records the last prompt it saw, so
// the /ask test can assert the retrieved chunk text was grounded into it.
type fakeCompleter struct {
	answer     string
	lastSystem string
	lastUser   string
}

func (f *fakeCompleter) Model() string { return "fake-llm" }
func (f *fakeCompleter) Complete(_ context.Context, system, user string) (string, error) {
	f.lastSystem, f.lastUser = system, user
	return f.answer, nil
}

// ── Managed LLM proxy (/api/cloud/llm/v1/chat/completions) ──────────────────

func TestCloudChat_GatedByEntitlement(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", cloudTestSecret)
	auth.ResetAPIKeySecretCache()
	ts, d, srv := newWiredServerOnDiskWithSrv(t)
	srv.llm = llm.NewServiceWithCompleter(&fakeCompleter{answer: "hi"})
	uid := seedUser(t, d, "freeuser", "freepw1234", false) // default personal_free, no ask_docs
	rawKey, _ := seedAPIKeyForUser(t, d, uid, auth.ScopeRead, nil)

	resp := bearerRequest(t, http.MethodPost, ts.URL+"/api/cloud/llm/v1/chat/completions", rawKey,
		`{"model":"x","messages":[{"role":"user","content":"hello"}]}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("free plan chat status=%d want 403", resp.StatusCode)
	}
}

func TestCloudChat_EntitledReturnsCompletion(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", cloudTestSecret)
	auth.ResetAPIKeySecretCache()
	ts, d, srv := newWiredServerOnDiskWithSrv(t)
	srv.llm = llm.NewServiceWithCompleter(&fakeCompleter{answer: "canned managed answer"})
	uid := seedUser(t, d, "plususer", "pluspw1234", false)
	if _, err := d.Exec(`UPDATE users SET plan_key='personal_plus' WHERE id=$1`, uid); err != nil {
		t.Fatalf("set plan: %v", err)
	}
	rawKey, _ := seedAPIKeyForUser(t, d, uid, auth.ScopeRead, nil)

	resp := bearerRequest(t, http.MethodPost, ts.URL+"/api/cloud/llm/v1/chat/completions", rawKey,
		`{"model":"x","messages":[{"role":"system","content":"sys"},{"role":"user","content":"hello"}]}`)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%q want 200", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "canned managed answer") || !strings.Contains(string(body), `"choices"`) {
		t.Fatalf("chat body not OpenAI-shaped / missing answer: %q", body)
	}
}

func TestCloudChat_RejectNoToken(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", cloudTestSecret)
	auth.ResetAPIKeySecretCache()
	ts, _, _ := newWiredServerOnDiskWithSrv(t)

	resp := bearerRequest(t, http.MethodPost, ts.URL+"/api/cloud/llm/v1/chat/completions", "tela_pat_bogus",
		`{"model":"x","messages":[{"role":"user","content":"hi"}]}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bogus token status=%d want 401", resp.StatusCode)
	}
}

// ── Ask your docs (/api/rag/ask) ────────────────────────────────────────────

func TestRAGAsk_DisabledReturns503(t *testing.T) {
	// rag enabled (fake embedder) but llm unconfigured → 503.
	ts, d, _ := newRagServer(t)
	_ = seedUser(t, d, "alice", "alicepw12", false)
	c := loginClient(t, ts, "alice", "alicepw12")
	resp, err := c.Post(ts.URL+"/api/rag/ask", "application/json", strings.NewReader(`{"question":"hi"}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status=%d want 503 when llm unconfigured", resp.StatusCode)
	}
}

func TestRAGAsk_GroundedAnswerWithSources(t *testing.T) {
	ts, d, srv := newRagServer(t)
	fake := &fakeCompleter{answer: "Run make deploy to ship a release."}
	srv.llm = llm.NewServiceWithCompleter(fake)

	alice := seedUser(t, d, "alice", "alicepw12", false)
	aSpace := seedSpace(t, d, "Alpha", "alpha", alice)
	page := mustPage(t, d, aSpace, "Deploy Guide", "## Shipping\nrun make deploy to push the release to production")
	if _, _, err := rag.NewServiceWithEmbedder(d, fakeEmb{}).ReindexSpace(context.Background(), aSpace); err != nil {
		t.Fatalf("index: %v", err)
	}

	c := loginClient(t, ts, "alice", "alicepw12")
	resp, err := c.Post(ts.URL+"/api/rag/ask", "application/json",
		strings.NewReader(`{"question":"how do I deploy a release","space_id":`+strconv.FormatInt(aSpace, 10)+`}`))
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%q want 200", resp.StatusCode, body)
	}
	var out struct {
		Answer  string    `json:"answer"`
		Sources []rag.Hit `json:"sources"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Answer != fake.answer {
		t.Fatalf("answer=%q want canned %q", out.Answer, fake.answer)
	}
	if len(out.Sources) == 0 || out.Sources[0].PageID != page {
		t.Fatalf("expected cited source page %d, got %+v", page, out.Sources)
	}
	// The retrieved chunk text must have been grounded into the prompt.
	if !strings.Contains(fake.lastUser, "make deploy") {
		t.Fatalf("retrieved chunk text not in prompt: %q", fake.lastUser)
	}
}
