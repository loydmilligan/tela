package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zcag/tela/backend/internal/auth"
)

// mcpServerName is the MCP server identity reported to clients on initialize.
const mcpServerName = "tela"

// TokenInfo.Extra keys under which the bearer verifier stashes the resolved
// tela identity so tool handlers can read it back per request via
// req.GetExtra().TokenInfo.Extra (the SDK threads TokenInfo from the request
// context into every tools/call — see streamable.go servePOST).
const (
	tokenExtraUser   = "tela.user"
	tokenExtraAPIKey = "tela.apiKey"
)

// MCPHandler builds the Streamable-HTTP handler mounted at /api/mcp. It is
// self-authenticated via the SDK's bearer middleware over tela PATs (the route
// is on auth.IsPublicPath so tela's own Middleware skips it). One shared server
// backs all sessions; identity is per-request, carried in TokenInfo.
func (s *Server) MCPHandler() http.Handler {
	server := s.newMCPServer()
	streamable := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server }, nil)
	opts := &sdkauth.RequireBearerTokenOptions{}
	if s.oauth != nil {
		// On 401, point clients at our Protected Resource Metadata so the OAuth
		// "Connect" flow can bootstrap. (No Scopes here — that gate applies to
		// ALL tokens incl. PATs, which don't carry openid/email.)
		opts.ResourceMetadataURL = s.oauth.resourceMetadataURL()
	}
	authed := sdkauth.RequireBearerToken(s.mcpVerifier, opts)(streamable)
	// Cap the request body: the SDK's streamable handler imposes no limit and the
	// endpoint is mounted public, so without this a client could POST a multi-GB
	// JSON-RPC body (memory-exhaustion DoS) — and it re-asserts import_mira's size
	// intent on the inline-payload path. SSE GET streams carry no request body, so
	// this only bounds the POST bodies.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, mcpMaxRequestBytes)
		authed.ServeHTTP(w, r)
	})
}

// mcpMaxRequestBytes bounds a single MCP HTTP request body. Generous enough for
// a batched JSON-RPC call + the 1 MiB import_mira payload, with headroom.
const mcpMaxRequestBytes = 4 << 20 // 4 MiB

// newMCPServer constructs the MCP server and registers the tool + resource
// surface. Capabilities (tools / resources) are inferred by the SDK from what's
// registered. The Implementation carries display branding (title, website, icon)
// for the host's connector card — website/icon are derived from the public base
// URL so self-hosters get their own.
func (s *Server) newMCPServer() *mcp.Server {
	impl := &mcp.Implementation{Name: mcpServerName, Title: "Tela", Version: Version}
	if base := publicBaseURL(); base != "" {
		impl.WebsiteURL = base
		impl.Icons = []mcp.Icon{{Source: base + "/favicon.svg", MIMEType: "image/svg+xml", Sizes: []string{"any"}}}
	}
	server := mcp.NewServer(impl, nil)
	s.registerMCPTools(server)
	s.registerMCPResources(server)
	s.registerMCPWidgets(server)
	return server
}

// mcpVerifier is the dual-mode bearer verifier for /api/mcp: a `tela_pat_*`
// token takes the PAT path; anything else is tried as a WorkOS OAuth JWT (only
// when OAuth is configured). Both resolve to a TokenInfo carrying the tela
// *User + *APIKey, so the tool layer is identical for either credential.
func (s *Server) mcpVerifier(ctx context.Context, token string, _ *http.Request) (*sdkauth.TokenInfo, error) {
	if strings.HasPrefix(token, auth.TokenPrefix) {
		return s.verifyPAT(ctx, token)
	}
	if s.oauth != nil {
		return s.verifyWorkOSToken(ctx, token)
	}
	return nil, sdkauth.ErrInvalidToken
}

// verifyPAT resolves a tela PAT to a TokenInfo. UserID is set so the SDK's
// Streamable-HTTP transport can pin a session to one user (anti-hijack).
func (s *Server) verifyPAT(ctx context.Context, token string) (*sdkauth.TokenInfo, error) {
	secret := auth.LoadAPIKeySecret()
	k, err := auth.LookupAPIKey(ctx, s.DB, secret, token)
	if err != nil {
		return nil, sdkauth.ErrInvalidToken
	}
	u, err := auth.UserForAPIKey(ctx, s.DB, k.UserID)
	if err != nil {
		return nil, sdkauth.ErrInvalidToken
	}
	return &sdkauth.TokenInfo{
		Scopes: []string{k.Scope},
		UserID: strconv.FormatInt(u.ID, 10),
		// The PAT's real expiry is enforced inside LookupAPIKey; this is the
		// SDK's required liveness window. The verifier re-runs on every request
		// (RequireBearerToken is per-request middleware), so it re-mints each
		// time and never spuriously expires a live PAT mid-session.
		Expiration: time.Now().Add(time.Hour),
		Extra: map[string]any{
			tokenExtraUser:   u,
			tokenExtraAPIKey: k,
		},
	}, nil
}

// mcpIdentity pulls the tela identity that mcpVerifier stashed into the
// per-request TokenInfo. Accepts any server request (tool call, resource read,
// prompt get, …) via the GetExtra() method they all share. Returns (nil, nil)
// if somehow unauthenticated — callers must treat that as an error rather than
// acting anonymously.
func mcpIdentity(req interface{ GetExtra() *mcp.RequestExtra }) (*auth.User, *auth.APIKey) {
	ex := req.GetExtra()
	if ex == nil || ex.TokenInfo == nil {
		return nil, nil
	}
	u, _ := ex.TokenInfo.Extra[tokenExtraUser].(*auth.User)
	k, _ := ex.TokenInfo.Extra[tokenExtraAPIKey].(*auth.APIKey)
	return u, k
}

// mcpRequireWrite enforces, at the tool boundary, the write/admin-scope gate
// that the HTTP method-scope middleware applies to mutating REST routes. The
// MCP transport is mounted public (single POST endpoint carries read + write
// tools), so the per-tool scope check moves here: a read-scope key calling a
// write tool gets the same api_key_scope 403 it would get from the middleware.
// Returns nil for write/admin keys (and for nil/session callers — not reachable
// over MCP, but safe).
func mcpRequireWrite(k *auth.APIKey) *apiErr {
	if k == nil || k.Scope != auth.ScopeRead {
		return nil
	}
	return &apiErr{http.StatusForbidden, "api_key_scope", "api key scope does not permit this method"}
}

// mcpErr maps a core *apiErr to a tool-error CallToolResult. The text payload
// is the same {error, code, status} envelope the TS client surfaced, so agents
// keyed on `code` keep working.
func mcpErr(ae *apiErr) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{
			Text: fmt.Sprintf(`{"error":%q,"code":%q,"status":%d}`, ae.Message, ae.Code, ae.Status),
		}},
	}
}

// mcpUnauthErr is returned when a tool runs without a resolved identity.
func mcpUnauthErr() *mcp.CallToolResult {
	return mcpErr(&apiErr{http.StatusUnauthorized, "unauthorized", "missing authenticated identity"})
}
