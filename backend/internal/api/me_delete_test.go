package api

import (
	"context"
	"net/http"
	"testing"
)

func TestDeleteMyAccount_AnonymisesUser(t *testing.T) {
	ctx := context.Background()
	d := newAPITestDB(t)
	srv := New(d)

	userID := seedUser(t, d, "alice", "alicepw123", false)
	sessionID := seedSession(t, d, userID)
	// Seed a space owned solely by alice so deletion includes it.
	seedSpace(t, d, "Alice Space", "alice-space", userID)

	req := userRequestWithSession(http.MethodDelete, "/api/users/me", "", authUser(userID, "alice", false), sessionID)
	rec := recordHandler(srv.DeleteMyAccount, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q want 200", rec.Code, rec.Body.String())
	}

	// User row must be anonymised.
	var username, bio, displayName string
	var email *string
	var isActive int
	var deletedAt *string
	if err := d.QueryRowContext(ctx,
		`SELECT username, email, bio, display_name, is_active, deleted_at
		   FROM users WHERE id = $1`, userID).
		Scan(&username, &email, &bio, &displayName, &isActive, &deletedAt); err != nil {
		t.Fatalf("read user: %v", err)
	}
	if want := "deleted_" + intStr(userID); username != want {
		t.Errorf("username=%q want %q", username, want)
	}
	if email != nil {
		t.Errorf("email=%v want nil", *email)
	}
	if bio != "" {
		t.Errorf("bio=%q want empty", bio)
	}
	if displayName != "" {
		t.Errorf("display_name=%q want empty", displayName)
	}
	if isActive != 0 {
		t.Errorf("is_active=%d want 0", isActive)
	}
	if deletedAt == nil {
		t.Error("deleted_at is nil, want non-nil")
	}

	// Sessions must be wiped.
	var sessions int
	if err := d.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sessions WHERE user_id = $1`, userID).Scan(&sessions); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessions != 0 {
		t.Errorf("sessions=%d want 0", sessions)
	}

	// Sole-owner space must be deleted.
	var spaces int
	if err := d.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM spaces WHERE id IN (SELECT space_id FROM space_members WHERE user_id = $1)`,
		userID).Scan(&spaces); err != nil {
		t.Fatalf("count spaces: %v", err)
	}
	if spaces != 0 {
		t.Errorf("spaces=%d want 0", spaces)
	}
}

func TestDeleteMyAccount_BlocksSoleOrgAdmin(t *testing.T) {
	ctx := context.Background()
	d := newAPITestDB(t)
	srv := New(d)

	userID := seedUser(t, d, "bob", "bobpw1234", false)

	// Create an org and make bob the sole admin.
	var orgID int64
	if err := d.QueryRowContext(ctx,
		`INSERT INTO orgs (name, slug) VALUES ('Bob Org', 'bob-org') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if _, err := d.ExecContext(ctx,
		`INSERT INTO org_members (org_id, user_id, org_role) VALUES ($1, $2, 'admin')`,
		orgID, userID); err != nil {
		t.Fatalf("insert org_members: %v", err)
	}

	req := userRequest(http.MethodDelete, "/api/users/me", "", authUser(userID, "bob", false))
	rec := recordHandler(srv.DeleteMyAccount, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%q want 422", rec.Code, rec.Body.String())
	}

	// User must NOT have been touched.
	var isActive int
	if err := d.QueryRowContext(ctx, `SELECT is_active FROM users WHERE id = $1`, userID).Scan(&isActive); err != nil {
		t.Fatalf("read user: %v", err)
	}
	if isActive != 1 {
		t.Errorf("is_active=%d want 1 (not deleted)", isActive)
	}
}

func TestDeleteMyAccount_AllowsWhenOtherOrgAdminExists(t *testing.T) {
	ctx := context.Background()
	d := newAPITestDB(t)
	srv := New(d)

	aliceID := seedUser(t, d, "alice2", "alicepw123", false)
	bobID := seedUser(t, d, "bob2", "bobpw1234", false)

	var orgID int64
	if err := d.QueryRowContext(ctx,
		`INSERT INTO orgs (name, slug) VALUES ('Shared Org', 'shared-org') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	// Both alice and bob are admins — alice is NOT the sole admin.
	for _, uid := range []int64{aliceID, bobID} {
		if _, err := d.ExecContext(ctx,
			`INSERT INTO org_members (org_id, user_id, org_role) VALUES ($1, $2, 'admin')`,
			orgID, uid); err != nil {
			t.Fatalf("insert org_members: %v", err)
		}
	}

	req := userRequest(http.MethodDelete, "/api/users/me", "", authUser(aliceID, "alice2", false))
	rec := recordHandler(srv.DeleteMyAccount, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q want 200", rec.Code, rec.Body.String())
	}

	// Alice's membership in the org should have been removed.
	var memberships int
	if err := d.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM org_members WHERE user_id = $1`, aliceID).Scan(&memberships); err != nil {
		t.Fatalf("count memberships: %v", err)
	}
	// org_members isn't cleaned up by DeleteMyAccount (it's not in the spec);
	// but the user row should be anonymised.
	var isActive int
	if err := d.QueryRowContext(ctx, `SELECT is_active FROM users WHERE id = $1`, aliceID).Scan(&isActive); err != nil {
		t.Fatalf("read user: %v", err)
	}
	if isActive != 0 {
		t.Errorf("is_active=%d want 0", isActive)
	}
}
