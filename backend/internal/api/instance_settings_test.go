package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestInstanceSettings_PatchThenGet(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	adminID := seedUser(t, d, "admin", "adminpw123", true)

	// Secrets resolved by New() live under the secret/ prefix and must NOT
	// appear in the listing.
	req := userRequest(http.MethodGet, "/api/admin/settings", "", authUser(adminID, "admin", true))
	rec := recordHandler(srv.GetInstanceSettings, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: status=%d body=%q want 200", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret/") {
		t.Fatalf("listing leaked a secret key: %q", rec.Body.String())
	}

	patch := userRequest(http.MethodPatch, "/api/admin/settings",
		`{"settings":{"registration_open":"false"}}`, authUser(adminID, "admin", true))
	prec := recordHandler(srv.PatchInstanceSettings, patch)
	if prec.Code != http.StatusOK {
		t.Fatalf("patch: status=%d body=%q want 200", prec.Code, prec.Body.String())
	}
	if !strings.Contains(prec.Body.String(), `"registration_open":"false"`) {
		t.Fatalf("patch response missing setting: %q", prec.Body.String())
	}

	rec2 := recordHandler(srv.GetInstanceSettings,
		userRequest(http.MethodGet, "/api/admin/settings", "", authUser(adminID, "admin", true)))
	if !strings.Contains(rec2.Body.String(), `"registration_open":"false"`) {
		t.Fatalf("get after patch missing setting: %q", rec2.Body.String())
	}
}

func TestInstanceSettings_RejectsSecretWrite(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	adminID := seedUser(t, d, "admin", "adminpw123", true)

	patch := userRequest(http.MethodPatch, "/api/admin/settings",
		`{"settings":{"secret/api_key":"deadbeef"}}`, authUser(adminID, "admin", true))
	rec := recordHandler(srv.PatchInstanceSettings, patch)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("secret write: status=%d body=%q want 403", rec.Code, rec.Body.String())
	}
}

func TestInstanceSettings_NonAdminForbidden(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	uid := seedUser(t, d, "user", "userpw1234", false)

	rec := recordHandler(srv.GetInstanceSettings,
		userRequest(http.MethodGet, "/api/admin/settings", "", authUser(uid, "user", false)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin get: status=%d want 403", rec.Code)
	}
}
