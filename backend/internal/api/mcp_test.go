package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zcag/tela/backend/internal/auth"
)

// bearerRoundTripper injects a tela PAT on every request the MCP client makes,
// so the Streamable-HTTP transport authenticates against /api/mcp.
type bearerRoundTripper struct {
	token string
	base  http.RoundTripper
}

func (b bearerRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(r)
}

// TestMCP_SpikeListSpaces is the Phase-0 spike: it drives the real MCP Go SDK
// client over Streamable HTTP against the wired backend, authenticates with a
// tela PAT, and asserts the list_spaces tool returns the caller's spaces as
// structured output. This proves transport + bearer-verifier + identity
// threading + typed output end-to-end.
func TestMCP_SpikeListSpaces(t *testing.T) {
	ts, d := newWiredServer(t)

	alice := seedUser(t, d, "alice", "alicepw12", false)
	bob := seedUser(t, d, "bob", "bobpw1234", false)
	seedSpace(t, d, "Alice Space", "alice-space", alice)
	seedSpace(t, d, "Bob Space", "bob-space", bob) // not alice's — must not leak

	// Mint an unrestricted read PAT for alice.
	raw, prefix, _, err := auth.NewAPIKey(auth.LoadAPIKeySecret())
	if err != nil {
		t.Fatalf("new api key: %v", err)
	}
	hmacHex := auth.HMACAPIKey(auth.LoadAPIKeySecret(), raw)
	if _, err := d.ExecContext(context.Background(), `
		INSERT INTO api_keys (user_id, name, key_prefix, key_hmac, scope, space_id)
		VALUES ($1, 'mcp', $2, $3, $4, NULL)`,
		alice, prefix, hmacHex, auth.ScopeRead); err != nil {
		t.Fatalf("seed key: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	transport := &mcp.StreamableClientTransport{
		Endpoint: ts.URL + "/api/mcp",
		HTTPClient: &http.Client{
			Transport: bearerRoundTripper{token: raw, base: http.DefaultTransport},
		},
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "spike-test", Version: "0"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	// tools/list advertises list_spaces with an output schema.
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	var found *mcp.Tool
	for _, tl := range tools.Tools {
		if tl.Name == "list_spaces" {
			found = tl
		}
	}
	if found == nil {
		t.Fatalf("list_spaces tool not advertised; got %d tools", len(tools.Tools))
	}
	if found.OutputSchema == nil {
		t.Errorf("list_spaces has no output schema")
	}

	// tools/call returns alice's space (and only hers) as structured output.
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_spaces"})
	if err != nil {
		t.Fatalf("call list_spaces: %v", err)
	}
	if res.IsError {
		t.Fatalf("list_spaces returned tool error: %v", res.Content)
	}

	var out listSpacesOut
	raw2, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw2, &out); err != nil {
		t.Fatalf("decode structured content %s: %v", raw2, err)
	}
	if len(out.Spaces) != 1 {
		t.Fatalf("want exactly alice's 1 space, got %d: %+v", len(out.Spaces), out.Spaces)
	}
	if out.Spaces[0].Name != "Alice Space" || out.Spaces[0].Slug != "alice-space" {
		t.Errorf("unexpected space: %+v", out.Spaces[0])
	}
	_ = bob
}

// TestMCP_SpikeRejectsNoToken asserts the transport refuses an unauthenticated
// connection (the bearer verifier 401s with WWW-Authenticate).
func TestMCP_SpikeRejectsNoToken(t *testing.T) {
	ts, _ := newWiredServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	transport := &mcp.StreamableClientTransport{Endpoint: ts.URL + "/api/mcp"}
	client := mcp.NewClient(&mcp.Implementation{Name: "spike-test", Version: "0"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err == nil {
		session.Close()
		t.Fatalf("expected connect to fail without a token")
	}
}
