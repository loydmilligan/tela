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

	"github.com/zcag/tela/backend/internal/auth"
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

// TestMCP_OAuthLoginBridge exercises the Phase-5b Standalone login bridge:
// missing external_auth_id → 400; no session → bounce through /login; an
// authenticated tela user with an email → completes via the (mocked) WorkOS API
// and redirects to the returned redirect_uri, having sent sub = tela user id.
func TestMCP_OAuthLoginBridge(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	jwks := startJWKSServer(t, &priv.PublicKey)
	t.Setenv("TELA_WORKOS_ISSUER", jwks.URL)
	t.Setenv("TELA_MCP_RESOURCE", "https://tela.test/api/mcp")
	t.Setenv("WORKOS_API_KEY", "sk_test_fake")
	t.Setenv("TELA_SHARE_SECRET", "tela-test-share-secret-fixed-32-byte!")

	// Mock the WorkOS completion API.
	var gotBody map[string]any
	complete := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk_test_fake" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"redirect_uri":"https://authkit.example/oauth2/consent?x=1"}`))
	}))
	t.Cleanup(complete.Close)

	d := newAPITestDB(t)
	handler, srv := HandlerWithServer(d)
	if srv.oauth == nil {
		t.Fatal("oauth not enabled despite TELA_WORKOS_ISSUER set")
	}
	srv.oauth.completeURL = complete.URL
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	noRedir := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	// Missing external_auth_id → 400.
	if r, err := noRedir.Get(ts.URL + "/oauth/workos/login"); err != nil {
		t.Fatal(err)
	} else if r.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing eid: want 400, got %d", r.StatusCode)
	}

	// No session → 302 to /login?next=<bridge>.
	r2, err := noRedir.Get(ts.URL + "/oauth/workos/login?external_auth_id=eid123")
	if err != nil {
		t.Fatal(err)
	}
	if r2.StatusCode != http.StatusFound {
		t.Fatalf("no session: want 302, got %d", r2.StatusCode)
	}
	if loc := r2.Header.Get("Location"); !strings.HasPrefix(loc, "/login?next=") || !strings.Contains(loc, "eid123") {
		t.Fatalf("no-session redirect Location: %q", loc)
	}

	// Authenticated user with an email → complete → 302 to the redirect_uri.
	var uid int64
	hash, _ := auth.HashPassword("pw12345678")
	if err := d.QueryRowContext(context.Background(),
		`INSERT INTO users (username, email, password_hash, is_instance_admin, is_active)
		 VALUES ('alice','alice@example.com',$1,0,1) RETURNING id`, hash).Scan(&uid); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	sid, err := auth.CreateSession(context.Background(), d, uid, "test")
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/oauth/workos/login?external_auth_id=eid456", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: sid})
	r3, err := noRedir.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if r3.StatusCode != http.StatusFound {
		t.Fatalf("authed: want 302, got %d", r3.StatusCode)
	}
	if loc := r3.Header.Get("Location"); loc != "https://authkit.example/oauth2/consent?x=1" {
		t.Fatalf("authed redirect Location: %q", loc)
	}
	usr, _ := gotBody["user"].(map[string]any)
	if usr["id"] != strconv.FormatInt(uid, 10) || usr["email"] != "alice@example.com" {
		t.Fatalf("complete body user: %+v", usr)
	}
	if gotBody["external_auth_id"] != "eid456" {
		t.Fatalf("complete external_auth_id: %v", gotBody["external_auth_id"])
	}
}
