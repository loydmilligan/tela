package api

import (
	"database/sql"
	"net/http"

	"github.com/zcag/tela/backend/internal/auth"
)

// Handler builds the canonical Tela HTTP handler: every route registered
// against a single mux, then wrapped with auth.Middleware. Used by both
// cmd/tela/main.go and the integration test suite so the two never drift.
//
// The Server's AuditWriter is threaded into Middleware so bearer-authed
// requests emit api_key_audit rows. Tests that need to assert on the audit
// table flush via srv.AuditWriter().Flush() before reading.
func Handler(d *sql.DB) http.Handler {
	srv := New(d)
	mux := http.NewServeMux()
	registerRoutes(srv, mux)
	return auth.Middleware(d, srv.auditWriter)(mux)
}

// HandlerWithServer is the wired-handler variant that also returns the
// underlying *Server. Used by integration tests so they can reach
// srv.AuditWriter().Flush() between issuing a bearer request and querying
// the api_key_audit table.
func HandlerWithServer(d *sql.DB) (http.Handler, *Server) {
	srv := New(d)
	mux := http.NewServeMux()
	registerRoutes(srv, mux)
	return auth.Middleware(d, srv.auditWriter)(mux), srv
}

func registerRoutes(srv *Server, mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", srv.Health)

	// M16.A.1.5 build-metadata probe. Public (see auth.IsPublicPath) — the MCP
	// server (M16.B.1) hits this at startup with no credentials to compat-check
	// itself against the running backend version. Values are injected via
	// ldflags at build time; defaults are version=dev / commit=unknown /
	// built_at=process-start.
	mux.HandleFunc("GET /api/version", srv.Version)

	mux.HandleFunc("POST /api/auth/login", srv.Login)
	mux.HandleFunc("POST /api/auth/logout", srv.Logout)
	mux.HandleFunc("GET /api/auth/me", srv.Me)
	mux.HandleFunc("POST /api/auth/register", srv.Register)
	mux.HandleFunc("POST /api/auth/verify-email", srv.VerifyEmail)
	mux.HandleFunc("POST /api/auth/resend-verification", srv.ResendVerification)
	mux.HandleFunc("POST /api/auth/request-password-reset", srv.RequestPasswordReset)
	mux.HandleFunc("POST /api/auth/reset-password", srv.ResetPassword)

	mux.HandleFunc("GET /api/spaces", srv.ListSpaces)
	mux.HandleFunc("POST /api/spaces", srv.CreateSpace)
	mux.HandleFunc("GET /api/spaces/{id}", srv.GetSpace)
	mux.HandleFunc("PATCH /api/spaces/{id}", srv.UpdateSpace)
	mux.HandleFunc("DELETE /api/spaces/{id}", srv.DeleteSpace)
	mux.HandleFunc("GET /api/spaces/{id}/index-version", srv.GetSpaceIndexVersion)
	mux.HandleFunc("POST /api/spaces/{id}/import", srv.ImportSpace)
	// M18.A.3 mira import: wraps the miraimport converter behind the same
	// editor+ membership gate as /import. URL-fetch path has its own
	// allowlist + caps (see import_mira.go); payload path enforces the 1 MiB
	// request-body cap via http.MaxBytesReader.
	mux.HandleFunc("POST /api/spaces/{id}/import-mira", srv.ImportMira)

	mux.HandleFunc("GET /api/pages", srv.ListPages)
	mux.HandleFunc("GET /api/pages/all", srv.ListAllPages)
	mux.HandleFunc("GET /api/pages/bodies", srv.ListPageBodies)
	mux.HandleFunc("POST /api/pages", srv.CreatePage)
	mux.HandleFunc("GET /api/pages/{id}", srv.GetPage)
	mux.HandleFunc("PATCH /api/pages/{id}", srv.UpdatePage)
	mux.HandleFunc("DELETE /api/pages/{id}", srv.DeletePage)
	mux.HandleFunc("POST /api/pages/{id}/move", srv.MovePage)
	mux.HandleFunc("GET /api/pages/{id}/backlinks", srv.Backlinks)
	// Persisted Yjs state for instant editor paint (member-gated). The editor
	// applies this on mount so content shows without waiting for the WS sync.
	mux.HandleFunc("GET /api/pages/{id}/yjs", srv.GetPageYjsState)

	// #3 PDF export. Session-authed: renders the caller's own page to PDF via
	// gotenberg (which loads the /print/{token} reader). The public token-data
	// endpoint below is what gotenberg's headless Chromium fetches; it MUST be
	// on auth.IsPublicPath (/api/print/) so the session middleware skips it. The
	// page id lives inside the signed token, not the path, which keeps the
	// public branch a clean HasPrefix.
	mux.HandleFunc("GET /api/pages/{id}/pdf", srv.ExportPagePDF)
	mux.HandleFunc("GET /api/print/{token}", srv.GetPrintPage)

	mux.HandleFunc("GET /api/pages/{id}/comments", srv.ListComments)
	mux.HandleFunc("POST /api/pages/{id}/comments", srv.CreateComment)
	mux.HandleFunc("PATCH /api/comments/{id}", srv.PatchComment)
	mux.HandleFunc("DELETE /api/comments/{id}", srv.DeleteComment)

	mux.HandleFunc("GET /api/pages/{id}/revisions", srv.ListPageRevisions)
	mux.HandleFunc("GET /api/pages/{id}/revisions/{rev_id}", srv.GetPageRevision)

	// M13.2 RichView Excalidraw PNG sidecar (HYBRID storage). PUT is
	// session-authed editor+ on the page's space; the GET counterpart below
	// is on /api/diagrams/* (public) so it can be served without a session.
	mux.HandleFunc("PUT /api/pages/{id}/diagrams", srv.UploadPageDiagram)

	// M15.0 PublicShare management: session-authed, editor+ on source page's
	// space. Soft-delete via revoked_at so the audit trail survives revocation.
	mux.HandleFunc("POST /api/pages/{id}/shares", srv.CreateShareLink)
	mux.HandleFunc("GET /api/pages/{id}/shares", srv.ListShareLinks)
	// Cross-space audit: every active share link the caller can see, in one
	// place. Distinct pattern from the {share_id} routes below (no wildcard).
	mux.HandleFunc("GET /api/shares", srv.ListAllShares)
	mux.HandleFunc("PATCH /api/shares/{share_id}", srv.PatchShareLink)
	mux.HandleFunc("DELETE /api/shares/{share_id}", srv.DeleteShareLink)

	// M15.0 PublicShare public token API: no session cookie required. MUST be
	// on auth.IsPublicPath (/api/share/) so the session middleware skips it.
	// Password-gated when the share has a password_hash — handlers validate the
	// per-share HMAC cookie themselves.
	mux.HandleFunc("GET /api/share/{token}", srv.GetPublicShare)
	mux.HandleFunc("POST /api/share/{token}/auth", srv.PublicShareAuth)
	mux.HandleFunc("GET /api/share/{token}/page/{page_id}", srv.GetPublicSharePage)
	mux.HandleFunc("GET /api/share/{token}/tree", srv.GetPublicShareTree)
	// #3 ".pdf on a share URL" trick. Caddy rewrites /share/<tok>.pdf →
	// /api/share/{token}/pdf (and the descendant /p/<id>.pdf → ?p=<id>). Public
	// via the /api/share/ prefix; the handler validates the share token + scope.
	mux.HandleFunc("GET /api/share/{token}/pdf", srv.ExportSharePDF)

	// M17.A.1 Feedback submit-only channel. Session OR bearer (any scope —
	// the bearer carve-out lives in auth.scopeAllowsRequest so the MCP
	// `submit_feedback` tool can use a read-scope key). Write-only in v0: no
	// list/get/patch/delete surface.
	mux.HandleFunc("POST /api/feedback", srv.CreateFeedback)

	mux.HandleFunc("GET /api/search", srv.Search)

	// M16.A.5 server-side body-fuzzy search. Powers the MCP `search_bodies`
	// tool so stdio agents don't have to spin up an Orama runtime on every
	// invocation. Session OR bearer-`read`; member of space_id required.
	mux.HandleFunc("GET /api/search/bodies", srv.SearchBodies)

	mux.HandleFunc("GET /api/admin/users", srv.ListAdminUsers)
	mux.HandleFunc("POST /api/admin/users", srv.CreateAdminUser)
	mux.HandleFunc("PATCH /api/admin/users/{id}", srv.PatchAdminUser)
	mux.HandleFunc("DELETE /api/admin/users/{id}", srv.DeleteAdminUser)

	mux.HandleFunc("POST /api/users/me/password", srv.ChangePassword)
	mux.HandleFunc("GET /api/users/me/sessions", srv.ListMySessions)
	mux.HandleFunc("DELETE /api/users/me/sessions", srv.DeleteAllMySessionsExceptCurrent)
	mux.HandleFunc("DELETE /api/users/me/sessions/{id}", srv.DeleteMySession)

	// M16.A.1 API keys: bearer-token management. Instance-admin only via the
	// session cookie path, OR a bearer key with admin scope. Keys are issued
	// once on POST and never re-exposed — list/delete operate over key_prefix
	// + opaque id, not the raw token. Owner of a key can DELETE their own
	// (soft-revoke).
	mux.HandleFunc("POST /api/api_keys", srv.CreateAPIKey)
	mux.HandleFunc("GET /api/api_keys", srv.ListAPIKeys)
	mux.HandleFunc("DELETE /api/api_keys/{id}", srv.DeleteAPIKey)

	// M16.A.2 API-key audit log read. Owner-of-key OR instance-admin via the
	// cookie session; bearer-mode callers need admin scope (a read/write key
	// reading its own audit trail would let a stolen token enumerate the
	// surface used to detect it). Writes happen asynchronously in
	// auth.Middleware on the bearer-auth path.
	mux.HandleFunc("GET /api/api_keys/{id}/audit", srv.ListAPIKeyAudit)

	mux.HandleFunc("GET /api/spaces/{id}/members", srv.ListSpaceMembers)
	mux.HandleFunc("POST /api/spaces/{id}/members", srv.AddSpaceMember)
	mux.HandleFunc("PATCH /api/spaces/{id}/members/{user_id}", srv.PatchSpaceMember)
	mux.HandleFunc("DELETE /api/spaces/{id}/members/{user_id}", srv.DeleteSpaceMember)

	// M7.1 LiveCollab: ws upgrade for Yjs relay. Authed via auth.Middleware
	// on the upgrade request — must NOT be added to auth.IsPublicPath.
	mux.HandleFunc("GET /ws/pages/{id}", srv.WSPage)

	// M13.2 RichView PNG sidecar GET: public, content-addressed by scene_hash.
	// MUST be on auth.IsPublicPath via the /api/diagrams/ HasPrefix branch.
	// Lives outside /api/pages/* so the session-gated PageView prefix doesn't
	// need regex special-casing to carve out a public hole. The .png suffix
	// lives inside the {file} path value (Go 1.22 mux wildcards must end a
	// segment); the handler strips it before validating against the hex regex.
	mux.HandleFunc("GET /api/diagrams/{page_id}/{file}", srv.ServePageDiagramPNG)

	// M11.0 OG share: public unauthenticated route. Crawler UAs get OG HTML;
	// real browsers get 302'd to the SPA. MUST be on auth.IsPublicPath.
	mux.HandleFunc("GET /p/{id}", srv.HandlePublicShare)
	// M11.1 OG image: public, not UA-gated (image fetchers carry arbitrary
	// UAs). Registered before /p/{id}/{slug} so the more-specific literal
	// pattern wins regardless of mux iteration order.
	mux.HandleFunc("GET /p/{id}/og.png", srv.HandleOGImage)
	mux.HandleFunc("GET /p/{id}/{slug}", srv.HandlePublicShare)

	// M15.5 PublicShare OG bot-gate: Caddy's /share/* block routes bot UAs
	// here so Slack / Twitter / Discord unfurl shared pages with real OG
	// metadata; real browsers fall through to the frontend SPA (M15.1). MUST
	// be on auth.IsPublicPath. Defense-in-depth UA check inside the handler
	// means a misconfigured Caddy block 404s real browsers instead of
	// serving the OG envelope in place of the SPA.
	mux.HandleFunc("GET /share/{token}", srv.HandlePublicShareLink)
	// Cosmetic-slug variant (/share/{token}/{slug}) — the slug is ignored; the
	// token is canonical. Distinct from the 4-segment descendant pattern below.
	mux.HandleFunc("GET /share/{token}/{slug}", srv.HandlePublicShareLink)
	mux.HandleFunc("GET /share/{token}/p/{page_id}", srv.HandlePublicShareLinkPage)
}
