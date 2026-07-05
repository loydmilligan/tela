package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// The machine-discovery manifest at /.well-known/mcp.json is public (no auth),
// serves JSON, and injects this instance's origin into the remote URL.
func TestMCP_WellKnownManifest(t *testing.T) {
	ts, _ := newWiredServer(t)

	resp, err := http.Get(ts.URL + "/.well-known/mcp.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("want json content-type, got %q", ct)
	}
	if acao := resp.Header.Get("Access-Control-Allow-Origin"); acao != "*" {
		t.Fatalf("want CORS *, got %q", acao)
	}

	var m struct {
		Name    string `json:"name"`
		Remotes []struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"remotes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	if m.Name != "io.github.zcag/tela" {
		t.Fatalf("unexpected name %q", m.Name)
	}
	if len(m.Remotes) != 1 || !strings.HasSuffix(m.Remotes[0].URL, "/api/mcp") {
		t.Fatalf("unexpected remotes %+v", m.Remotes)
	}
}
