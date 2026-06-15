package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// The ai.disabled kill-switch + maintenance banner flow through instance settings
// and surface on the public host-context.
func TestMaintenance_KillSwitchAndBanner(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	adminID := seedUser(t, d, "admin", "adminpw123", true)

	// No notice by default.
	hc0 := recordHandler(srv.HostContext, userRequest(http.MethodGet, "/api/host-context", "", authUser(adminID, "admin", true)))
	var out0 hostContextDTO
	_ = json.Unmarshal(hc0.Body.Bytes(), &out0)
	if out0.Maintenance != nil {
		t.Fatalf("maintenance should be nil by default, got %+v", out0.Maintenance)
	}

	patch := userRequest(http.MethodPatch, "/api/admin/settings",
		`{"settings":{"ai.disabled":"1","maintenance.notice":"AI is down for maintenance","maintenance.level":"warning"}}`,
		authUser(adminID, "admin", true))
	if rec := recordHandler(srv.PatchInstanceSettings, patch); rec.Code != http.StatusOK {
		t.Fatalf("patch: status=%d body=%q", rec.Code, rec.Body.String())
	}

	if srv.aiEnabled() {
		t.Fatalf("aiEnabled() must be false once ai.disabled=1")
	}

	hc := recordHandler(srv.HostContext, userRequest(http.MethodGet, "/api/host-context", "", authUser(adminID, "admin", true)))
	var out hostContextDTO
	if err := json.Unmarshal(hc.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode host-context: %v", err)
	}
	if out.AIAvailable {
		t.Fatalf("ai_available must be false when ai.disabled=1")
	}
	if out.Maintenance == nil || out.Maintenance.Notice != "AI is down for maintenance" || out.Maintenance.Level != "warning" {
		t.Fatalf("maintenance notice not surfaced: %+v", out.Maintenance)
	}
}

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
