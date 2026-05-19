package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestListAdminUsers_OkSortedByUsername(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	adminID := seedUser(t, d, "admin", "adminpw123", true)
	seedUser(t, d, "charlie", "charliepw", false)
	seedUser(t, d, "bob", "bobpw1234", false)

	req := userRequest(http.MethodGet, "/api/admin/users", "", authUser(adminID, "admin", true))
	rec := recordHandler(srv.ListAdminUsers, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q want 200", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	adminIdx := strings.Index(body, `"admin"`)
	bobIdx := strings.Index(body, `"bob"`)
	charlieIdx := strings.Index(body, `"charlie"`)
	if adminIdx < 0 || bobIdx < 0 || charlieIdx < 0 {
		t.Fatalf("missing user(s) in body=%q", body)
	}
	if !(adminIdx < bobIdx && bobIdx < charlieIdx) {
		t.Fatalf("ordering wrong: admin=%d bob=%d charlie=%d body=%q", adminIdx, bobIdx, charlieIdx, body)
	}
}

func TestListAdminUsers_NonAdmin_Forbidden(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	uid := seedUser(t, d, "user", "userpw1234", false)

	req := userRequest(http.MethodGet, "/api/admin/users", "", authUser(uid, "user", false))
	rec := recordHandler(srv.ListAdminUsers, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d want 403 body=%q", rec.Code, rec.Body.String())
	}
}

func TestCreateAdminUser_OkAndDuplicateConflict(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	adminID := seedUser(t, d, "admin", "adminpw123", true)

	req := userRequest(http.MethodPost, "/api/admin/users",
		`{"username":"alice","password":"alicepw123"}`,
		authUser(adminID, "admin", true))
	rec := recordHandler(srv.CreateAdminUser, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("first create: status=%d body=%q want 201", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"username":"alice"`) {
		t.Fatalf("first create: body missing alice: %q", rec.Body.String())
	}

	req2 := userRequest(http.MethodPost, "/api/admin/users",
		`{"username":"alice","password":"differentpw"}`,
		authUser(adminID, "admin", true))
	rec2 := recordHandler(srv.CreateAdminUser, req2)
	if rec2.Code != http.StatusConflict {
		t.Fatalf("duplicate: status=%d body=%q want 409", rec2.Code, rec2.Body.String())
	}
	if !strings.Contains(rec2.Body.String(), `"code":"conflict"`) {
		t.Fatalf("duplicate: missing code=conflict body=%q", rec2.Body.String())
	}
}

func TestCreateAdminUser_RejectsShortPassword(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	adminID := seedUser(t, d, "admin", "adminpw123", true)

	req := userRequest(http.MethodPost, "/api/admin/users",
		`{"username":"alice","password":"short"}`,
		authUser(adminID, "admin", true))
	rec := recordHandler(srv.CreateAdminUser, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400 body=%q", rec.Code, rec.Body.String())
	}
}

func TestPatchAdminUser_PasswordResetWipesSessions(t *testing.T) {
	ctx := context.Background()
	d := newAPITestDB(t)
	srv := New(d)
	adminID := seedUser(t, d, "admin", "adminpw123", true)
	bobID := seedUser(t, d, "bob", "bobpw1234", false)

	seedSession(t, d, bobID)
	seedSession(t, d, bobID)
	seedSession(t, d, adminID)

	req := userRequest(http.MethodPatch, "/api/admin/users/"+intStr(bobID),
		`{"password":"brand-new-pw"}`,
		authUser(adminID, "admin", true))
	rec := routedRecorder("PATCH /api/admin/users/{id}", srv.PatchAdminUser, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q want 200", rec.Code, rec.Body.String())
	}

	var bobSessions int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE user_id = ?`, bobID).Scan(&bobSessions); err != nil {
		t.Fatalf("count bob sessions: %v", err)
	}
	if bobSessions != 0 {
		t.Fatalf("bob has %d sessions after pw reset, want 0", bobSessions)
	}
	var adminSessions int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE user_id = ?`, adminID).Scan(&adminSessions); err != nil {
		t.Fatalf("count admin sessions: %v", err)
	}
	if adminSessions != 1 {
		t.Fatalf("admin sessions=%d, want 1", adminSessions)
	}
}

func TestPatchAdminUser_RejectsSelfTarget(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	adminID := seedUser(t, d, "admin", "adminpw123", true)

	req := userRequest(http.MethodPatch, "/api/admin/users/"+intStr(adminID),
		`{"is_active":false}`,
		authUser(adminID, "admin", true))
	rec := routedRecorder("PATCH /api/admin/users/{id}", srv.PatchAdminUser, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%q want 400", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "cannot modify self") {
		t.Fatalf("body=%q missing self-target message", rec.Body.String())
	}
}

func TestPatchAdminUser_LastAdminSafeguard(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	adminA := seedUser(t, d, "admin-a", "adminpw123", true)
	adminB := seedUser(t, d, "admin-b", "adminpw123", true)

	// Deactivate admin-b out-of-band so admin-a is the only ACTIVE admin.
	// Then have admin-b (still flagged is_instance_admin=1, test-bypassed
	// middleware) try to demote admin-a — that would leave zero active
	// admins and must be refused with last_admin.
	if _, err := d.Exec(`UPDATE users SET is_active = 0 WHERE id = ?`, adminB); err != nil {
		t.Fatalf("deactivate admin-b: %v", err)
	}

	req := userRequest(http.MethodPatch, "/api/admin/users/"+intStr(adminA),
		`{"is_instance_admin":false}`,
		authUser(adminB, "admin-b", true))
	rec := routedRecorder("PATCH /api/admin/users/{id}", srv.PatchAdminUser, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%q want 400", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"last_admin"`) {
		t.Fatalf("missing last_admin code: %q", rec.Body.String())
	}
}

func TestDeleteAdminUser_SoftDeleteAndSessionWipe(t *testing.T) {
	ctx := context.Background()
	d := newAPITestDB(t)
	srv := New(d)
	adminID := seedUser(t, d, "admin", "adminpw123", true)
	bobID := seedUser(t, d, "bob", "bobpw1234", false)
	seedSession(t, d, bobID)
	seedSession(t, d, bobID)

	req := userRequest(http.MethodDelete, "/api/admin/users/"+intStr(bobID), "",
		authUser(adminID, "admin", true))
	rec := routedRecorder("DELETE /api/admin/users/{id}", srv.DeleteAdminUser, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%q want 204", rec.Code, rec.Body.String())
	}

	var active int
	if err := d.QueryRowContext(ctx, `SELECT is_active FROM users WHERE id = ?`, bobID).Scan(&active); err != nil {
		t.Fatalf("read bob: %v", err)
	}
	if active != 0 {
		t.Fatalf("bob is_active=%d, want 0", active)
	}
	var sessions int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE user_id = ?`, bobID).Scan(&sessions); err != nil {
		t.Fatalf("count bob sessions: %v", err)
	}
	if sessions != 0 {
		t.Fatalf("bob sessions=%d, want 0", sessions)
	}
}

func TestDeleteAdminUser_Idempotent(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	adminID := seedUser(t, d, "admin", "adminpw123", true)
	bobID := seedUser(t, d, "bob", "bobpw1234", false)

	req := userRequest(http.MethodDelete, "/api/admin/users/"+intStr(bobID), "",
		authUser(adminID, "admin", true))
	rec := routedRecorder("DELETE /api/admin/users/{id}", srv.DeleteAdminUser, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("first delete: status=%d body=%q", rec.Code, rec.Body.String())
	}
	rec2 := routedRecorder("DELETE /api/admin/users/{id}", srv.DeleteAdminUser,
		userRequest(http.MethodDelete, "/api/admin/users/"+intStr(bobID), "",
			authUser(adminID, "admin", true)))
	if rec2.Code != http.StatusNoContent {
		t.Fatalf("second delete: status=%d body=%q want 204 (idempotent)", rec2.Code, rec2.Body.String())
	}
}

// intStr is a tiny strconv.FormatInt alias used throughout the M6.2 tests
// to build path strings like /api/admin/users/{id}.
func intStr(id int64) string { return strconv.FormatInt(id, 10) }
