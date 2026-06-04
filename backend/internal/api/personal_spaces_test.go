package api

import (
	"context"
	"testing"
)

func TestEnsurePersonalSpace_Idempotent(t *testing.T) {
	d := newAPITestDB(t)
	uid := seedUser(t, d, "alice", "pw", true)

	id1, err := EnsurePersonalSpace(context.Background(), d, uid, "alice")
	if err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	id2, err := EnsurePersonalSpace(context.Background(), d, uid, "alice")
	if err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("expected same space id, got %d then %d", id1, id2)
	}

	// Exactly one personal space, named "Personal", owned by the user.
	var count int
	if err := d.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM spaces WHERE personal_user_id = $1`, uid).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 personal space, got %d", count)
	}
	var name, role string
	if err := d.QueryRowContext(context.Background(),
		`SELECT name FROM spaces WHERE id = $1`, id1).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != personalSpaceName {
		t.Errorf("name = %q, want %q", name, personalSpaceName)
	}
	if err := d.QueryRowContext(context.Background(),
		`SELECT role FROM space_members WHERE space_id = $1 AND user_id = $2`, id1, uid).Scan(&role); err != nil {
		t.Fatalf("membership lookup: %v", err)
	}
	if role != roleOwner {
		t.Errorf("role = %q, want owner", role)
	}
}

func TestEnsurePersonalSpace_UniqueSlugOnCollision(t *testing.T) {
	d := newAPITestDB(t)
	uid := seedUser(t, d, "alice", "pw", true)
	// Occupy the slug the username would normalise to.
	seedSpace(t, d, "Decoy", "alice", 0)

	id, err := EnsurePersonalSpace(context.Background(), d, uid, "alice")
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	var slug string
	if err := d.QueryRowContext(context.Background(),
		`SELECT slug FROM spaces WHERE id = $1`, id).Scan(&slug); err != nil {
		t.Fatal(err)
	}
	if slug == "alice" {
		t.Fatalf("slug should have avoided the taken 'alice', got %q", slug)
	}
}

func TestEnsurePersonalSpacesForAll_Backfill(t *testing.T) {
	d := newAPITestDB(t)
	a := seedUser(t, d, "alice", "pw", true)
	b := seedUser(t, d, "bob", "pw", false)
	// alice already has a personal space; bob does not.
	if _, err := EnsurePersonalSpace(context.Background(), d, a, "alice"); err != nil {
		t.Fatal(err)
	}

	if err := EnsurePersonalSpacesForAll(context.Background(), d); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	// Both users now have exactly one personal space; alice's wasn't duplicated.
	for _, uid := range []int64{a, b} {
		var count int
		if err := d.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM spaces WHERE personal_user_id = $1`, uid).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("user %d: expected 1 personal space, got %d", uid, count)
		}
	}
}

func TestEnsurePersonalSpacesForAll_SkipsInactive(t *testing.T) {
	d := newAPITestDB(t)
	uid := seedUser(t, d, "ghost", "pw", false)
	if _, err := d.ExecContext(context.Background(),
		`UPDATE users SET is_active = 0 WHERE id = $1`, uid); err != nil {
		t.Fatal(err)
	}
	if err := EnsurePersonalSpacesForAll(context.Background(), d); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	var count int
	if err := d.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM spaces WHERE personal_user_id = $1`, uid).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("inactive user should not be provisioned, got %d spaces", count)
	}
}
