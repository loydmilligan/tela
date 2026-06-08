package api

import (
	"context"
	"net/http"
	"strings"
)

// Unified-handle guard. Users (users.username) and orgs (orgs.slug) share ONE
// public namespace (/{handle}), so a new handle in either namespace must:
//   1. not be a reserved word (a top-level app route would shadow it), and
//   2. not collide with an EXISTING handle in the OTHER namespace.
// Existing rows are grandfathered — the guard only runs on create (Register +
// org create), never retroactively.

// reservedHandles are top-level paths the SPA / API already own; a handle equal
// to one of these would be unreachable (the route shadows it). Kept lowercase;
// reservedHandle compares case-insensitively.
var reservedHandles = map[string]bool{
	"api": true, "login": true, "logout": true, "register": true,
	"settings": true, "spaces": true, "discover": true, "share": true,
	"shared": true, "p": true, "u": true, "dav": true, "oauth": true,
	"admin": true, "public": true, "search": true, "read": true,
	"print": true, "graph": true, "n": true, "assets": true,
	"well-known": true, "metrics": true, "sitemap": true, "favicon": true,
	"robots": true, "verify-email": true, "forgot-password": true,
	"reset-password": true,
}

// reservedHandle reports whether h is a reserved top-level word that cannot be
// used as a user/org handle.
func reservedHandle(h string) bool {
	return reservedHandles[strings.ToLower(strings.TrimSpace(h))]
}

// usernameTaken reports whether an existing user already owns this handle.
func usernameTaken(ctx context.Context, q queryer, handle string) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE LOWER(username) = LOWER($1)`, handle).Scan(&n)
	return n > 0, err
}

// orgSlugTaken reports whether an existing org already owns this handle.
func orgSlugTaken(ctx context.Context, q queryer, handle string) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM orgs WHERE LOWER(slug) = LOWER($1)`, handle).Scan(&n)
	return n > 0, err
}

// checkHandleAvailable validates a NEW handle against the reserved list and the
// OTHER namespace. otherTaken is the namespace-crossing collision probe (for a
// new username: orgSlugTaken; for a new org slug: usernameTaken). Returns an
// *apiErr (409/500) the caller writes, or nil when the handle is free. The same
// namespace's own uniqueness is left to the INSERT's UNIQUE constraint.
func checkHandleAvailable(ctx context.Context, handle string, otherTaken func(context.Context, queryer, string) (bool, error), q queryer) *apiErr {
	if reservedHandle(handle) {
		return &apiErr{http.StatusConflict, "handle_reserved", "that handle is reserved and cannot be used"}
	}
	taken, err := otherTaken(ctx, q, handle)
	if err != nil {
		return &apiErr{http.StatusInternalServerError, "internal", "handle availability check failed"}
	}
	if taken {
		return &apiErr{http.StatusConflict, "handle_taken", "that handle is already in use"}
	}
	return nil
}
