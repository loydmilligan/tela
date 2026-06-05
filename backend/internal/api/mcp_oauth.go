package api

import (
	"context"
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
}

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
	log.Printf("mcp oauth: enabled — issuer=%s resource=%s", issuer, resource)
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
		issuer:     issuer,
		resource:   resource,
		keyfunc:    kf,
		prmHandler: sdkauth.ProtectedResourceMetadataHandler(prm),
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
