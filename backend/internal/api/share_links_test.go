package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"
)

// shareLinkAPI is the wire shape returned in {share: ...} envelopes. Mirrors
// shareLinkDTO with json field names.
type shareLinkAPI struct {
	ID                 int64   `json:"id"`
	Token              string  `json:"token"`
	PageID             int64   `json:"page_id"`
	IncludeDescendants bool    `json:"include_descendants"`
	HasPassword        bool    `json:"has_password"`
	CreatedBy          int64   `json:"created_by"`
	CreatedAt          string  `json:"created_at"`
	ExpiresAt          *string `json:"expires_at"`
	RevokedAt          *string `json:"revoked_at"`
	URL                string  `json:"url"`
}

type shareCreateResponse struct {
	Share shareLinkAPI `json:"share"`
}

type shareListResponse struct {
	Shares []shareLinkAPI `json:"shares"`
}

type sharePageView struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	UpdatedAt string `json:"updated_at"`
}

type sharePublicGet struct {
	Share struct {
		Token              string  `json:"token"`
		IncludeDescendants bool    `json:"include_descendants"`
		HasPassword        bool    `json:"has_password"`
		ExpiresAt          *string `json:"expires_at"`
	} `json:"share"`
	Page sharePageView `json:"page"`
}

// seedPageInSpace inserts a vanilla page row directly so tests know the id
// without going through the page-create API.
func seedPageInSpace(t *testing.T, d *sql.DB, spaceID int64, parentID *int64, title, body string) int64 {
	t.Helper()
	var id int64
	var err error
	if parentID == nil {
		err = d.QueryRowContext(context.Background(),
			`INSERT INTO pages (space_id, parent_id, title, body, position)
			 VALUES ($1, NULL, $2, $3, 0) RETURNING id`, spaceID, title, body).Scan(&id)
	} else {
		err = d.QueryRowContext(context.Background(),
			`INSERT INTO pages (space_id, parent_id, title, body, position)
			 VALUES ($1, $2, $3, $4, 0) RETURNING id`, spaceID, *parentID, title, body).Scan(&id)
	}
	if err != nil {
		t.Fatalf("seed page %q: %v", title, err)
	}
	return id
}

func postJSON(c *http.Client, u, body string) (*http.Response, error) {
	return c.Post(u, "application/json", strings.NewReader(body))
}

// shareTestEnv bundles the fixture so tests don't repeat the setup dance.
type shareTestEnv struct {
	tsURL  string
	db     *sql.DB
	admin  int64
	bob    int64 // editor
	carol  int64 // viewer
	dave   int64 // non-member
	space  int64
	page   int64
	child  int64
	other  int64 // page in a different space; out of share scope
	adminC *http.Client
	bobC   *http.Client
	carolC *http.Client
	daveC  *http.Client
}

func newShareTestEnv(t *testing.T) *shareTestEnv {
	t.Helper()
	ts, d := newWiredServer(t)

	admin := seedUser(t, d, "admin", "adminpw12", true)
	bob := seedUser(t, d, "bob", "bobpw1234", false)
	carol := seedUser(t, d, "carol", "carolpw12", false)
	dave := seedUser(t, d, "dave", "davepw1234", false)
	space := seedSpace(t, d, "Test Space", "test-space", admin)
	seedMember(t, d, space, bob, roleEditor)
	seedMember(t, d, space, carol, roleViewer)

	rootBody := "hello root"
	root := seedPageInSpace(t, d, space, nil, "Root", rootBody)
	child := seedPageInSpace(t, d, space, &root, "Child", "child body")

	// A second space and a page in it so out-of-scope tests have a real
	// target (page exists but is not in the share's subtree).
	otherSpace := seedSpace(t, d, "Other Space", "other-space", admin)
	other := seedPageInSpace(t, d, otherSpace, nil, "Other", "other body")

	env := &shareTestEnv{
		tsURL:  ts.URL,
		db:     d,
		admin:  admin,
		bob:    bob,
		carol:  carol,
		dave:   dave,
		space:  space,
		page:   root,
		child:  child,
		other:  other,
		adminC: loginClient(t, ts, "admin", "adminpw12"),
		bobC:   loginClient(t, ts, "bob", "bobpw1234"),
		carolC: loginClient(t, ts, "carol", "carolpw12"),
		daveC:  loginClient(t, ts, "dave", "davepw1234"),
	}
	return env
}

// newUnauthClient returns a cookie-jar-equipped client with no session — used
// for the public token API to confirm the middleware bypass really works.
func newUnauthClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	return &http.Client{Jar: jar}
}

func TestShareLinks_Create_Success(t *testing.T) {
	env := newShareTestEnv(t)

	resp, err := postJSON(env.adminC,
		fmt.Sprintf("%s/api/pages/%d/shares", env.tsURL, env.page),
		`{"include_descendants":false}`)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, b)
	}
	var got shareCreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	sh := got.Share
	if sh.ID == 0 || sh.Token == "" || len(sh.Token) != 43 {
		t.Fatalf("token shape unexpected: %+v", sh)
	}
	if sh.PageID != env.page {
		t.Fatalf("page_id=%d want %d", sh.PageID, env.page)
	}
	if sh.IncludeDescendants {
		t.Fatalf("include_descendants=true want false")
	}
	if sh.HasPassword {
		t.Fatalf("has_password=true want false")
	}
	if sh.CreatedBy != env.admin {
		t.Fatalf("created_by=%d want %d", sh.CreatedBy, env.admin)
	}
	if sh.RevokedAt != nil {
		t.Fatalf("revoked_at=%v want nil", *sh.RevokedAt)
	}
	// The cosmetic page slug ("Root" → "root") is appended to the share URL.
	if !strings.HasSuffix(sh.URL, "/share/"+sh.Token+"/root") {
		t.Fatalf("url=%q should end with /share/{token}/root", sh.URL)
	}
}

func TestShareLinks_Create_WithPassword(t *testing.T) {
	env := newShareTestEnv(t)

	resp, err := postJSON(env.adminC,
		fmt.Sprintf("%s/api/pages/%d/shares", env.tsURL, env.page),
		`{"include_descendants":false,"password":"correct horse battery staple"}`)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, b)
	}
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "correct horse battery staple") {
		t.Fatalf("response leaks plaintext password: %s", body)
	}
	if strings.Contains(string(body), "password_hash") {
		t.Fatalf("response includes password_hash field: %s", body)
	}
	var got shareCreateResponse
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Share.HasPassword {
		t.Fatalf("has_password=false want true")
	}
	var hash sql.NullString
	if err := env.db.QueryRow(`SELECT password_hash FROM share_links WHERE id = $1`, got.Share.ID).Scan(&hash); err != nil {
		t.Fatalf("lookup hash: %v", err)
	}
	if !hash.Valid || hash.String == "" {
		t.Fatalf("password_hash not stored: %+v", hash)
	}
	if !strings.HasPrefix(hash.String, "$argon2id$") {
		t.Fatalf("password_hash not argon2id encoded: %q", hash.String)
	}
}

func TestShareLinks_Create_WithDescendants(t *testing.T) {
	env := newShareTestEnv(t)

	sh := mustShareCreate(t, env.adminC, env.tsURL, env.page,
		`{"include_descendants":true}`)
	if !sh.IncludeDescendants {
		t.Fatalf("include_descendants=false want true")
	}
	var flag int
	if err := env.db.QueryRow(`SELECT include_descendants FROM share_links WHERE id = $1`, sh.ID).Scan(&flag); err != nil {
		t.Fatalf("lookup flag: %v", err)
	}
	if flag != 1 {
		t.Fatalf("include_descendants flag=%d want 1", flag)
	}
}

func TestShareLinks_Create_ViewerForbidden(t *testing.T) {
	env := newShareTestEnv(t)
	resp, err := postJSON(env.carolC,
		fmt.Sprintf("%s/api/pages/%d/shares", env.tsURL, env.page),
		`{"include_descendants":false}`)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d want 403", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"code":"viewer_no_write"`) {
		t.Fatalf("body=%s missing viewer_no_write code", body)
	}
}

func TestShareLinks_Create_NonMember(t *testing.T) {
	env := newShareTestEnv(t)
	resp, err := postJSON(env.daveC,
		fmt.Sprintf("%s/api/pages/%d/shares", env.tsURL, env.page),
		`{"include_descendants":false}`)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d want 404", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"code":"not_found"`) {
		t.Fatalf("body=%s missing not_found code", body)
	}
}

func TestShareLinks_Create_PastExpiry(t *testing.T) {
	env := newShareTestEnv(t)
	past := time.Now().Add(-time.Hour).UTC().Format("2006-01-02 15:04:05")
	resp, err := postJSON(env.adminC,
		fmt.Sprintf("%s/api/pages/%d/shares", env.tsURL, env.page),
		fmt.Sprintf(`{"include_descendants":false,"expires_at":%q}`, past))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d want 400", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"code":"bad_request"`) {
		t.Fatalf("body=%s missing bad_request code", body)
	}
}

func TestShareLinks_List_FiltersRevoked(t *testing.T) {
	env := newShareTestEnv(t)
	open := mustShareCreate(t, env.adminC, env.tsURL, env.page,
		`{"include_descendants":false}`)
	doomed := mustShareCreate(t, env.adminC, env.tsURL, env.page,
		`{"include_descendants":true}`)
	// Revoke the second one.
	resp, err := deleteReq(env.adminC, fmt.Sprintf("%s/api/shares/%d", env.tsURL, doomed.ID))
	if err != nil || resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("revoke status=%v err=%v body=%s", resp.StatusCode, err, b)
	}
	resp.Body.Close()

	// Default — only active.
	resp, err = env.adminC.Get(fmt.Sprintf("%s/api/pages/%d/shares", env.tsURL, env.page))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var got shareListResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()
	if len(got.Shares) != 1 || got.Shares[0].ID != open.ID {
		t.Fatalf("default list got %+v want only open id=%d", got.Shares, open.ID)
	}

	// include_revoked=true — both visible, revoked one carries revoked_at.
	resp, err = env.adminC.Get(fmt.Sprintf("%s/api/pages/%d/shares?include_revoked=true", env.tsURL, env.page))
	if err != nil {
		t.Fatalf("list inc revoked: %v", err)
	}
	got.Shares = nil
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode inc revoked: %v", err)
	}
	resp.Body.Close()
	if len(got.Shares) != 2 {
		t.Fatalf("include_revoked=true got %d shares want 2", len(got.Shares))
	}
	foundRevoked := false
	for _, sh := range got.Shares {
		if sh.ID == doomed.ID {
			if sh.RevokedAt == nil {
				t.Fatalf("revoked share has nil revoked_at")
			}
			foundRevoked = true
		}
	}
	if !foundRevoked {
		t.Fatalf("revoked share missing from include_revoked=true output")
	}
}

func TestShareLinks_Patch_Password(t *testing.T) {
	env := newShareTestEnv(t)
	sh := mustShareCreate(t, env.adminC, env.tsURL, env.page,
		`{"include_descendants":false}`)

	// Set a password.
	resp, err := patchJSON(env.adminC,
		fmt.Sprintf("%s/api/shares/%d", env.tsURL, sh.ID),
		`{"password":"hunter2hunter2"}`)
	if err != nil {
		t.Fatalf("patch set: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("patch set status=%d body=%s", resp.StatusCode, b)
	}
	var got shareCreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode set: %v", err)
	}
	if !got.Share.HasPassword {
		t.Fatalf("after set has_password=false")
	}
	var hash sql.NullString
	if err := env.db.QueryRow(`SELECT password_hash FROM share_links WHERE id = $1`, sh.ID).Scan(&hash); err != nil {
		t.Fatalf("lookup hash: %v", err)
	}
	if !hash.Valid {
		t.Fatalf("password_hash is NULL after set")
	}

	// Clear it via null.
	resp2, err := patchJSON(env.adminC,
		fmt.Sprintf("%s/api/shares/%d", env.tsURL, sh.ID),
		`{"password":null}`)
	if err != nil {
		t.Fatalf("patch clear: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp2.Body)
		t.Fatalf("patch clear status=%d body=%s", resp2.StatusCode, b)
	}
	var got2 shareCreateResponse
	if err := json.NewDecoder(resp2.Body).Decode(&got2); err != nil {
		t.Fatalf("decode clear: %v", err)
	}
	if got2.Share.HasPassword {
		t.Fatalf("after clear has_password=true")
	}
	if err := env.db.QueryRow(`SELECT password_hash FROM share_links WHERE id = $1`, sh.ID).Scan(&hash); err != nil {
		t.Fatalf("lookup hash after clear: %v", err)
	}
	if hash.Valid {
		t.Fatalf("password_hash is non-NULL after clear: %q", hash.String)
	}
}

func TestShareLinks_Patch_RevokedConflict(t *testing.T) {
	env := newShareTestEnv(t)
	sh := mustShareCreate(t, env.adminC, env.tsURL, env.page,
		`{"include_descendants":false}`)
	resp, _ := deleteReq(env.adminC, fmt.Sprintf("%s/api/shares/%d", env.tsURL, sh.ID))
	resp.Body.Close()

	resp, err := patchJSON(env.adminC,
		fmt.Sprintf("%s/api/shares/%d", env.tsURL, sh.ID),
		`{"include_descendants":true}`)
	if err != nil {
		t.Fatalf("patch revoked: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d want 409", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"code":"conflict"`) {
		t.Fatalf("body=%s missing conflict code", body)
	}
}

func TestShareLinks_Patch_RejectsReadOnlyFields(t *testing.T) {
	env := newShareTestEnv(t)
	sh := mustShareCreate(t, env.adminC, env.tsURL, env.page,
		`{"include_descendants":false}`)
	for _, body := range []string{
		`{"token":"deadbeef"}`,
		`{"page_id":42}`,
		`{"created_by":99}`,
	} {
		resp, err := patchJSON(env.adminC,
			fmt.Sprintf("%s/api/shares/%d", env.tsURL, sh.ID), body)
		if err != nil {
			t.Fatalf("patch %s: %v", body, err)
		}
		if resp.StatusCode != http.StatusBadRequest {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			t.Fatalf("patch %s status=%d want 400 body=%s", body, resp.StatusCode, b)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if !strings.Contains(string(b), `"code":"bad_request"`) {
			t.Fatalf("patch %s body=%s missing bad_request code", body, b)
		}
	}
}

func TestShareLinks_Delete_Soft(t *testing.T) {
	env := newShareTestEnv(t)
	sh := mustShareCreate(t, env.adminC, env.tsURL, env.page,
		`{"include_descendants":false}`)

	resp, err := deleteReq(env.adminC, fmt.Sprintf("%s/api/shares/%d", env.tsURL, sh.ID))
	if err != nil {
		t.Fatalf("delete 1: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete 1 status=%d want 204", resp.StatusCode)
	}
	var revokedAt sql.NullString
	if err := env.db.QueryRow(`SELECT revoked_at FROM share_links WHERE id = $1`, sh.ID).Scan(&revokedAt); err != nil {
		t.Fatalf("lookup revoked_at: %v", err)
	}
	if !revokedAt.Valid || revokedAt.String == "" {
		t.Fatalf("revoked_at not set after delete: %+v", revokedAt)
	}

	// Second DELETE — idempotent 204.
	resp2, err := deleteReq(env.adminC, fmt.Sprintf("%s/api/shares/%d", env.tsURL, sh.ID))
	if err != nil {
		t.Fatalf("delete 2: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNoContent {
		t.Fatalf("delete 2 status=%d want 204 (idempotent)", resp2.StatusCode)
	}
}

func TestShareLinks_CascadeOnPageDelete(t *testing.T) {
	env := newShareTestEnv(t)
	sh := mustShareCreate(t, env.adminC, env.tsURL, env.page,
		`{"include_descendants":false}`)

	resp, err := deleteReq(env.adminC, fmt.Sprintf("%s/api/pages/%d", env.tsURL, env.page))
	if err != nil {
		t.Fatalf("delete page: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete page status=%d want 204", resp.StatusCode)
	}
	var count int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM share_links WHERE id = $1`, sh.ID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("share row count=%d after page delete, want 0 (FK CASCADE)", count)
	}
}

func TestShareGet_NoPassword_ReturnsPage(t *testing.T) {
	env := newShareTestEnv(t)
	sh := mustShareCreate(t, env.adminC, env.tsURL, env.page,
		`{"include_descendants":false}`)

	pub := newUnauthClient(t)
	resp, err := pub.Get(fmt.Sprintf("%s/api/share/%s", env.tsURL, sh.Token))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, b)
	}
	var got sharePublicGet
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Page.ID != env.page || got.Page.Title != "Root" {
		t.Fatalf("page=%+v want id=%d title=Root", got.Page, env.page)
	}
	if got.Page.Body != "hello root" {
		t.Fatalf("page.body=%q want 'hello root'", got.Page.Body)
	}
	if got.Share.HasPassword {
		t.Fatalf("has_password=true want false")
	}
}

func TestShareGet_WithPassword_RequiresCookie(t *testing.T) {
	env := newShareTestEnv(t)
	sh := mustShareCreate(t, env.adminC, env.tsURL, env.page,
		`{"include_descendants":false,"password":"hunter2hunter2"}`)

	pub := newUnauthClient(t)
	resp, err := pub.Get(fmt.Sprintf("%s/api/share/%s", env.tsURL, sh.Token))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d want 401 body=%s", resp.StatusCode, b)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"code":"password_required"`) {
		t.Fatalf("body=%s missing password_required code", body)
	}
}

func TestShareAuth_WrongPassword(t *testing.T) {
	env := newShareTestEnv(t)
	sh := mustShareCreate(t, env.adminC, env.tsURL, env.page,
		`{"include_descendants":false,"password":"hunter2hunter2"}`)

	pub := newUnauthClient(t)
	resp, err := postJSON(pub,
		fmt.Sprintf("%s/api/share/%s/auth", env.tsURL, sh.Token),
		`{"password":"wrong"}`)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d want 401 body=%s", resp.StatusCode, b)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"code":"password_required"`) {
		t.Fatalf("body=%s missing password_required code", body)
	}
	// Bucket should have one attempt now; the rate-limit test will exercise
	// the cap. Here we just confirm no cookie was set.
	if len(resp.Cookies()) > 0 {
		t.Fatalf("cookie set on wrong password: %+v", resp.Cookies())
	}
}

func TestShareAuth_CorrectPassword_SetsCookie(t *testing.T) {
	env := newShareTestEnv(t)
	sh := mustShareCreate(t, env.adminC, env.tsURL, env.page,
		`{"include_descendants":false,"password":"hunter2hunter2"}`)

	pub := newUnauthClient(t)
	resp, err := postJSON(pub,
		fmt.Sprintf("%s/api/share/%s/auth", env.tsURL, sh.Token),
		`{"password":"hunter2hunter2"}`)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d want 200 body=%s", resp.StatusCode, b)
	}

	cookieName := "tela_share_" + sh.Token
	found := false
	for _, c := range resp.Cookies() {
		if c.Name == cookieName {
			found = true
			if !c.HttpOnly {
				t.Fatalf("cookie %s not HttpOnly", cookieName)
			}
			if c.Path != "/api/share/"+sh.Token {
				t.Fatalf("cookie path=%q want %q", c.Path, "/api/share/"+sh.Token)
			}
			if c.SameSite != http.SameSiteLaxMode {
				t.Fatalf("cookie samesite=%v want Lax", c.SameSite)
			}
		}
	}
	if !found {
		t.Fatalf("cookie %s not set; got %+v", cookieName, resp.Cookies())
	}
}

func TestShareAuth_RateLimit(t *testing.T) {
	env := newShareTestEnv(t)
	sh := mustShareCreate(t, env.adminC, env.tsURL, env.page,
		`{"include_descendants":false,"password":"hunter2hunter2"}`)

	pub := newUnauthClient(t)
	authURL := fmt.Sprintf("%s/api/share/%s/auth", env.tsURL, sh.Token)
	for i := 1; i <= 5; i++ {
		resp, err := postJSON(pub, authURL, `{"password":"wrong"}`)
		if err != nil {
			t.Fatalf("attempt %d: %v", i, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d status=%d want 401", i, resp.StatusCode)
		}
	}
	// 6th — rate limited.
	resp, err := postJSON(pub, authURL, `{"password":"wrong"}`)
	if err != nil {
		t.Fatalf("attempt 6: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("attempt 6 status=%d want 429 body=%s", resp.StatusCode, b)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"code":"too_many_requests"`) {
		t.Fatalf("body=%s missing too_many_requests code", body)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Fatalf("Retry-After header missing on 429")
	}
}

func TestShareGet_WithValidCookie(t *testing.T) {
	env := newShareTestEnv(t)
	sh := mustShareCreate(t, env.adminC, env.tsURL, env.page,
		`{"include_descendants":false,"password":"hunter2hunter2"}`)

	pub := newUnauthClient(t)
	authResp, err := postJSON(pub,
		fmt.Sprintf("%s/api/share/%s/auth", env.tsURL, sh.Token),
		`{"password":"hunter2hunter2"}`)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	authResp.Body.Close()
	if authResp.StatusCode != http.StatusOK {
		t.Fatalf("auth status=%d want 200", authResp.StatusCode)
	}

	// Cookie jar should now hold the share cookie; subsequent GET succeeds.
	getURL := fmt.Sprintf("%s/api/share/%s", env.tsURL, sh.Token)
	resp, err := pub.Get(getURL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("get status=%d want 200 body=%s", resp.StatusCode, b)
	}
	var got sharePublicGet
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Page.ID != env.page {
		t.Fatalf("page id=%d want %d", got.Page.ID, env.page)
	}
}

func TestShareGet_Revoked_Returns404(t *testing.T) {
	env := newShareTestEnv(t)
	sh := mustShareCreate(t, env.adminC, env.tsURL, env.page,
		`{"include_descendants":false}`)
	delResp, _ := deleteReq(env.adminC, fmt.Sprintf("%s/api/shares/%d", env.tsURL, sh.ID))
	delResp.Body.Close()

	pub := newUnauthClient(t)
	revokedResp, err := pub.Get(fmt.Sprintf("%s/api/share/%s", env.tsURL, sh.Token))
	if err != nil {
		t.Fatalf("get revoked: %v", err)
	}
	defer revokedResp.Body.Close()
	if revokedResp.StatusCode != http.StatusNotFound {
		t.Fatalf("revoked status=%d want 404", revokedResp.StatusCode)
	}
	revokedBody, _ := io.ReadAll(revokedResp.Body)

	// Compare against a totally bogus token — body must be identical.
	bogusResp, err := pub.Get(fmt.Sprintf("%s/api/share/%s", env.tsURL,
		"definitely-not-a-real-share-token-zzzzzzzzzzzzzzzz"))
	if err != nil {
		t.Fatalf("get bogus: %v", err)
	}
	defer bogusResp.Body.Close()
	if bogusResp.StatusCode != http.StatusNotFound {
		t.Fatalf("bogus status=%d want 404", bogusResp.StatusCode)
	}
	bogusBody, _ := io.ReadAll(bogusResp.Body)
	if string(revokedBody) != string(bogusBody) {
		t.Fatalf("revoked vs bogus bodies differ:\n  revoked=%s\n  bogus=%s", revokedBody, bogusBody)
	}
}

func TestShareGet_Expired_Returns404(t *testing.T) {
	env := newShareTestEnv(t)
	sh := mustShareCreate(t, env.adminC, env.tsURL, env.page,
		`{"include_descendants":false}`)
	past := time.Now().Add(-time.Hour).UTC().Format("2006-01-02 15:04:05")
	if _, err := env.db.Exec(`UPDATE share_links SET expires_at = $1 WHERE id = $2`, past, sh.ID); err != nil {
		t.Fatalf("backdate expiry: %v", err)
	}

	pub := newUnauthClient(t)
	resp, err := pub.Get(fmt.Sprintf("%s/api/share/%s", env.tsURL, sh.Token))
	if err != nil {
		t.Fatalf("get expired: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d want 404", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"code":"not_found"`) {
		t.Fatalf("body=%s missing not_found code", body)
	}
}

func TestSharePage_OutOfScope(t *testing.T) {
	env := newShareTestEnv(t)
	// Share with include_descendants=true on root (covers root + child).
	sh := mustShareCreate(t, env.adminC, env.tsURL, env.page,
		`{"include_descendants":true}`)

	pub := newUnauthClient(t)

	// In-scope: the child page is reachable.
	resp, err := pub.Get(fmt.Sprintf("%s/api/share/%s/page/%d", env.tsURL, sh.Token, env.child))
	if err != nil {
		t.Fatalf("get child: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("in-scope status=%d want 200 body=%s", resp.StatusCode, b)
	}
	resp.Body.Close()

	// Out-of-scope: a page in a different space.
	resp2, err := pub.Get(fmt.Sprintf("%s/api/share/%s/page/%d", env.tsURL, sh.Token, env.other))
	if err != nil {
		t.Fatalf("get other: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		b, _ := io.ReadAll(resp2.Body)
		t.Fatalf("out-of-scope status=%d want 404 body=%s", resp2.StatusCode, b)
	}
	body, _ := io.ReadAll(resp2.Body)
	if !strings.Contains(string(body), `"code":"not_found"`) {
		t.Fatalf("body=%s missing not_found code", body)
	}

	// Without include_descendants, even the child is out of scope.
	rootOnly := mustShareCreate(t, env.adminC, env.tsURL, env.page,
		`{"include_descendants":false}`)
	resp3, err := pub.Get(fmt.Sprintf("%s/api/share/%s/page/%d", env.tsURL, rootOnly.Token, env.child))
	if err != nil {
		t.Fatalf("get child via root-only: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusNotFound {
		t.Fatalf("root-only child status=%d want 404", resp3.StatusCode)
	}
}

func TestShareTree_IncludesDescendants(t *testing.T) {
	env := newShareTestEnv(t)
	sh := mustShareCreate(t, env.adminC, env.tsURL, env.page,
		`{"include_descendants":true}`)

	pub := newUnauthClient(t)
	resp, err := pub.Get(fmt.Sprintf("%s/api/share/%s/tree", env.tsURL, sh.Token))
	if err != nil {
		t.Fatalf("tree: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d want 200 body=%s", resp.StatusCode, b)
	}
	var got struct {
		Pages []sharePageNode `json:"pages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Pages) != 2 {
		t.Fatalf("pages=%d want 2 (root + child) got=%+v", len(got.Pages), got.Pages)
	}
	gotIDs := map[int64]bool{}
	for _, p := range got.Pages {
		gotIDs[p.ID] = true
	}
	if !gotIDs[env.page] || !gotIDs[env.child] {
		t.Fatalf("tree missing root or child: %+v", got.Pages)
	}
}

func TestShareTree_NoDescendants_ReturnsRootOnly(t *testing.T) {
	env := newShareTestEnv(t)
	sh := mustShareCreate(t, env.adminC, env.tsURL, env.page,
		`{"include_descendants":false}`)

	pub := newUnauthClient(t)
	resp, err := pub.Get(fmt.Sprintf("%s/api/share/%s/tree", env.tsURL, sh.Token))
	if err != nil {
		t.Fatalf("tree: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200", resp.StatusCode)
	}
	var got struct {
		Pages []sharePageNode `json:"pages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Pages) != 1 || got.Pages[0].ID != env.page {
		t.Fatalf("tree=%+v want only root id=%d", got.Pages, env.page)
	}
}

// TestShare_PageMovedOutOfSubtree_Returns404 — M15.4 follow-up B (M-3 regression).
// Locked design #12: subtree scope is live. Pages moved INTO the subtree after
// share creation are included; pages moved OUT must 404. Existing tests cover
// cross-space pages that were never in scope; this one exercises the "in-scope
// then moved out" path explicitly.
func TestShare_PageMovedOutOfSubtree_Returns404(t *testing.T) {
	env := newShareTestEnv(t)
	var otherSpaceID int64
	if err := env.db.QueryRow(`SELECT space_id FROM pages WHERE id = $1`, env.other).Scan(&otherSpaceID); err != nil {
		t.Fatalf("lookup other space id: %v", err)
	}
	sh := mustShareCreate(t, env.adminC, env.tsURL, env.page,
		`{"include_descendants":true}`)

	pub := newUnauthClient(t)
	pageURL := fmt.Sprintf("%s/api/share/%s/page/%d", env.tsURL, sh.Token, env.child)

	// Pre-move: child is in scope, so the public page handler returns 200.
	pre, err := pub.Get(pageURL)
	if err != nil {
		t.Fatalf("pre-move get: %v", err)
	}
	if pre.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(pre.Body)
		pre.Body.Close()
		t.Fatalf("pre-move status=%d want 200 body=%s", pre.StatusCode, b)
	}
	pre.Body.Close()

	// Move child out of the shared subtree to Other Space (root parent).
	moveBody := fmt.Sprintf(`{"space_id":%d,"parent_id":null,"position":0}`, otherSpaceID)
	mvResp, err := postJSON(env.adminC,
		fmt.Sprintf("%s/api/pages/%d/move", env.tsURL, env.child), moveBody)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if mvResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(mvResp.Body)
		mvResp.Body.Close()
		t.Fatalf("move status=%d want 200 body=%s", mvResp.StatusCode, b)
	}
	mvResp.Body.Close()

	// Post-move: child is out of scope, public handler must 404 with not_found.
	post, err := pub.Get(pageURL)
	if err != nil {
		t.Fatalf("post-move get: %v", err)
	}
	defer post.Body.Close()
	if post.StatusCode != http.StatusNotFound {
		b, _ := io.ReadAll(post.Body)
		t.Fatalf("post-move status=%d want 404 body=%s", post.StatusCode, b)
	}
	body, _ := io.ReadAll(post.Body)
	if !strings.Contains(string(body), `"code":"not_found"`) {
		t.Fatalf("post-move body=%s missing not_found code", body)
	}
}

// TestSharePasswordAuth_RotatedXFFStillRateLimited — M15.4 follow-up B (S-2 regression).
// Before the fix, clientIPForRateLimit read the LEFTMOST X-Forwarded-For entry,
// so an attacker could rotate that header per request and mint a fresh 5/min
// bucket each time. With the RIGHTMOST + normalize fix in place, the bucket
// key is the Caddy-authored hop (rightmost), which remains stable across the
// rotation — so the 6th attempt hits the limiter.
//
// The test bypasses Caddy (httptest.Server connects the client directly to the
// backend), so we simulate Caddy's framing by sending a two-value XFF on every
// attempt: a rotating attacker-supplied LEFTMOST entry plus a fixed RIGHTMOST
// hop that stands in for what Caddy would have written. Old LEFTMOST code
// would bucket each attempt under a different IP and never hit 429; new
// RIGHTMOST code buckets all attempts under the same IP.
func TestSharePasswordAuth_RotatedXFFStillRateLimited(t *testing.T) {
	env := newShareTestEnv(t)
	sh := mustShareCreate(t, env.adminC, env.tsURL, env.page,
		`{"include_descendants":false,"password":"hunter2hunter2"}`)

	authURL := fmt.Sprintf("%s/api/share/%s/auth", env.tsURL, sh.Token)
	pub := newUnauthClient(t)
	send := func(t *testing.T, xff string, label string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, authURL,
			strings.NewReader(`{"password":"wrong"}`))
		if err != nil {
			t.Fatalf("%s build req: %v", label, err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", xff)
		resp, err := pub.Do(req)
		if err != nil {
			t.Fatalf("%s do: %v", label, err)
		}
		return resp
	}

	for i := 0; i < 5; i++ {
		spoof := fmt.Sprintf("%d.%d.%d.%d", i+1, i+1, i+1, i+1)
		// Rightmost is the "Caddy-authored" hop; fixed across requests.
		resp := send(t, spoof+", 10.0.0.42", fmt.Sprintf("attempt %d", i+1))
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d status=%d want 401", i+1, resp.StatusCode)
		}
	}
	// 6th attempt — rightmost identical to the previous 5 → bucket cap hit → 429.
	resp := send(t, "99.99.99.99, 10.0.0.42", "attempt 6")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("attempt 6 status=%d want 429 body=%s", resp.StatusCode, b)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"code":"too_many_requests"`) {
		t.Fatalf("body=%s missing too_many_requests code", body)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Fatalf("Retry-After header missing on 429")
	}
}

// Helper kept simple: returns the decoded share envelope; bails with t.Fatalf
// on any non-201 status.
func mustShareCreate(t *testing.T, c *http.Client, baseURL string, pageID int64, body string) shareLinkAPI {
	t.Helper()
	resp, err := postJSON(c, fmt.Sprintf("%s/api/pages/%d/shares", baseURL, pageID), body)
	if err != nil {
		t.Fatalf("create share: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create share status=%d body=%s", resp.StatusCode, b)
	}
	var got shareCreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode share: %v", err)
	}
	return got.Share
}
