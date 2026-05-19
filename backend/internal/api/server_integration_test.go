package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/zcag/tela/backend/internal/auth"
)

// newWiredServer spins up an httptest.Server backed by the canonical
// production handler (api.Handler) — every route + auth.Middleware in one
// piece. Tests assert behaviour end-to-end including the cookie / middleware
// / context plumbing the package-level handler tests skip.
func newWiredServer(t *testing.T) (*httptest.Server, *sql.DB) {
	t.Helper()
	d := newAPITestDB(t)
	ts := httptest.NewServer(Handler(d))
	t.Cleanup(ts.Close)
	return ts, d
}

// loginClient POSTs /api/auth/login and returns an *http.Client whose cookie
// jar carries the resulting session cookie. Subsequent requests from the
// returned client are authenticated as that user.
func loginClient(t *testing.T, ts *httptest.Server, username, password string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	c := &http.Client{Jar: jar}
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)
	resp, err := c.Post(ts.URL+"/api/auth/login", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("login %s: %v", username, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("login %s: status=%d body=%s", username, resp.StatusCode, b)
	}
	u, _ := url.Parse(ts.URL)
	for _, ck := range jar.Cookies(u) {
		if ck.Name == auth.CookieName && ck.Value != "" {
			return c
		}
	}
	t.Fatalf("login %s: session cookie missing from jar", username)
	return nil
}

// TestIntegration_LoginThenListSpaces — login wires the session cookie
// through middleware to a gated handler. Asserts ListSpaces filters to
// caller's memberships only.
func TestIntegration_LoginThenListSpaces(t *testing.T) {
	ts, d := newWiredServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)
	bob := seedUser(t, d, "bob", "bobpw1234", false)
	aliceSpace := seedSpace(t, d, "Alice Space", "alice-space", alice)
	_ = seedSpace(t, d, "Bob Space", "bob-space", bob)

	c := loginClient(t, ts, "alice", "alicepw12")
	resp, err := c.Get(ts.URL + "/api/spaces")
	if err != nil {
		t.Fatalf("get spaces: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	var got struct {
		Spaces []struct {
			ID int64 `json:"id"`
		} `json:"spaces"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Spaces) != 1 || got.Spaces[0].ID != aliceSpace {
		t.Fatalf("got %+v, want one space (id=%d)", got.Spaces, aliceSpace)
	}
}

// TestIntegration_LoginBadPassword locks the "no user-enum leak" promise:
// bad password and missing-user both return the same 401 envelope.
func TestIntegration_LoginBadPassword(t *testing.T) {
	ts, d := newWiredServer(t)
	seedUser(t, d, "alice", "alicepw12", false)

	resp, err := http.Post(ts.URL+"/api/auth/login", "application/json",
		strings.NewReader(`{"username":"alice","password":"wrong"}`))
	if err != nil {
		t.Fatalf("post login: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"code":"unauthorized"`) ||
		!strings.Contains(string(body), `"error":"invalid credentials"`) {
		t.Fatalf("body=%s missing generic unauthorized envelope", body)
	}
	for _, ck := range resp.Cookies() {
		if ck.Name == auth.CookieName && ck.Value != "" {
			t.Fatalf("login set a session cookie on bad password: %+v", ck)
		}
	}
}

// TestIntegration_AdminEndpointBlockedForNonAdmin proves requireInstanceAdmin
// fires AFTER middleware authn — only reachable when the cookie+context
// plumbing actually attaches the user.
func TestIntegration_AdminEndpointBlockedForNonAdmin(t *testing.T) {
	ts, d := newWiredServer(t)
	seedUser(t, d, "alice", "alicepw12", false)

	c := loginClient(t, ts, "alice", "alicepw12")
	resp, err := c.Get(ts.URL + "/api/admin/users")
	if err != nil {
		t.Fatalf("get admin users: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d want 403", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"code":"forbidden"`) ||
		!strings.Contains(string(body), `"error":"instance admin required"`) {
		t.Fatalf("body=%s missing forbidden envelope", body)
	}
}

// TestIntegration_AdminPATCH_SelfTargetAndDemoteSibling covers two
// admin-mutation safeguards reachable through the real HTTP stack:
//
//  1. PATCH self → 400 "cannot modify self via admin endpoint".
//  2. PATCH other admin to demote → 200 (last_admin does NOT fire when other
//     active admins still exist).
//
// The `last_admin` 400-path itself is structurally unreachable over real
// HTTP: the safeguard fires only when no other ACTIVE admin remains, but the
// auth middleware rejects inactive users (sessions.is_active=1 check in
// LoadSessionAndSlide), so a caller who could trigger it can never make it
// past authn. The package-level test (admin_users_test.go) covers the guard
// via injected fake users; this HTTP test pins the adjacent guards that ARE
// reachable, so a future refactor that breaks them still trips.
func TestIntegration_AdminPATCH_SelfTargetAndDemoteSibling(t *testing.T) {
	ts, d := newWiredServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", true)
	bob := seedUser(t, d, "bob", "bobpw1234", true)

	c := loginClient(t, ts, "alice", "alicepw12")

	// 1. self-target → 400 bad_request.
	resp, err := patchJSON(c, fmt.Sprintf("%s/api/admin/users/%d", ts.URL, alice),
		`{"is_instance_admin":false}`)
	if err != nil {
		t.Fatalf("patch self: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("self-target status=%d want 400", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), `"code":"bad_request"`) ||
		!strings.Contains(string(body), `cannot modify self`) {
		t.Fatalf("self-target body=%s missing self-target guard", body)
	}

	// 2. demote bob (the OTHER admin) → 200, no last_admin fire.
	resp, err = patchJSON(c, fmt.Sprintf("%s/api/admin/users/%d", ts.URL, bob),
		`{"is_instance_admin":false}`)
	if err != nil {
		t.Fatalf("patch bob: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("demote bob status=%d body=%s", resp.StatusCode, b)
	}
	var dto struct {
		User struct {
			IsInstanceAdmin bool `json:"is_instance_admin"`
		} `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&dto); err != nil {
		t.Fatalf("decode demote: %v", err)
	}
	if dto.User.IsInstanceAdmin {
		t.Fatalf("after demote bob.is_instance_admin=true, want false")
	}
}

// TestIntegration_SpaceMember_LastOwnerAndSelfLeave covers the membership
// lifecycle the M6.6 FE hangs on. Two assertions on one fixture:
//
//  1. DELETE /api/spaces/{id}/members/{ownerId} — last owner self-leave →
//     400 last_owner.
//  2. DELETE /api/spaces/{id}/members/{selfId} — non-owner self-leave →
//     204 (and the member row is gone).
func TestIntegration_SpaceMember_LastOwnerAndSelfLeave(t *testing.T) {
	ts, d := newWiredServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)
	bob := seedUser(t, d, "bob", "bobpw1234", false)
	space := seedSpace(t, d, "Shared", "shared", alice)
	seedMember(t, d, space, bob, roleViewer)

	// 1. alice (sole owner) tries to self-leave → 400 last_owner.
	aliceC := loginClient(t, ts, "alice", "alicepw12")
	resp, err := deleteReq(aliceC, fmt.Sprintf("%s/api/spaces/%d/members/%d", ts.URL, space, alice))
	if err != nil {
		t.Fatalf("alice self-leave: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("alice self-leave status=%d want 400", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), `"code":"last_owner"`) {
		t.Fatalf("alice self-leave body=%s missing last_owner code", body)
	}

	// 2. bob (viewer) self-leaves → 204, row gone.
	bobC := loginClient(t, ts, "bob", "bobpw1234")
	resp, err = deleteReq(bobC, fmt.Sprintf("%s/api/spaces/%d/members/%d", ts.URL, space, bob))
	if err != nil {
		t.Fatalf("bob self-leave: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("bob self-leave status=%d want 204", resp.StatusCode)
	}
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM space_members WHERE space_id = ? AND user_id = ?`,
		space, bob).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("bob row count=%d after self-leave, want 0", n)
	}
}

// TestIntegration_AuthMe_ReturnsInternalOnDBError covers the M6.7b fix
// (#55) at the HTTP level: a transient DB failure on /api/auth/me must
// surface as 500, not 401 — otherwise the FE evicts the signed-in user
// across a backend hiccup. The unit test in auth_test.go covers the handler
// directly; this test proves the same through the wired stack (middleware
// bypass on /api/auth/*, response written by writeError).
func TestIntegration_AuthMe_ReturnsInternalOnDBError(t *testing.T) {
	ts, d := newWiredServer(t)
	seedUser(t, d, "alice", "alicepw12", false)
	c := loginClient(t, ts, "alice", "alicepw12")

	if err := d.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	resp, err := c.Get(ts.URL + "/api/auth/me")
	if err != nil {
		t.Fatalf("get me: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status=%d want 500", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"code":"internal"`) {
		t.Fatalf("body=%s missing internal envelope", body)
	}
}

// TestIntegration_GetPage_MissingIdReturnsForbidden covers the M6.7d fix
// (#57) at the HTTP level: a non-member who probes a missing page id must
// see the same 403 a real non-member sees, so the response can't be used to
// enumerate page ids across spaces. Mirrors the handler-level coverage in
// pages_handlers_test.go but through the wired middleware stack.
func TestIntegration_GetPage_MissingIdReturnsForbidden(t *testing.T) {
	ts, d := newWiredServer(t)
	seedUser(t, d, "alice", "alicepw12", false)
	c := loginClient(t, ts, "alice", "alicepw12")

	resp, err := c.Get(ts.URL + "/api/pages/99999")
	if err != nil {
		t.Fatalf("get page: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d want 403", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"code":"forbidden"`) ||
		!strings.Contains(string(body), `"error":"not a member"`) {
		t.Fatalf("body=%s missing forbidden envelope", body)
	}
}

// TestComments_FullFlow exercises the M8.0 comments REST surface end-to-end
// through the wired stack: role gating, anchor validation, reply parentage,
// author-only edit, editor+ resolve, owner hard-delete, and soft-delete
// cascade visibility. One fixture covers every published constraint so a
// regression in any one branch lights up fast.
func TestComments_FullFlow(t *testing.T) {
	ts, d := newWiredServer(t)
	admin := seedUser(t, d, "admin", "adminpw12", true)
	bob := seedUser(t, d, "bob", "bobpw1234", false)
	carolID := seedUser(t, d, "carol", "carolpw12", false)
	space := seedSpace(t, d, "Test Space", "test-space", admin)
	seedMember(t, d, space, bob, "viewer")
	seedMember(t, d, space, carolID, "editor")

	// Seed a page directly so we know the id.
	res, err := d.Exec(`INSERT INTO pages (space_id, parent_id, title, body, position)
	                    VALUES (?, NULL, 'P', 'hello world body', 0)`, space)
	if err != nil {
		t.Fatalf("seed page: %v", err)
	}
	pageID, _ := res.LastInsertId()
	pageURL := fmt.Sprintf("%s/api/pages/%d/comments", ts.URL, pageID)

	adminC := loginClient(t, ts, "admin", "adminpw12")
	bobC := loginClient(t, ts, "bob", "bobpw1234")
	carolC := loginClient(t, ts, "carol", "carolpw12")

	// 1. viewer bob GET → 403.
	if r, _ := bobC.Get(pageURL); r.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer GET status=%d want 403", r.StatusCode)
	}

	// 2. viewer bob POST → 403.
	if r, _ := bobC.Post(pageURL, "application/json",
		strings.NewReader(`{"body":"x","anchor_prefix":"","anchor_exact":"hello","anchor_suffix":""}`)); r.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer POST status=%d want 403", r.StatusCode)
	}

	// 3. owner admin POST root → 201, capture ID.
	rootID := mustPostComment(t, adminC, pageURL,
		`{"body":"root by admin","anchor_prefix":"","anchor_exact":"hello","anchor_suffix":" world"}`)

	// 4. owner admin POST reply (parent=root) → 201.
	replyID := mustPostComment(t, adminC, pageURL,
		fmt.Sprintf(`{"body":"reply by admin","parent_id":%d}`, rootID))

	// 5. POST reply with parent_id of another reply → 400 comment_reply_to_reply.
	resp, _ := adminC.Post(pageURL, "application/json",
		strings.NewReader(fmt.Sprintf(`{"body":"x","parent_id":%d}`, replyID)))
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), `"code":"comment_reply_to_reply"`) {
		t.Fatalf("reply-to-reply status=%d body=%s", resp.StatusCode, body)
	}

	// 6. POST root without anchor fields → 400 comment_no_anchor.
	resp, _ = adminC.Post(pageURL, "application/json", strings.NewReader(`{"body":"no anchor"}`))
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), `"code":"comment_no_anchor"`) {
		t.Fatalf("no-anchor status=%d body=%s", resp.StatusCode, body)
	}

	// 7. author admin PATCH own comment body → 200.
	resp, _ = patchJSON(adminC, fmt.Sprintf("%s/api/comments/%d", ts.URL, rootID),
		`{"body":"edited root"}`)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("author edit status=%d body=%s", resp.StatusCode, b)
	}
	resp.Body.Close()

	// 8. non-author carol PATCH admin's comment body → 403.
	resp, _ = patchJSON(carolC, fmt.Sprintf("%s/api/comments/%d", ts.URL, rootID),
		`{"body":"hijack"}`)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-author edit status=%d want 403", resp.StatusCode)
	}
	resp.Body.Close()

	// 9a. editor carol PATCH resolved:true on root → 200.
	resp, _ = patchJSON(carolC, fmt.Sprintf("%s/api/comments/%d", ts.URL, rootID),
		`{"resolved":true}`)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("editor resolve status=%d body=%s", resp.StatusCode, b)
	}
	resp.Body.Close()

	// 9b. second PATCH resolved:true → 409 comment_already_resolved.
	resp, _ = patchJSON(carolC, fmt.Sprintf("%s/api/comments/%d", ts.URL, rootID),
		`{"resolved":true}`)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict || !strings.Contains(string(body), `"code":"comment_already_resolved"`) {
		t.Fatalf("double-resolve status=%d body=%s", resp.StatusCode, body)
	}

	// 9c. resolve on reply → 400 bad_request (replies are not resolvable).
	resp, _ = patchJSON(carolC, fmt.Sprintf("%s/api/comments/%d", ts.URL, replyID),
		`{"resolved":true}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("resolve-reply status=%d want 400", resp.StatusCode)
	}
	resp.Body.Close()

	// 9d. PATCH with both body AND resolved → 400 bad_request.
	resp, _ = patchJSON(adminC, fmt.Sprintf("%s/api/comments/%d", ts.URL, rootID),
		`{"body":"x","resolved":false}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("mutually-exclusive PATCH status=%d want 400", resp.StatusCode)
	}
	resp.Body.Close()

	// Add a fresh root authored by carol so we can verify the owner-deletes-any path.
	otherRoot := mustPostComment(t, carolC, pageURL,
		`{"body":"carol root","anchor_prefix":"","anchor_exact":"world","anchor_suffix":" body"}`)

	// 10. owner admin DELETE carol's comment → 204; row has deleted_at set.
	resp, _ = deleteReq(adminC, fmt.Sprintf("%s/api/comments/%d", ts.URL, otherRoot))
	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("owner delete status=%d body=%s", resp.StatusCode, b)
	}
	resp.Body.Close()
	var deletedAt sql.NullString
	if err := d.QueryRow(`SELECT deleted_at FROM comments WHERE id = ?`, otherRoot).Scan(&deletedAt); err != nil {
		t.Fatalf("lookup deleted comment: %v", err)
	}
	if !deletedAt.Valid {
		t.Fatalf("deleted_at not set after DELETE")
	}

	// 11. include_resolved=true GET returns the resolved root (with the reply).
	// 12. Soft-deleted root excluded from both filter modes.
	// 12b. M8.5 acceptance: resolved roots ship resolved_at + resolved_by +
	//      resolved_by_username populated so the panel can render the
	//      "Resolved by … • when" line without a second roundtrip.
	resp, _ = adminC.Get(pageURL + "?include_resolved=true")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET include_resolved status=%d", resp.StatusCode)
	}
	var got struct {
		Threads []struct {
			Root struct {
				ID                 int64   `json:"id"`
				Resolved           bool    `json:"resolved"`
				ResolvedAt         *string `json:"resolved_at"`
				ResolvedBy         *int64  `json:"resolved_by"`
				ResolvedByUsername *string `json:"resolved_by_username"`
			} `json:"root"`
			Replies []struct {
				ID int64 `json:"id"`
			} `json:"replies"`
		} `json:"threads"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	resp.Body.Close()
	if len(got.Threads) != 1 {
		t.Fatalf("got %d threads with include_resolved, want 1 (resolved admin root only — carol root soft-deleted)", len(got.Threads))
	}
	if got.Threads[0].Root.ID != rootID || !got.Threads[0].Root.Resolved {
		t.Fatalf("thread root id=%d resolved=%t, want id=%d resolved=true", got.Threads[0].Root.ID, got.Threads[0].Root.Resolved, rootID)
	}
	if got.Threads[0].Root.ResolvedAt == nil || *got.Threads[0].Root.ResolvedAt == "" {
		t.Fatalf("resolved_at empty on resolved root, want SQLite datetime string")
	}
	if got.Threads[0].Root.ResolvedBy == nil || *got.Threads[0].Root.ResolvedBy != carolID {
		t.Fatalf("resolved_by=%v, want carol id=%d", got.Threads[0].Root.ResolvedBy, carolID)
	}
	if got.Threads[0].Root.ResolvedByUsername == nil || *got.Threads[0].Root.ResolvedByUsername != "carol" {
		t.Fatalf("resolved_by_username=%v, want 'carol'", got.Threads[0].Root.ResolvedByUsername)
	}
	if len(got.Threads[0].Replies) != 1 || got.Threads[0].Replies[0].ID != replyID {
		t.Fatalf("replies=%+v, want one with id=%d", got.Threads[0].Replies, replyID)
	}

	// 13. Default GET (resolved excluded) returns no threads.
	resp, _ = adminC.Get(pageURL)
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode default list: %v", err)
	}
	resp.Body.Close()
	if len(got.Threads) != 0 {
		t.Fatalf("default GET returned %d threads, want 0 (resolved hidden)", len(got.Threads))
	}

	// 14. Reopen → thread reappears in default GET.
	resp, _ = patchJSON(adminC, fmt.Sprintf("%s/api/comments/%d", ts.URL, rootID),
		`{"resolved":false}`)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("reopen status=%d body=%s", resp.StatusCode, b)
	}
	resp.Body.Close()
	resp, _ = adminC.Get(pageURL)
	got.Threads = nil
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode after reopen: %v", err)
	}
	resp.Body.Close()
	if len(got.Threads) != 1 || got.Threads[0].Root.ID != rootID || got.Threads[0].Root.Resolved {
		t.Fatalf("after reopen got threads=%+v, want one open root id=%d", got.Threads, rootID)
	}
}

func mustPostComment(t *testing.T, c *http.Client, url, body string) int64 {
	t.Helper()
	resp, err := c.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post comment: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("post comment status=%d body=%s body-sent=%s", resp.StatusCode, b, body)
	}
	var got struct {
		Comment struct {
			ID int64 `json:"id"`
		} `json:"comment"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode comment: %v", err)
	}
	return got.Comment.ID
}

func patchJSON(c *http.Client, u, body string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPatch, u, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.Do(req)
}

func deleteReq(c *http.Client, u string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodDelete, u, nil)
	if err != nil {
		return nil, err
	}
	return c.Do(req)
}
