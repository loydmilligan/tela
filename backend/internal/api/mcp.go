package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
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
	return sdkauth.RequireBearerToken(s.mcpVerifier, &sdkauth.RequireBearerTokenOptions{})(streamable)
}

// newMCPServer constructs the MCP server and registers the tool surface.
func (s *Server) newMCPServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: mcpServerName, Version: Version}, nil)
	s.registerMCPTools(server)
	return server
}

// mcpVerifier validates a bearer token as a tela PAT and resolves it to a
// TokenInfo carrying the tela *User + *APIKey. UserID is set so the SDK's
// Streamable-HTTP transport can pin a session to one user (anti-hijack).
func (s *Server) mcpVerifier(ctx context.Context, token string, _ *http.Request) (*sdkauth.TokenInfo, error) {
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
// per-request TokenInfo. Returns (nil, nil) if somehow unauthenticated — tools
// must treat that as an error rather than acting anonymously.
func mcpIdentity(req *mcp.CallToolRequest) (*auth.User, *auth.APIKey) {
	ex := req.GetExtra()
	if ex == nil || ex.TokenInfo == nil {
		return nil, nil
	}
	u, _ := ex.TokenInfo.Extra[tokenExtraUser].(*auth.User)
	k, _ := ex.TokenInfo.Extra[tokenExtraAPIKey].(*auth.APIKey)
	return u, k
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
