package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// Phase-2a feedback routing: groups + permissions + settings + the composer
// options endpoint, and the feedbackCore gates (enabled bit, allow_claude
// clamp, recipient default).

func fbPut(t *testing.T, c *http.Client, url, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	return resp
}

func TestFeedbackRouting_GroupsSettingsPermissions(t *testing.T) {
	ts, d := newWiredServer(t)
	adminID := seedUser(t, d, "admin", "testpass123", true)
	maraID := seedUser(t, d, "mara", "testpass123", false)
	admin := loginClient(t, ts, "admin", "testpass123")
	mara := loginClient(t, ts, "mara", "testpass123")

	// 1. Non-admin can't touch the admin surface.
	resp := fbPut(t, mara, ts.URL+"/api/admin/feedback/settings", `{}`)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin settings PUT status=%d want 403", resp.StatusCode)
	}
	resp.Body.Close()

	// 2. Create a group with both users.
	resp, err := admin.Post(ts.URL+"/api/admin/feedback/groups", "application/json",
		strings.NewReader(`{"name":"tela crew","member_ids":[`+itoa(adminID)+`,`+itoa(maraID)+`]}`))
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	var created struct {
		Group feedbackGroupDTO `json:"group"`
	}
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create group status=%d body=%s", resp.StatusCode, b)
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode group: %v", err)
	}
	resp.Body.Close()
	gid := created.Group.ID

	// 3. Set the group as default target; both-set is rejected.
	resp = fbPut(t, admin, ts.URL+"/api/admin/feedback/settings",
		`{"default_group_id":`+itoa(gid)+`}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings PUT status=%d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = fbPut(t, admin, ts.URL+"/api/admin/feedback/settings",
		`{"default_group_id":`+itoa(gid)+`,"default_user_id":`+itoa(adminID)+`}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("both-defaults PUT status=%d want 400", resp.StatusCode)
	}
	resp.Body.Close()

	// 4. Grant mara the Claude permission.
	resp = fbPut(t, admin, ts.URL+"/api/admin/feedback/permissions/"+itoa(maraID),
		`{"enabled":true,"allow_claude":true}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("permission PUT status=%d", resp.StatusCode)
	}
	resp.Body.Close()

	// 5. The admin bundle reflects all of it.
	resp, err = admin.Get(ts.URL + "/api/admin/feedback/settings")
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	var bundle struct {
		Settings feedbackSettingsDTO `json:"settings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&bundle); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	resp.Body.Close()
	if bundle.Settings.DefaultGroupID == nil || *bundle.Settings.DefaultGroupID != gid {
		t.Fatalf("default_group_id=%v want %d", bundle.Settings.DefaultGroupID, gid)
	}
	if len(bundle.Settings.Groups) != 1 || len(bundle.Settings.Groups[0].MemberIDs) != 2 {
		t.Fatalf("groups=%+v want 1 group with 2 members", bundle.Settings.Groups)
	}
	var maraPerm *feedbackPermissionDTO
	for i := range bundle.Settings.Permissions {
		if bundle.Settings.Permissions[i].UserID == maraID {
			maraPerm = &bundle.Settings.Permissions[i]
		}
	}
	if maraPerm == nil || !maraPerm.AllowClaude || !maraPerm.Enabled {
		t.Fatalf("mara permission=%+v want enabled+allow_claude", maraPerm)
	}

	// 6. Composer options for mara: enabled, allow_claude, sees the default.
	resp, err = mara.Get(ts.URL + "/api/feedback/options")
	if err != nil {
		t.Fatalf("get options: %v", err)
	}
	var opts struct {
		Enabled     bool `json:"enabled"`
		AllowClaude bool `json:"allow_claude"`
		Groups      []struct {
			ID int64 `json:"id"`
		} `json:"groups"`
		Default struct {
			GroupID *int64 `json:"group_id"`
		} `json:"default"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&opts); err != nil {
		t.Fatalf("decode options: %v", err)
	}
	resp.Body.Close()
	if !opts.Enabled || !opts.AllowClaude {
		t.Fatalf("options=%+v want enabled+allow_claude", opts)
	}
	if opts.Default.GroupID == nil || *opts.Default.GroupID != gid {
		t.Fatalf("options default group=%v want %d", opts.Default.GroupID, gid)
	}

	// 7. Mara files feedback with the Claude flag → sticks (she has the perm),
	// and the default recipient group is stamped.
	resp, err = mara.Post(ts.URL+"/api/feedback", "application/json",
		strings.NewReader(`{"subject":"routed","body":"please triage","claude_requested":true}`))
	if err != nil {
		t.Fatalf("post feedback: %v", err)
	}
	var env feedbackEnvelope
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("post feedback status=%d body=%s", resp.StatusCode, b)
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode feedback: %v", err)
	}
	resp.Body.Close()
	if !env.Feedback.ClaudeRequested {
		t.Fatalf("claude_requested not persisted: %+v", env.Feedback)
	}
	if env.Feedback.RecipientGroupID == nil || *env.Feedback.RecipientGroupID != gid {
		t.Fatalf("recipient_group_id=%v want default %d", env.Feedback.RecipientGroupID, gid)
	}

	// 8. Admin WITHOUT allow_claude asks for Claude → clamped to false.
	resp, err = admin.Post(ts.URL+"/api/feedback", "application/json",
		strings.NewReader(`{"subject":"no perm","body":"clamp me","claude_requested":true,"recipient_user_id":`+itoa(maraID)+`}`))
	if err != nil {
		t.Fatalf("post feedback 2: %v", err)
	}
	env = feedbackEnvelope{}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode feedback 2: %v", err)
	}
	resp.Body.Close()
	if env.Feedback.ClaudeRequested {
		t.Fatalf("claude_requested should be clamped without allow_claude")
	}
	if env.Feedback.RecipientUserID == nil || *env.Feedback.RecipientUserID != maraID {
		t.Fatalf("explicit recipient_user_id=%v want %d", env.Feedback.RecipientUserID, maraID)
	}

	// 9. Disable mara's widget access → her POST is refused.
	resp = fbPut(t, admin, ts.URL+"/api/admin/feedback/permissions/"+itoa(maraID),
		`{"enabled":false,"allow_claude":true}`)
	resp.Body.Close()
	resp, err = mara.Post(ts.URL+"/api/feedback", "application/json",
		strings.NewReader(`{"subject":"x","body":"y"}`))
	if err != nil {
		t.Fatalf("post feedback 3: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("disabled sender status=%d body=%s want 403", resp.StatusCode, b)
	}
	resp.Body.Close()

	// 10. Recipient user XOR group.
	resp, err = admin.Post(ts.URL+"/api/feedback", "application/json",
		strings.NewReader(`{"subject":"x","body":"y","recipient_user_id":1,"recipient_group_id":1}`))
	if err != nil {
		t.Fatalf("post feedback 4: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("both-recipients status=%d want 400", resp.StatusCode)
	}
	resp.Body.Close()
}

// The per-page badge endpoint: counts feedback whose context references the
// page, gated by page readability.
func TestFeedbackForPage_CountAndAccess(t *testing.T) {
	ts, d := newWiredServer(t)
	adminID := seedUser(t, d, "admin", "testpass123", true)
	_ = seedUser(t, d, "stranger", "testpass123", false)
	spaceID := seedSpace(t, d, "S", "s", adminID)
	pageID := seedPage(t, d, spaceID, "Broken page")
	admin := loginClient(t, ts, "admin", "testpass123")
	stranger := loginClient(t, ts, "stranger", "testpass123")

	// No feedback yet → 0.
	var out struct {
		Count int `json:"count"`
	}
	resp, err := admin.Get(ts.URL + "/api/feedback/for-page/" + itoa(pageID))
	if err != nil {
		t.Fatalf("get count: %v", err)
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()
	if out.Count != 0 {
		t.Fatalf("count=%d want 0", out.Count)
	}

	// File feedback about the page → 1.
	resp, err = admin.Post(ts.URL+"/api/feedback", "application/json",
		strings.NewReader(`{"subject":"broken","body":"this page misbehaves","context":{"page_id":`+itoa(pageID)+`}}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()
	resp, err = admin.Get(ts.URL + "/api/feedback/for-page/" + itoa(pageID))
	if err != nil {
		t.Fatalf("get count 2: %v", err)
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode 2: %v", err)
	}
	resp.Body.Close()
	if out.Count != 1 {
		t.Fatalf("count=%d want 1", out.Count)
	}

	// A user who can't read the page can't read its count either.
	resp, err = stranger.Get(ts.URL + "/api/feedback/for-page/" + itoa(pageID))
	if err != nil {
		t.Fatalf("get count 3: %v", err)
	}
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("stranger got %d, want a 403/404", resp.StatusCode)
	}
	resp.Body.Close()
}
