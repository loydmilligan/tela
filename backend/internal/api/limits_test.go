package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ── helpers ─────────────────────────────────────────────────────────────────

func seedPersonalSpace(t *testing.T, d *sql.DB, name, slug string, userID int64) int64 {
	t.Helper()
	var id int64
	if err := d.QueryRowContext(context.Background(),
		`INSERT INTO spaces (name, slug, personal_user_id) VALUES ($1, $2, $3) RETURNING id`,
		name, slug, userID).Scan(&id); err != nil {
		t.Fatalf("insert personal space: %v", err)
	}
	seedMember(t, d, id, userID, "owner")
	return id
}

func seedOrgSpace(t *testing.T, d *sql.DB, name, slug string, orgID int64) int64 {
	t.Helper()
	var id int64
	if err := d.QueryRowContext(context.Background(),
		`INSERT INTO spaces (name, slug, org_id) VALUES ($1, $2, $3) RETURNING id`,
		name, slug, orgID).Scan(&id); err != nil {
		t.Fatalf("insert org space: %v", err)
	}
	return id
}

func seedFile(t *testing.T, d *sql.DB, spaceID int64, name string, size int64) {
	t.Helper()
	if _, err := d.ExecContext(context.Background(),
		`INSERT INTO space_files (space_id, name, content_hash, mime, data, byte_size)
		 VALUES ($1, $2, $3, 'application/octet-stream', $4, $5)`,
		spaceID, name, name, []byte("x"), size); err != nil {
		t.Fatalf("insert space_file: %v", err)
	}
}

func setPlan(t *testing.T, d *sql.DB, kind string, id int64, planKey string) {
	t.Helper()
	table := "users"
	if kind == accountOrg {
		table = "orgs"
	}
	if _, err := d.ExecContext(context.Background(),
		`UPDATE `+table+` SET plan_key = $1 WHERE id = $2`, planKey, id); err != nil {
		t.Fatalf("set plan: %v", err)
	}
}

// tunePlan overrides a limit column on a plan row (isolated test DB, so safe).
func tunePlan(t *testing.T, d *sql.DB, key, col string, val any) {
	t.Helper()
	if _, err := d.ExecContext(context.Background(),
		`UPDATE plans SET `+col+` = $1 WHERE key = $2`, val, key); err != nil {
		t.Fatalf("tune plan: %v", err)
	}
}

// ── owning-account resolution ─────────────────────────────────────────────────

func TestSpaceOwner_Resolution(t *testing.T) {
	d := newAPITestDB(t)
	ctx := context.Background()
	u := seedUser(t, d, "owner", "pw12345678", false)
	org := seedOrg(t, d, "Acme", "acme")

	legacy := seedSpace(t, d, "Team", "team", u) // no personal_user_id / org_id
	personal := seedPersonalSpace(t, d, "Home", "home", u)
	orgSpace := seedOrgSpace(t, d, "Docs", "docs", org)

	cases := []struct {
		name    string
		spaceID int64
		want    account
	}{
		{"legacy team space → owner user", legacy, account{accountUser, u}},
		{"personal space → user", personal, account{accountUser, u}},
		{"org space → org", orgSpace, account{accountOrg, org}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := spaceOwner(ctx, d, c.spaceID)
			if err != nil || got != c.want {
				t.Fatalf("spaceOwner = %+v, %v; want %+v", got, err, c.want)
			}
		})
	}
}

// ── space quota ───────────────────────────────────────────────────────────────

func TestCheckSpaceQuota(t *testing.T) {
	d := newAPITestDB(t)
	s := &Server{DB: d}
	ctx := context.Background()
	u := seedUser(t, d, "u", "pw12345678", false) // personal_free: max_spaces 3

	// A personal home is exempt; only owned team spaces count.
	seedPersonalSpace(t, d, "Home", "home", u)
	for i := 0; i < 2; i++ {
		seedSpace(t, d, fmt.Sprintf("s%d", i), fmt.Sprintf("s%d", i), u)
	}
	if ae := s.checkSpaceQuota(ctx, account{accountUser, u}); ae != nil {
		t.Fatalf("2 owned + home should pass, got %v", ae)
	}
	seedSpace(t, d, "s3", "s3", u) // now 3 owned == limit
	ae := s.checkSpaceQuota(ctx, account{accountUser, u})
	if ae == nil || ae.Code != "quota_exceeded" || ae.Status != http.StatusPaymentRequired {
		t.Fatalf("at limit should 402 quota_exceeded, got %v", ae)
	}

	// Unlimited plan never blocks.
	setPlan(t, d, accountUser, u, "personal_plus")
	tunePlan(t, d, "personal_plus", "max_spaces", nil)
	if ae := s.checkSpaceQuota(ctx, account{accountUser, u}); ae != nil {
		t.Fatalf("unlimited plan should pass, got %v", ae)
	}
}

// ── page quota ────────────────────────────────────────────────────────────────

func TestCheckPageQuota(t *testing.T) {
	d := newAPITestDB(t)
	s := &Server{DB: d}
	ctx := context.Background()
	u := seedUser(t, d, "u", "pw12345678", false)
	space := seedSpace(t, d, "S", "s", u)
	tunePlan(t, d, "personal_free", "max_pages_per_space", 2)

	seedPage(t, d, space, "p1")
	if ae := s.checkPageQuota(ctx, space); ae != nil {
		t.Fatalf("1/2 pages should pass, got %v", ae)
	}
	seedPage(t, d, space, "p2") // at limit
	if ae := s.checkPageQuota(ctx, space); ae == nil || ae.Code != "quota_exceeded" {
		t.Fatalf("at page limit should 402, got %v", ae)
	}
}

// Bulk paths (import, cross-space move) gate N pages at once.
func TestCheckPageQuotaN(t *testing.T) {
	d := newAPITestDB(t)
	s := &Server{DB: d}
	ctx := context.Background()
	u := seedUser(t, d, "u", "pw12345678", false)
	space := seedSpace(t, d, "S", "s", u)
	tunePlan(t, d, "personal_free", "max_pages_per_space", 5)

	seedPage(t, d, space, "p1") // 1 used, 4 remaining
	if ae := s.checkPageQuotaN(ctx, space, 4); ae != nil {
		t.Fatalf("1+4 == limit should pass, got %v", ae)
	}
	if ae := s.checkPageQuotaN(ctx, space, 5); ae == nil || ae.Code != "quota_exceeded" {
		t.Fatalf("1+5 > limit should 402, got %v", ae)
	}
	if ae := s.checkPageQuotaN(ctx, space, 0); ae != nil {
		t.Fatalf("n=0 is a no-op, got %v", ae)
	}
}

// ── storage quota ─────────────────────────────────────────────────────────────

func TestCheckStorageQuota(t *testing.T) {
	d := newAPITestDB(t)
	s := &Server{DB: d}
	ctx := context.Background()
	u := seedUser(t, d, "u", "pw12345678", false)
	space := seedSpace(t, d, "S", "s", u)
	tunePlan(t, d, "personal_free", "max_storage_bytes", 1000)

	seedFile(t, d, space, "a.bin", 600)
	if ae := s.checkStorageQuota(ctx, space, 300); ae != nil {
		t.Fatalf("600+300 < 1000 should pass, got %v", ae)
	}
	if ae := s.checkStorageQuota(ctx, space, 500); ae == nil || ae.Code != "quota_exceeded" {
		t.Fatalf("600+500 > 1000 should 402, got %v", ae)
	}
	// Storage rolls up across the user's owned spaces.
	other := seedSpace(t, d, "S2", "s2", u)
	seedFile(t, d, other, "b.bin", 500)
	if ae := s.checkStorageQuota(ctx, space, 1); ae == nil {
		t.Fatalf("1100 total already over cap should 402")
	}
}

// ── seat quota ────────────────────────────────────────────────────────────────

func TestCheckSeatQuota(t *testing.T) {
	d := newAPITestDB(t)
	s := &Server{DB: d}
	ctx := context.Background()
	org := seedOrg(t, d, "Acme", "acme") // org_free: max_members 5
	tunePlan(t, d, "org_free", "max_members", 2)

	seedOrgMember(t, d, org, seedUser(t, d, "a", "pw12345678", false), orgRoleAdmin)
	if ae := s.checkSeatQuota(ctx, org); ae != nil {
		t.Fatalf("1/2 seats should pass, got %v", ae)
	}
	seedOrgMember(t, d, org, seedUser(t, d, "b", "pw12345678", false), orgRoleMember)
	if ae := s.checkSeatQuota(ctx, org); ae == nil || ae.Code != "quota_exceeded" {
		t.Fatalf("at seat limit should 402, got %v", ae)
	}
}

// ── HTTP: create-space quota + org ownership ──────────────────────────────────

func TestCreateSpace_PersonalQuota_HTTP(t *testing.T) {
	t.Setenv("TELA_DISABLE_WELCOME_SEED", "1")
	ts, d := newWiredServer(t)
	seedUser(t, d, "alice", "pw12345678", false)
	c := loginClient(t, ts, "alice", "pw12345678")

	for i := 0; i < 3; i++ {
		if code := postSpace(t, c, ts, fmt.Sprintf(`{"name":"S%d"}`, i)); code != http.StatusCreated {
			t.Fatalf("space %d: status=%d, want 201", i, code)
		}
	}
	if code := postSpace(t, c, ts, `{"name":"S4"}`); code != http.StatusPaymentRequired {
		t.Fatalf("4th space: status=%d, want 402", code)
	}
}

func TestCreateSpace_OrgOwned_HTTP(t *testing.T) {
	t.Setenv("TELA_DISABLE_WELCOME_SEED", "1")
	ts, d := newWiredServer(t)
	uid := seedUser(t, d, "alice", "pw12345678", false)
	org := seedOrg(t, d, "Acme", "acme")
	seedOrgMember(t, d, org, uid, orgRoleAdmin)
	c := loginClient(t, ts, "alice", "pw12345678")

	body := fmt.Sprintf(`{"name":"Docs","org_id":%d}`, org)
	if code := postSpace(t, c, ts, body); code != http.StatusCreated {
		t.Fatalf("org-owned space: status=%d, want 201", code)
	}
	// org_id stamped + org granted editor.
	var gotOrg sql.NullInt64
	if err := d.QueryRow(`SELECT org_id FROM spaces WHERE name='Docs'`).Scan(&gotOrg); err != nil {
		t.Fatalf("query space: %v", err)
	}
	if !gotOrg.Valid || gotOrg.Int64 != org {
		t.Fatalf("space org_id = %v, want %d", gotOrg, org)
	}
	var grants int
	d.QueryRow(`SELECT COUNT(*) FROM space_grants WHERE principal_kind='org' AND principal_id=$1 AND role='editor'`, org).Scan(&grants)
	if grants != 1 {
		t.Fatalf("org editor grant count = %d, want 1", grants)
	}

	// A non-member can't create a space owned by that org.
	seedUser(t, d, "bob", "pw12345678", false)
	cb := loginClient(t, ts, "bob", "pw12345678")
	if code := postSpace(t, cb, ts, fmt.Sprintf(`{"name":"X","org_id":%d}`, org)); code != http.StatusForbidden {
		t.Fatalf("non-member org space: status=%d, want 403", code)
	}
}

// ── HTTP: usage + set-plan ────────────────────────────────────────────────────

func TestUsageAndSetPlan_HTTP(t *testing.T) {
	ts, d := newWiredServer(t)
	uid := seedUser(t, d, "alice", "pw12345678", false)
	seedUser(t, d, "root", "pw12345678", true) // instance admin
	c := loginClient(t, ts, "alice", "pw12345678")

	// Default personal_free.
	var u usageOut
	getJSON(t, c, ts.URL+"/api/usage", &u)
	if u.Plan.Key != "personal_free" || u.AccountKind != accountUser {
		t.Fatalf("usage plan = %+v, want personal_free/user", u.Plan)
	}

	admin := loginClient(t, ts, "root", "pw12345678")

	// Wrong kind is rejected.
	if code := patchPlan(t, admin, ts, fmt.Sprintf(`{"account_kind":"user","account_id":%d,"plan_key":"org_team"}`, uid)); code != http.StatusBadRequest {
		t.Fatalf("kind mismatch: status=%d, want 400", code)
	}
	// Non-admin is rejected.
	if code := patchPlan(t, c, ts, fmt.Sprintf(`{"account_kind":"user","account_id":%d,"plan_key":"personal_plus"}`, uid)); code != http.StatusForbidden {
		t.Fatalf("non-admin set plan: status=%d, want 403", code)
	}
	// Admin upgrade works.
	if code := patchPlan(t, admin, ts, fmt.Sprintf(`{"account_kind":"user","account_id":%d,"plan_key":"personal_plus"}`, uid)); code != http.StatusOK {
		t.Fatalf("admin set plan: status=%d, want 200", code)
	}
	getJSON(t, c, ts.URL+"/api/usage", &u)
	if u.Plan.Key != "personal_plus" {
		t.Fatalf("after upgrade plan = %s, want personal_plus", u.Plan.Key)
	}
}

// ── tiny HTTP helpers ─────────────────────────────────────────────────────────

func postSpace(t *testing.T, c *http.Client, ts *httptest.Server, body string) int {
	t.Helper()
	resp, err := c.Post(ts.URL+"/api/spaces", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("post space: %v", err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func patchPlan(t *testing.T, c *http.Client, ts *httptest.Server, body string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPatch, ts.URL+"/api/admin/plan", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("patch plan: %v", err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func getJSON(t *testing.T, c *http.Client, url string, out any) {
	t.Helper()
	resp, err := c.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("get %s: status=%d body=%s", url, resp.StatusCode, b)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
}
