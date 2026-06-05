package api

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const testJWTKID = "test-key-1"

// startJWKSServer serves a JWKS containing pub at /oauth2/jwks, mimicking a
// WorkOS AuthKit instance. The server URL is used as the issuer; the verifier
// fetches the key set at issuer + "/oauth2/jwks".
func startJWKSServer(t *testing.T, pub *rsa.PublicKey) *httptest.Server {
	t.Helper()
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes())
	jwks := fmt.Sprintf(`{"keys":[{"kty":"RSA","use":"sig","alg":"RS256","kid":%q,"n":%q,"e":%q}]}`, testJWTKID, n, e)
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/jwks", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(jwks))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// mintWorkOSJWT signs a WorkOS-style access token. aud=="" → the resource.
func mintWorkOSJWT(t *testing.T, priv *rsa.PrivateKey, issuer, resource, sub, aud string) string {
	t.Helper()
	if aud == "" {
		aud = resource
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":   issuer,
		"aud":   aud,
		"sub":   sub,
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(time.Hour).Unix(),
		"email": "x@example.com",
	})
	tok.Header["kid"] = testJWTKID
	s, err := tok.SignedString(priv)
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	return s
}

// TestMCP_OAuthDisabled_PRM404 — with no TELA_WORKOS_ISSUER, the PRM endpoint
// 404s (OAuth layer inert) and the MCP endpoint stays PAT-only.
func TestMCP_OAuthDisabled_PRM404(t *testing.T) {
	ts, _ := newWiredServer(t)
	for _, p := range []string{
		"/.well-known/oauth-protected-resource",
		"/.well-known/oauth-protected-resource/api/mcp",
	} {
		resp, err := http.Get(ts.URL + p)
		if err != nil {
			t.Fatalf("get %s: %v", p, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("disabled PRM %s: want 404, got %d", p, resp.StatusCode)
		}
	}
}

// TestMCP_OAuth_PRMAndJWTConnect — with OAuth configured (a fake AuthKit JWKS),
// the PRM advertises the resource + AS, the 401 carries WWW-Authenticate, a valid
// WorkOS JWT (sub = tela user id) connects and calls tools, and a wrong-audience
// token is rejected (RFC 8707 replay defense).
func TestMCP_OAuth_PRMAndJWTConnect(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	jwks := startJWKSServer(t, &priv.PublicKey)
	issuer := jwks.URL
	resource := "https://tela.test/api/mcp"
	t.Setenv("TELA_WORKOS_ISSUER", issuer)
	t.Setenv("TELA_MCP_RESOURCE", resource)

	ts, d := newWiredServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)
	seedSpace(t, d, "Docs", "docs", alice)

	// PRM advertises the resource + the WorkOS AS.
	resp, err := http.Get(ts.URL + "/.well-known/oauth-protected-resource/api/mcp")
	if err != nil {
		t.Fatal(err)
	}
	var prm struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&prm)
	resp.Body.Close()
	if prm.Resource != resource || len(prm.AuthorizationServers) != 1 || prm.AuthorizationServers[0] != issuer {
		t.Fatalf("PRM unexpected: %+v", prm)
	}

	// 401 without a token points at the PRM via WWW-Authenticate.
	r401, err := http.Get(ts.URL + "/api/mcp")
	if err != nil {
		t.Fatal(err)
	}
	r401.Body.Close()
	if r401.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", r401.StatusCode)
	}
	if wa := r401.Header.Get("WWW-Authenticate"); !strings.Contains(wa, "resource_metadata=") {
		t.Errorf("401 missing resource_metadata in WWW-Authenticate: %q", wa)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Valid WorkOS JWT (sub = alice) → connects + list_spaces returns her space.
	good := mintWorkOSJWT(t, priv, issuer, resource, strconv.FormatInt(alice, 10), "")
	sess := mcpSession(t, ctx, ts, good)
	var out listSpacesOut
	mcpCallJSON(t, ctx, sess, "list_spaces", map[string]any{}, &out)
	if len(out.Spaces) != 1 || out.Spaces[0].Slug != "docs" {
		t.Fatalf("list_spaces via WorkOS JWT: %+v", out.Spaces)
	}

	// Wrong-audience token → rejected at connect (replay defense).
	badAud := mintWorkOSJWT(t, priv, issuer, resource, strconv.FormatInt(alice, 10), "https://evil.example/api/mcp")
	transport := &mcp.StreamableClientTransport{
		Endpoint:   ts.URL + "/api/mcp",
		HTTPClient: &http.Client{Transport: bearerRoundTripper{token: badAud, base: http.DefaultTransport}},
	}
	cl := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	if sess2, err := cl.Connect(ctx, transport, nil); err == nil {
		sess2.Close()
		t.Fatalf("wrong-audience JWT should be rejected")
	}
}
