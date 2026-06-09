package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zcag/tela/backend/internal/auth"
)

func TestAdminDomainLoginToken(t *testing.T) {
	s := &Server{shareSecret: []byte("unit-test-share-secret-aaaaaaaaaa")}

	tok := s.mintAdminDomainLoginToken(7, 2, "tela.ngss.io")
	uid, oid, host, ok := s.verifyAdminDomainLoginToken(tok)
	if !ok || uid != 7 || oid != 2 || host != "tela.ngss.io" {
		t.Fatalf("verify = (%d,%d,%q,%v), want (7,2,tela.ngss.io,true)", uid, oid, host, ok)
	}

	if _, _, _, ok := s.verifyAdminDomainLoginToken(tok + "x"); ok {
		t.Error("tampered token accepted")
	}
	other := &Server{shareSecret: []byte("a-completely-different-secret-key")}
	if _, _, _, ok := other.verifyAdminDomainLoginToken(tok); ok {
		t.Error("token verified under a different secret")
	}
	for _, bad := range []string{"", "nope", "a.b", "...."} {
		if _, _, _, ok := s.verifyAdminDomainLoginToken(bad); ok {
			t.Errorf("garbage %q accepted", bad)
		}
	}
}

func TestAdminDomainLoginMint(t *testing.T) {
	d := newAPITestDB(t)
	s := &Server{DB: d, shareSecret: []byte("unit-test-share-secret-aaaaaaaaaa")}
	orgID := seedOrg(t, d, "NGSS", "ngss")
	if _, err := d.Exec(
		`INSERT INTO org_hostnames (hostname, org_id, status, verify_token) VALUES ($1,$2,'active',$3)`,
		"tela.ngss.io", orgID, "vt"); err != nil {
		t.Fatal(err)
	}
	admin := &auth.User{ID: seedUser(t, d, "root", "password1", true), IsInstanceAdmin: true}
	plain := &auth.User{ID: seedUser(t, d, "joe", "password1", false)}

	pat := "/api/orgs/{id}/hostnames/{hostname}/admin-login"

	// Non-admin → 403.
	req := userRequest(http.MethodPost, "/api/orgs/1/hostnames/tela.ngss.io/admin-login", "", plain)
	if rec := routedRecorder(pat, s.AdminDomainLoginMint, req); rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin mint: got %d, want 403", rec.Code)
	}

	// Admin + active hostname → 200 with a redeem URL that verifies.
	req = userRequest(http.MethodPost, "/api/orgs/1/hostnames/tela.ngss.io/admin-login", "", admin)
	rec := routedRecorder(pat, s.AdminDomainLoginMint, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin mint: got %d, want 200 (%s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "https://tela.ngss.io/api/auth/admin-login/redeem?t=") {
		t.Fatalf("mint body missing redeem url: %s", rec.Body)
	}

	// Admin + hostname not owned by this org → 404.
	req = userRequest(http.MethodPost, "/api/orgs/999/hostnames/tela.ngss.io/admin-login", "", admin)
	if rec := routedRecorder(pat, s.AdminDomainLoginMint, req); rec.Code != http.StatusNotFound {
		t.Fatalf("wrong-org mint: got %d, want 404", rec.Code)
	}
}

func TestAdminDomainLoginRedeem(t *testing.T) {
	d := newAPITestDB(t)
	s := &Server{DB: d, shareSecret: []byte("unit-test-share-secret-aaaaaaaaaa")}
	orgID := seedOrg(t, d, "NGSS", "ngss")
	uid := seedUser(t, d, "root", "password1", true)
	tok := s.mintAdminDomainLoginToken(uid, orgID, "tela.ngss.io")

	redeem := func(host string, withOrg bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "https://"+host+"/api/auth/admin-login/redeem?t="+tok, nil)
		if withOrg {
			req = req.WithContext(auth.WithOrgContext(req.Context(), auth.OrgContext{OrgID: orgID, Host: host}))
		}
		return recordHandler(s.AdminDomainLoginRedeem, req)
	}

	// Happy path: right host + matching org context → 302 to / + session cookie.
	rec := redeem("tela.ngss.io", true)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
		t.Fatalf("redeem: got %d %q, want 302 /", rec.Code, rec.Header().Get("Location"))
	}
	var sid string
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName {
			sid = c.Value
		}
	}
	if sid == "" {
		t.Fatal("redeem set no session cookie")
	}
	// The minted session is bound to the org (so it validates on that host).
	onHost := auth.WithOrgContext(context.Background(), auth.OrgContext{OrgID: orgID, Host: "tela.ngss.io"})
	if _, err := auth.LoadSessionAndSlide(onHost, d, sid); err != nil {
		t.Fatalf("minted session not valid on org host: %v", err)
	}

	// Wrong host → bounce to login, no session.
	if rec := redeem("evil.example.com", true); rec.Header().Get("Location") != "/login?sso_error=admin_login" {
		t.Fatalf("wrong-host redeem: Location=%q", rec.Header().Get("Location"))
	}
	// Missing org context (canonical host) → bounce to login.
	if rec := redeem("tela.ngss.io", false); rec.Header().Get("Location") != "/login?sso_error=admin_login" {
		t.Fatalf("no-org redeem: Location=%q", rec.Header().Get("Location"))
	}
}
