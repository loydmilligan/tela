package auth

import (
	"context"
	"crypto/rand"
	"testing"
)

func TestDeleteUserSessions_RemovesAllForUser(t *testing.T) {
	ctx := context.Background()
	d := newAuthTestDB(t)

	if _, err := BootstrapAdmin(ctx, d, "alice", "alicepw123456", "", rand.Reader); err != nil {
		t.Fatalf("bootstrap alice: %v", err)
	}
	var aliceID int64
	if err := d.QueryRowContext(ctx, `SELECT id FROM users WHERE username='alice'`).Scan(&aliceID); err != nil {
		t.Fatalf("read alice: %v", err)
	}
	// Seed a second user to make sure we only wipe alice's rows.
	hash, err := HashPassword("bobpw123456")
	if err != nil {
		t.Fatalf("hash bob: %v", err)
	}
	res, err := d.ExecContext(ctx,
		`INSERT INTO users(username,password_hash,is_instance_admin,is_active) VALUES (?,?,0,1)`,
		"bob", hash)
	if err != nil {
		t.Fatalf("insert bob: %v", err)
	}
	bobID, _ := res.LastInsertId()

	for i := 0; i < 3; i++ {
		if _, err := CreateSession(ctx, d, aliceID, "ua"); err != nil {
			t.Fatalf("create alice session %d: %v", i, err)
		}
	}
	if _, err := CreateSession(ctx, d, bobID, "ua"); err != nil {
		t.Fatalf("create bob session: %v", err)
	}

	if err := DeleteUserSessions(ctx, d, aliceID); err != nil {
		t.Fatalf("DeleteUserSessions: %v", err)
	}

	var aliceCount, bobCount int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE user_id = ?`, aliceID).Scan(&aliceCount); err != nil {
		t.Fatalf("count alice: %v", err)
	}
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE user_id = ?`, bobID).Scan(&bobCount); err != nil {
		t.Fatalf("count bob: %v", err)
	}
	if aliceCount != 0 {
		t.Errorf("alice sessions=%d, want 0", aliceCount)
	}
	if bobCount != 1 {
		t.Errorf("bob sessions=%d, want 1 (untouched)", bobCount)
	}
}

func TestDeleteUserSessionsExcept_KeepsTheException(t *testing.T) {
	ctx := context.Background()
	d := newAuthTestDB(t)

	if _, err := BootstrapAdmin(ctx, d, "alice", "alicepw123456", "", rand.Reader); err != nil {
		t.Fatalf("bootstrap alice: %v", err)
	}
	var aliceID int64
	if err := d.QueryRowContext(ctx, `SELECT id FROM users WHERE username='alice'`).Scan(&aliceID); err != nil {
		t.Fatalf("read alice: %v", err)
	}

	keep, err := CreateSession(ctx, d, aliceID, "current")
	if err != nil {
		t.Fatalf("create keep: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := CreateSession(ctx, d, aliceID, "other"); err != nil {
			t.Fatalf("create other %d: %v", i, err)
		}
	}

	if err := DeleteUserSessionsExcept(ctx, d, aliceID, keep); err != nil {
		t.Fatalf("DeleteUserSessionsExcept: %v", err)
	}

	var total, kept int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE user_id = ?`, aliceID).Scan(&total); err != nil {
		t.Fatalf("count: %v", err)
	}
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id = ?`, keep).Scan(&kept); err != nil {
		t.Fatalf("count kept: %v", err)
	}
	if total != 1 {
		t.Errorf("total=%d, want 1", total)
	}
	if kept != 1 {
		t.Errorf("kept=%d, want 1 (current preserved)", kept)
	}
}
