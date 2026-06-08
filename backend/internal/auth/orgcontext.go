package auth

import "context"

// OrgContext is the org a request is scoped to by virtue of the host it arrived
// on. It is set ONLY when the request came in on an org's active custom domain
// (resolved by the host→org middleware, which runs before Middleware). On the
// canonical host it is absent — the app is unscoped (all the user's orgs).
//
// It lives in the auth package, not api, because two auth-layer concerns read
// it: CreateSession stamps the bound org from it, and Middleware enforces the
// session↔org binding against it. The api layer reads it too (originFor,
// org-scoped space listing).
type OrgContext struct {
	OrgID int64  // the org that owns Host
	Host  string // the active custom hostname the request arrived on (lowercased, no port)
}

const orgCtxKey contextKey = 3

// WithOrgContext returns a context carrying oc. Called by the host→org
// middleware after it resolves an active custom hostname.
func WithOrgContext(ctx context.Context, oc OrgContext) context.Context {
	return context.WithValue(ctx, orgCtxKey, oc)
}

// OrgContextFromContext pulls the host-derived org context out of ctx. The
// second return is false on the canonical host (no custom-domain scope).
func OrgContextFromContext(ctx context.Context) (OrgContext, bool) {
	oc, ok := ctx.Value(orgCtxKey).(OrgContext)
	return oc, ok
}
