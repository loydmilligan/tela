package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/zcag/tela/backend/internal/auth"
)

// getNeedsSetup hits GET /api/setup/status and returns needs_setup.
func getNeedsSetup(t *testing.T, baseURL string) bool {
	t.Helper()
	resp, err := http.Get(baseURL + "/api/setup/status")
	if err != nil {
		t.Fatalf("setup status: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("setup status: status=%d body=%s", resp.StatusCode, b)
	}
	var got struct {
		NeedsSetup bool `json:"needs_setup"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	return got.NeedsSetup
}

// TestSetup_CreatesFirstAdminAndSignsIn — on an empty instance, POST /api/setup
// creates a pre-verified instance admin, provisions a personal space, returns
// the user, and sets a working session cookie. status flips needs_setup
// true→false across the call.
func TestSetup_CreatesFirstAdminAndSignsIn(t *testing.T) {
	ts, d := newWiredServer(t)

	if !getNeedsSetup(t, ts.URL) {
		t.Fatalf("needs_setup=false on empty instance; want true")
	}

	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	body := `{"username":"owner","email":"owner@example.com","password":"sup3rsecret"}`
	resp, err := c.Post(ts.URL+"/api/setup", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("setup: status=%d body=%s", resp.StatusCode, b)
	}
	var out struct {
		User struct {
			ID              int64  `json:"id"`
			Username        string `json:"username"`
			Email           string `json:"email"`
			EmailVerified   bool   `json:"email_verified"`
			IsInstanceAdmin bool   `json:"is_instance_admin"`
		} `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode setup: %v", err)
	}
	if out.User.Username != "owner" || out.User.Email != "owner@example.com" {
		t.Fatalf("user=%+v; want username=owner email=owner@example.com", out.User)
	}
	if !out.User.IsInstanceAdmin {
		t.Fatalf("is_instance_admin=false; want true")
	}
	if !out.User.EmailVerified {
		t.Fatalf("email_verified=false; want true (pre-verified)")
	}

	// DB-level admin flags.
	var isAdmin, isActive int
	var verified *string
	if err := d.QueryRow(`SELECT is_instance_admin, is_active, email_verified_at FROM users WHERE username='owner'`).
		Scan(&isAdmin, &isActive, &verified); err != nil {
		t.Fatalf("query admin row: %v", err)
	}
	if isAdmin != 1 || isActive != 1 || verified == nil {
		t.Fatalf("admin flags: is_instance_admin=%d is_active=%d verified=%v; want 1/1/non-nil", isAdmin, isActive, verified)
	}

	// Personal space provisioned + owned.
	var owns int
	if err := d.QueryRow(`
		SELECT COUNT(*) FROM space_members sm
		  JOIN spaces s ON s.id = sm.space_id
		 WHERE sm.user_id = $1 AND sm.role = 'owner' AND s.personal_user_id = $1`, out.User.ID).Scan(&owns); err != nil {
		t.Fatalf("count personal space: %v", err)
	}
	if owns != 1 {
		t.Fatalf("personal spaces owned=%d; want 1", owns)
	}

	// Session cookie set and usable: an authenticated request succeeds.
	u, _ := url.Parse(ts.URL)
	var hasSession bool
	for _, ck := range jar.Cookies(u) {
		if ck.Name == auth.CookieName && ck.Value != "" {
			hasSession = true
		}
	}
	if !hasSession {
		t.Fatalf("setup did not set a session cookie")
	}
	meResp, err := c.Get(ts.URL + "/api/auth/me")
	if err != nil {
		t.Fatalf("me: %v", err)
	}
	defer meResp.Body.Close()
	if meResp.StatusCode != http.StatusOK {
		t.Fatalf("me after setup: status=%d", meResp.StatusCode)
	}

	if getNeedsSetup(t, ts.URL) {
		t.Fatalf("needs_setup=true after setup; want false")
	}
}

// TestSetup_SecondCallConflicts — once an admin exists, a second POST /api/setup
// must 409 already_setup and never create a second user.
func TestSetup_SecondCallConflicts(t *testing.T) {
	ts, d := newWiredServer(t)

	first := `{"username":"owner","email":"owner@example.com","password":"sup3rsecret"}`
	resp, err := http.Post(ts.URL+"/api/setup", "application/json", strings.NewReader(first))
	if err != nil {
		t.Fatalf("first setup: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first setup: status=%d", resp.StatusCode)
	}

	second := `{"username":"intruder","email":"intruder@example.com","password":"anotherpw1"}`
	resp2, err := http.Post(ts.URL+"/api/setup", "application/json", strings.NewReader(second))
	if err != nil {
		t.Fatalf("second setup: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusConflict {
		b, _ := io.ReadAll(resp2.Body)
		t.Fatalf("second setup: status=%d body=%s; want 409", resp2.StatusCode, b)
	}
	var errBody struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode err: %v", err)
	}
	if errBody.Code != "already_setup" {
		t.Fatalf("code=%q; want already_setup", errBody.Code)
	}

	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if n != 1 {
		t.Fatalf("users count=%d after second setup; want 1", n)
	}
}

// TestSetup_ConcurrentCallsCreateExactlyOneAdmin — fire many setup calls at once
// against an empty instance; the atomic insert-if-empty gate must yield exactly
// one admin (one 200, the rest 409).
func TestSetup_ConcurrentCallsCreateExactlyOneAdmin(t *testing.T) {
	ts, d := newWiredServer(t)

	const n = 8
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		ok409   int
		ok200   int
		otherSt []int
	)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			body := `{"username":"owner` + string(rune('a'+i)) +
				`","email":"owner` + string(rune('a'+i)) + `@example.com","password":"sup3rsecret"}`
			resp, err := http.Post(ts.URL+"/api/setup", "application/json", strings.NewReader(body))
			if err != nil {
				return
			}
			resp.Body.Close()
			mu.Lock()
			switch resp.StatusCode {
			case http.StatusOK:
				ok200++
			case http.StatusConflict:
				ok409++
			default:
				otherSt = append(otherSt, resp.StatusCode)
			}
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	if len(otherSt) != 0 {
		t.Fatalf("unexpected statuses: %v", otherSt)
	}
	if ok200 != 1 {
		t.Fatalf("got %d successful setups; want exactly 1", ok200)
	}
	if ok409 != n-1 {
		t.Fatalf("got %d conflicts; want %d", ok409, n-1)
	}

	var users int
	if err := d.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&users); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if users != 1 {
		t.Fatalf("users count=%d; want exactly 1", users)
	}
}

// TestSetup_ValidatesInput — bad email / short password / empty username 400.
func TestSetup_ValidatesInput(t *testing.T) {
	ts, _ := newWiredServer(t)

	cases := []string{
		`{"username":"owner","email":"not-an-email","password":"sup3rsecret"}`,
		`{"username":"owner","email":"owner@example.com","password":"short"}`,
		`{"username":"","email":"owner@example.com","password":"sup3rsecret"}`,
	}
	for _, body := range cases {
		resp, err := http.Post(ts.URL+"/api/setup", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("body=%s: status=%d; want 400", body, resp.StatusCode)
		}
	}
}
