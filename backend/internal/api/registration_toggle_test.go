package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// With the instance setting registration_open=false, self-registration is
// refused with 403 (admins can still create users directly).
func TestRegister_ClosedByInstanceSetting(t *testing.T) {
	t.Setenv("TELA_SHARE_SECRET", "tela-test-share-secret-fixed-32-byte!")
	d := newAPITestDB(t)
	handler, srv := HandlerWithServer(d)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	if err := srv.settings.Set(context.Background(), "registration_open", "false", nil); err != nil {
		t.Fatalf("set registration_open: %v", err)
	}
	resp, err := http.Post(ts.URL+"/api/auth/register", "application/json",
		strings.NewReader(`{"email":"a@b.com","username":"x","password":"hunter2hunter"}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("register status=%d want 403 (registration closed)", resp.StatusCode)
	}

	// Re-open → registration works again.
	if err := srv.settings.Set(context.Background(), "registration_open", "true", nil); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	resp2, err := http.Post(ts.URL+"/api/auth/register", "application/json",
		strings.NewReader(`{"email":"a@b.com","username":"x","password":"hunter2hunter"}`))
	if err != nil {
		t.Fatalf("post 2: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusCreated {
		t.Fatalf("reopened register status=%d want 201", resp2.StatusCode)
	}
}
