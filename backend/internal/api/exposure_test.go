package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
)

// ptr is a tiny helper for building the parent map in resolver unit tests.
func i64(v int64) *int64 { return &v }

func nullStr(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }

// TestResolveExposures_Pure exercises the I/O-free core: most-open collapse,
// inheritance via an ancestor's include_descendants share, the inherited flag,
// and exposure-end expiry semantics.
func TestResolveExposures_Pure(t *testing.T) {
	// Tree: 1 (root) → 2 → 3 ; 4 (root, unshared)
	parent := map[int64]*int64{
		1: nil,
		2: i64(1),
		3: i64(2),
		4: nil,
	}
	shares := map[int64][]shareFact{
		// open share on the root, cascading to the whole subtree, expires 2099
		1: {{includeDescendants: true, isPublic: true, expiresAt: nullStr("2099-01-01 00:00:00")}},
		// direct password share on a deep page, never expires
		3: {{includeDescendants: false, isPublic: false, expiresAt: sql.NullString{}}},
	}

	got := resolveExposures(parent, shares)

	// 1: its own open share → public, direct, expires 2099.
	if e := got[1]; e.State != exposurePublic || e.Inherited || e.ExpiresAt == nil || *e.ExpiresAt != "2099-01-01 00:00:00" {
		t.Errorf("page 1: got %+v", e)
	}
	// 2: only the ancestor's open share reaches it → public, inherited, expires 2099.
	if e := got[2]; e.State != exposurePublic || !e.Inherited || e.ExpiresAt == nil || *e.ExpiresAt != "2099-01-01 00:00:00" {
		t.Errorf("page 2: got %+v", e)
	}
	// 3: direct password + inherited open → most-open public, direct (not inherited),
	// and a never-expiring contributor means exposure never ends.
	if e := got[3]; e.State != exposurePublic || e.Inherited || e.ExpiresAt != nil {
		t.Errorf("page 3: got %+v", e)
	}
	// 4: unshared → private.
	if e := got[4]; e.State != exposurePrivate || e.Inherited || e.ExpiresAt != nil {
		t.Errorf("page 4: got %+v", e)
	}
}

// TestResolveExposures_PasswordOnly: a lone password share resolves to password.
func TestResolveExposures_PasswordOnly(t *testing.T) {
	parent := map[int64]*int64{10: nil}
	shares := map[int64][]shareFact{
		10: {{isPublic: false, expiresAt: nullStr("2099-01-01 00:00:00")}},
	}
	if e := resolveExposures(parent, shares)[10]; e.State != exposurePassword {
		t.Errorf("expected password, got %+v", e)
	}
}

// TestResolveExposures_NoCascadeWithoutFlag: an ancestor share that does NOT
// include descendants must not expose the child.
func TestResolveExposures_NoCascadeWithoutFlag(t *testing.T) {
	parent := map[int64]*int64{1: nil, 2: i64(1)}
	shares := map[int64][]shareFact{
		1: {{includeDescendants: false, isPublic: true}},
	}
	got := resolveExposures(parent, shares)
	if got[1].State != exposurePublic {
		t.Errorf("page 1 should be public, got %+v", got[1])
	}
	if got[2].State != exposurePrivate {
		t.Errorf("page 2 should stay private (no cascade), got %+v", got[2])
	}
}

// TestResolveSpaceExposures_DB drives the SQL path: active/expired/revoked
// filtering and ancestor inheritance through real share_links rows.
func TestResolveSpaceExposures_DB(t *testing.T) {
	d := newAPITestDB(t)
	uid := seedUser(t, d, "alice", "pw", true)
	sid := seedSpace(t, d, "S", "s", uid)

	root := seedPageRow(t, d, sid, nil, "Root")
	child := seedPageRow(t, d, sid, &root, "Child")
	other := seedPageRow(t, d, sid, nil, "Other")

	// open share on root incl. descendants; expired share on other; revoked too
	seedShareRow(t, d, root, uid, true, false, "", "")
	seedShareRow(t, d, other, uid, false, false, "2000-01-01 00:00:00", "") // expired
	seedShareRow(t, d, other, uid, false, false, "", "2024-01-01 00:00:00") // revoked

	m, err := resolveSpaceExposures(context.Background(), d, sid)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if m[root].State != exposurePublic || m[root].Inherited {
		t.Errorf("root: %+v", m[root])
	}
	if m[child].State != exposurePublic || !m[child].Inherited {
		t.Errorf("child should inherit: %+v", m[child])
	}
	if m[other].State != exposurePrivate {
		t.Errorf("other should be private (expired + revoked only): %+v", m[other])
	}
}

// TestListAllShares_ScopedToMembership: the audit endpoint returns only shares
// in spaces the caller belongs to, enriched with page + space context.
func TestListAllShares_ScopedToMembership(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	alice := seedUser(t, d, "alice", "pw", true)
	bob := seedUser(t, d, "bob", "pw", false)

	mine := seedSpace(t, d, "Mine", "mine", alice)
	theirs := seedSpace(t, d, "Theirs", "theirs", bob)

	myPage := seedPageRow(t, d, mine, nil, "My Page")
	theirPage := seedPageRow(t, d, theirs, nil, "Their Page")
	seedShareRow(t, d, myPage, alice, false, false, "", "")
	seedShareRow(t, d, theirPage, bob, false, false, "", "")

	req := userRequest(http.MethodGet, "/api/shares", "", authUser(alice, "alice", true))
	rec := recordHandler(srv.ListAllShares, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Shares []shareAuditItem `json:"shares"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Shares) != 1 {
		t.Fatalf("expected 1 share (alice's only), got %d", len(out.Shares))
	}
	s := out.Shares[0]
	if s.PageTitle != "My Page" || s.SpaceName != "Mine" || s.SpaceID != mine {
		t.Errorf("unexpected audit item: %+v", s)
	}
	if s.URL == "" || s.Token == "" {
		t.Errorf("audit item missing url/token: %+v", s)
	}
}

// --- row seeders (direct SQL; the handlers are exercised elsewhere) ---

func seedPageRow(t *testing.T, d *sql.DB, spaceID int64, parentID *int64, title string) int64 {
	t.Helper()
	res, err := d.ExecContext(context.Background(),
		`INSERT INTO pages (space_id, parent_id, title, body, position) VALUES (?, ?, ?, '', 0)`,
		spaceID, nullableInt64(parentID), title)
	if err != nil {
		t.Fatalf("insert page %q: %v", title, err)
	}
	id, _ := res.LastInsertId()
	return id
}

// seedShareRow inserts a share_links row. passwordHash/expiresAt/revokedAt are
// applied only when non-empty.
func seedShareRow(t *testing.T, d *sql.DB, pageID, createdBy int64, includeDesc, withPassword bool, expiresAt, revokedAt string) {
	t.Helper()
	token, err := newShareToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	var pw, exp, rev sql.NullString
	if withPassword {
		pw = nullStr("argon2-placeholder")
	}
	if expiresAt != "" {
		exp = nullStr(expiresAt)
	}
	if revokedAt != "" {
		rev = nullStr(revokedAt)
	}
	inc := 0
	if includeDesc {
		inc = 1
	}
	if _, err := d.ExecContext(context.Background(),
		`INSERT INTO share_links (token, page_id, include_descendants, password_hash, created_by, expires_at, revoked_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		token, pageID, inc, pw, createdBy, exp, rev); err != nil {
		t.Fatalf("insert share_links: %v", err)
	}
}
