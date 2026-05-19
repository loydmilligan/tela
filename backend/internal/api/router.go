package api

import (
	"database/sql"
	"net/http"

	"github.com/zcag/tela/backend/internal/auth"
)

// Handler builds the canonical Tela HTTP handler: every route registered
// against a single mux, then wrapped with auth.Middleware. Used by both
// cmd/tela/main.go and the integration test suite so the two never drift.
func Handler(d *sql.DB) http.Handler {
	srv := New(d)
	mux := http.NewServeMux()
	registerRoutes(srv, mux)
	return auth.Middleware(d)(mux)
}

func registerRoutes(srv *Server, mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", srv.Health)

	mux.HandleFunc("POST /api/auth/login", srv.Login)
	mux.HandleFunc("POST /api/auth/logout", srv.Logout)
	mux.HandleFunc("GET /api/auth/me", srv.Me)

	mux.HandleFunc("GET /api/spaces", srv.ListSpaces)
	mux.HandleFunc("POST /api/spaces", srv.CreateSpace)
	mux.HandleFunc("GET /api/spaces/{id}", srv.GetSpace)
	mux.HandleFunc("PATCH /api/spaces/{id}", srv.UpdateSpace)
	mux.HandleFunc("DELETE /api/spaces/{id}", srv.DeleteSpace)
	mux.HandleFunc("GET /api/spaces/{id}/index-version", srv.GetSpaceIndexVersion)
	mux.HandleFunc("POST /api/spaces/{id}/import", srv.ImportSpace)

	mux.HandleFunc("GET /api/pages", srv.ListPages)
	mux.HandleFunc("GET /api/pages/all", srv.ListAllPages)
	mux.HandleFunc("GET /api/pages/bodies", srv.ListPageBodies)
	mux.HandleFunc("POST /api/pages", srv.CreatePage)
	mux.HandleFunc("GET /api/pages/{id}", srv.GetPage)
	mux.HandleFunc("PATCH /api/pages/{id}", srv.UpdatePage)
	mux.HandleFunc("DELETE /api/pages/{id}", srv.DeletePage)
	mux.HandleFunc("POST /api/pages/{id}/move", srv.MovePage)
	mux.HandleFunc("GET /api/pages/{id}/backlinks", srv.Backlinks)

	mux.HandleFunc("GET /api/pages/{id}/comments", srv.ListComments)
	mux.HandleFunc("POST /api/pages/{id}/comments", srv.CreateComment)
	mux.HandleFunc("PATCH /api/comments/{id}", srv.PatchComment)
	mux.HandleFunc("DELETE /api/comments/{id}", srv.DeleteComment)

	mux.HandleFunc("GET /api/pages/{id}/revisions", srv.ListPageRevisions)
	mux.HandleFunc("GET /api/pages/{id}/revisions/{rev_id}", srv.GetPageRevision)

	mux.HandleFunc("GET /api/search", srv.Search)

	mux.HandleFunc("GET /api/admin/users", srv.ListAdminUsers)
	mux.HandleFunc("POST /api/admin/users", srv.CreateAdminUser)
	mux.HandleFunc("PATCH /api/admin/users/{id}", srv.PatchAdminUser)
	mux.HandleFunc("DELETE /api/admin/users/{id}", srv.DeleteAdminUser)

	mux.HandleFunc("POST /api/users/me/password", srv.ChangePassword)
	mux.HandleFunc("GET /api/users/me/sessions", srv.ListMySessions)
	mux.HandleFunc("DELETE /api/users/me/sessions", srv.DeleteAllMySessionsExceptCurrent)
	mux.HandleFunc("DELETE /api/users/me/sessions/{id}", srv.DeleteMySession)

	mux.HandleFunc("GET /api/spaces/{id}/members", srv.ListSpaceMembers)
	mux.HandleFunc("POST /api/spaces/{id}/members", srv.AddSpaceMember)
	mux.HandleFunc("PATCH /api/spaces/{id}/members/{user_id}", srv.PatchSpaceMember)
	mux.HandleFunc("DELETE /api/spaces/{id}/members/{user_id}", srv.DeleteSpaceMember)

	// M7.1 LiveCollab: ws upgrade for Yjs relay. Authed via auth.Middleware
	// on the upgrade request — must NOT be added to auth.IsPublicPath.
	mux.HandleFunc("GET /ws/pages/{id}", srv.WSPage)

	// M11.0 OG share: public unauthenticated route. Crawler UAs get OG HTML;
	// real browsers get 302'd to the SPA. MUST be on auth.IsPublicPath.
	mux.HandleFunc("GET /p/{id}", srv.HandlePublicShare)
	mux.HandleFunc("GET /p/{id}/{slug}", srv.HandlePublicShare)
}
