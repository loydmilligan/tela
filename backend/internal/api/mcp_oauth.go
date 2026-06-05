package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/zcag/tela/backend/internal/auth"
)

// mcpOAuth is the OAuth 2.1 Resource-Server config for /api/mcp: it lets the
// endpoint accept WorkOS-issued JWTs (the Claude.ai / ChatGPT "Connect" flow) in
// addition to tela PATs, and serves the Protected Resource Metadata (RFC 9728)
// that bootstraps that flow.
//
// It is nil (OAuth disabled) unless TELA_WORKOS_ISSUER is set — when
// unconfigured the MCP endpoint stays PAT-only and advertises no OAuth, the same
// "no-op until configured" posture as the RAG embedder. So this whole layer is
// inert in prod until the WorkOS env vars land.
type mcpOAuth struct {
	issuer     string // AuthKit domain, e.g. https://x.authkit.app
	resource   string // the aud we enforce (this MCP endpoint's public URL)
	keyfunc    keyfunc.Keyfunc
	prmHandler http.Handler

	// apiKey (WORKOS_API_KEY) + completeURL back the 5b Standalone login bridge:
	// after tela authenticates the user, POST {external_auth_id, user} here to
	// finish the OAuth dance. completeURL is overridable in tests.
	apiKey      string
	completeURL string
}

const workosCompleteURL = "https://api.workos.com/authkit/oauth2/complete"

// loadMCPOAuth builds the OAuth config from env, or returns nil when
// TELA_WORKOS_ISSUER is unset (disabled). ctx bounds the JWKS refresh goroutine.
func loadMCPOAuth(ctx context.Context) *mcpOAuth {
	issuer := strings.TrimRight(os.Getenv("TELA_WORKOS_ISSUER"), "/")
	if issuer == "" {
		return nil
	}
	resource := strings.TrimSpace(os.Getenv("TELA_MCP_RESOURCE"))
	if resource == "" {
		resource = strings.TrimRight(os.Getenv("TELA_PUBLIC_BASE_URL"), "/") + "/api/mcp"
	}
	o, err := newMCPOAuth(ctx, issuer, resource, issuer+"/oauth2/jwks")
	if err != nil {
		log.Printf("mcp oauth: init failed (issuer=%s): %v — OAuth disabled, MCP stays PAT-only", issuer, err)
		return nil
	}
	o.apiKey = strings.TrimSpace(os.Getenv("WORKOS_API_KEY"))
	log.Printf("mcp oauth: enabled — issuer=%s resource=%s (login bridge %s)", issuer, resource,
		map[bool]string{true: "ready", false: "disabled: WORKOS_API_KEY unset"}[o.apiKey != ""])
	return o
}

// newMCPOAuth is the testable core: wires the JWKS keyfunc + the PRM handler for
// an explicit issuer/resource/jwks. Tests point jwksURL at an in-test JWK Set.
func newMCPOAuth(ctx context.Context, issuer, resource, jwksURL string) (*mcpOAuth, error) {
	kf, err := keyfunc.NewDefaultCtx(ctx, []string{jwksURL})
	if err != nil {
		return nil, err
	}
	prm := &oauthex.ProtectedResourceMetadata{
		Resource:               resource,
		AuthorizationServers:   []string{issuer},
		ScopesSupported:        []string{"openid", "email"},
		BearerMethodsSupported: []string{"header"},
	}
	return &mcpOAuth{
		issuer:      issuer,
		resource:    resource,
		keyfunc:     kf,
		prmHandler:  sdkauth.ProtectedResourceMetadataHandler(prm),
		completeURL: workosCompleteURL,
	}, nil
}

// resourceMetadataURL is the path-scoped PRM URL the 401 WWW-Authenticate points
// at: {origin}/.well-known/oauth-protected-resource{resource-path}.
func (o *mcpOAuth) resourceMetadataURL() string {
	u, err := url.Parse(o.resource)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host + wellKnownPRMPath + u.Path
}

// verifyJWT validates a WorkOS access token against the JWKS, enforcing issuer,
// audience (== resource; RFC 8707 replay defense), signing alg, and a small
// clock-skew leeway. Returns the claims on success.
func (o *mcpOAuth) verifyJWT(token string) (jwt.MapClaims, error) {
	claims := jwt.MapClaims{}
	tok, err := jwt.ParseWithClaims(token, claims, o.keyfunc.Keyfunc,
		jwt.WithIssuer(o.issuer),
		jwt.WithAudience(o.resource),
		jwt.WithValidMethods([]string{"RS256", "ES256"}),
		jwt.WithLeeway(60*time.Second),
	)
	if err != nil || !tok.Valid {
		return nil, err
	}
	return claims, nil
}

// verifyWorkOSToken resolves a validated WorkOS JWT to a tela identity. In the
// Standalone-bridge model the JWT's `sub` IS the tela users.id, so we map by sub
// — no email lookup, no mapping table. A synthetic *auth.APIKey makes a connector
// caller look like a write-scoped PAT user to the rest of the tool layer
// (mcpIdentity / mcpRequireWrite), so every tool keeps working unchanged.
// Connector tokens are deliberately never admin-scoped.
func (s *Server) verifyWorkOSToken(ctx context.Context, token string) (*sdkauth.TokenInfo, error) {
	claims, err := s.oauth.verifyJWT(token)
	if err != nil {
		return nil, sdkauth.ErrInvalidToken
	}
	sub, _ := claims["sub"].(string)
	uid, err := strconv.ParseInt(sub, 10, 64)
	if err != nil {
		return nil, sdkauth.ErrInvalidToken
	}
	u, err := auth.UserForAPIKey(ctx, s.DB, uid)
	if err != nil {
		return nil, sdkauth.ErrInvalidToken
	}
	exp := time.Now().Add(time.Hour)
	if e, ok := claims["exp"].(float64); ok {
		exp = time.Unix(int64(e), 0)
	}
	k := &auth.APIKey{UserID: uid, Scope: auth.ScopeWrite}
	return &sdkauth.TokenInfo{
		Scopes:     []string{k.Scope},
		UserID:     sub,
		Expiration: exp,
		Extra: map[string]any{
			tokenExtraUser:   u,
			tokenExtraAPIKey: k,
		},
	}, nil
}

const wellKnownPRMPath = "/.well-known/oauth-protected-resource"

// ServePRM serves the Protected Resource Metadata (RFC 9728), or 404 when OAuth
// is unconfigured. Registered at both the root well-known and the path-scoped
// variant (Claude probes both). Public + static (on auth.IsPublicPath).
func (s *Server) ServePRM(w http.ResponseWriter, r *http.Request) {
	if s.oauth == nil {
		http.NotFound(w, r)
		return
	}
	s.oauth.prmHandler.ServeHTTP(w, r)
}

// WorkOSLogin is the Standalone "OAuth Bridge" Login URI (5b): WorkOS sends the
// user here with ?external_auth_id=… during the Connect flow. We authenticate
// the user against tela's OWN session (bouncing through /login if they're not
// signed in), then call WorkOS's completion API which stamps the token's `sub`
// with the tela user id — so tela keeps owning identity, no user migration. On
// auth.IsPublicPath: it self-authenticates (unauthenticated → redirect to login,
// not 401).
func (s *Server) WorkOSLogin(w http.ResponseWriter, r *http.Request) {
	if s.oauth == nil {
		http.NotFound(w, r)
		return
	}
	externalAuthID := r.URL.Query().Get("external_auth_id")
	if externalAuthID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "external_auth_id is required")
		return
	}

	u := s.sessionUser(r)
	if u == nil {
		// Not signed in → bounce through tela's own login, returning here after
		// (the SPA hard-redirects to this backend path on success).
		ret := "/oauth/workos/login?external_auth_id=" + url.QueryEscape(externalAuthID)
		http.Redirect(w, r, "/login?next="+url.QueryEscape(ret), http.StatusFound)
		return
	}
	if u.Email == "" {
		http.Error(w, "your tela account has no email address; cannot connect via OAuth", http.StatusBadRequest)
		return
	}

	redirectURI, err := s.oauth.completeStandalone(r.Context(), externalAuthID, u)
	if err != nil {
		log.Printf("workos standalone complete failed (user=%d): %v", u.ID, err)
		http.Error(w, "could not complete authorization", http.StatusBadGateway)
		return
	}
	http.Redirect(w, r, redirectURI, http.StatusFound)
}

// sessionUser resolves the tela user from the session cookie, or nil when there
// is no valid session. Used by the public-path WorkOS bridge to self-authenticate.
func (s *Server) sessionUser(r *http.Request) *auth.User {
	c, err := r.Cookie(auth.CookieName)
	if err != nil || c.Value == "" {
		return nil
	}
	u, err := auth.LoadSessionAndSlide(r.Context(), s.DB, c.Value)
	if err != nil {
		return nil
	}
	return u
}

// completeStandalone finishes the WorkOS Standalone OAuth flow for an
// authenticated tela user: POST {external_auth_id, user{id,email}} to the
// completion API (Bearer WORKOS_API_KEY) and return the redirect_uri the browser
// must be sent to next. The user.id we send becomes the issued token's `sub`.
func (o *mcpOAuth) completeStandalone(ctx context.Context, externalAuthID string, u *auth.User) (string, error) {
	if o.apiKey == "" {
		return "", errors.New("WORKOS_API_KEY not set")
	}
	body, _ := json.Marshal(map[string]any{
		"external_auth_id": externalAuthID,
		"user": map[string]any{
			"id":    strconv.FormatInt(u.ID, 10),
			"email": u.Email,
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.completeURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+o.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return "", fmt.Errorf("workos complete: status %d: %s", resp.StatusCode, b)
	}
	var out struct {
		RedirectURI string `json:"redirect_uri"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.RedirectURI == "" {
		return "", errors.New("workos complete: empty redirect_uri")
	}
	return out.RedirectURI, nil
}
