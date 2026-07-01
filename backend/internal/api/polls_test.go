package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

const pollBody = "Intro paragraph.\n\n:::poll{id=\"lunch\"}\n### Where for lunch?\n\n- Lisbon\n- Berlin\n:::\n\nOutro."

func mustPost(t *testing.T, c *http.Client, url, body string) *http.Response {
	t.Helper()
	resp, err := postJSON(c, url, body)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func pageBody(t *testing.T, d *sql.DB, id int64) string {
	t.Helper()
	var b string
	if err := d.QueryRow(`SELECT body FROM pages WHERE id = $1`, id).Scan(&b); err != nil {
		t.Fatalf("read body: %v", err)
	}
	return b
}

func TestVotePoll_CastChangeRetract(t *testing.T) {
	ts, d := newWiredServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)
	space := seedSpace(t, d, "S", "s", alice)
	page := seedPageInSpace(t, d, space, nil, "Poll", pollBody)
	c := loginClient(t, ts, "alice", "alicepw12")
	voteURL := fmt.Sprintf("%s/api/pages/%d/polls/lunch/vote", ts.URL, page)

	// Cast for Berlin.
	if resp := mustPost(t, c, voteURL, `{"choice":"Berlin"}`); resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("cast status=%d body=%s", resp.StatusCode, b)
	}
	if b := pageBody(t, d, page); !strings.Contains(b, "- Berlin\n  - @alice") {
		t.Fatalf("alice not under Berlin:\n%s", b)
	}

	// Change to Lisbon — exactly one @alice remains.
	mustPost(t, c, voteURL, `{"choice":"Lisbon"}`).Body.Close()
	b := pageBody(t, d, page)
	if strings.Count(b, "@alice") != 1 || !strings.Contains(b, "- Lisbon\n  - @alice") {
		t.Fatalf("change to Lisbon wrong:\n%s", b)
	}

	// Retract.
	mustPost(t, c, voteURL, `{"choice":""}`).Body.Close()
	if b := pageBody(t, d, page); strings.Contains(b, "@alice") {
		t.Fatalf("retract left a vote:\n%s", b)
	}
}

func TestVotePoll_Errors(t *testing.T) {
	ts, d := newWiredServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)
	space := seedSpace(t, d, "S", "s", alice)
	page := seedPageInSpace(t, d, space, nil, "Poll", pollBody)
	c := loginClient(t, ts, "alice", "alicepw12")

	// Unknown option → 400.
	if resp := mustPost(t, c, fmt.Sprintf("%s/api/pages/%d/polls/lunch/vote", ts.URL, page), `{"choice":"Atlantis"}`); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown option status=%d want 400", resp.StatusCode)
	}
	// Unknown poll id → 404.
	if resp := mustPost(t, c, fmt.Sprintf("%s/api/pages/%d/polls/ghost/vote", ts.URL, page), `{"choice":"Berlin"}`); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown poll status=%d want 404", resp.StatusCode)
	}
}

func TestVotePoll_RequiresEdit(t *testing.T) {
	ts, d := newWiredServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)
	seedUser(t, d, "bob", "bobpw1234", false)
	space := seedSpace(t, d, "S", "s", alice)
	page := seedPageInSpace(t, d, space, nil, "Poll", pollBody)

	// Bob is not a member of alice's space → cannot vote.
	bob := loginClient(t, ts, "bob", "bobpw1234")
	resp := mustPost(t, bob, fmt.Sprintf("%s/api/pages/%d/polls/lunch/vote", ts.URL, page), `{"choice":"Berlin"}`)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-member status=%d want 403", resp.StatusCode)
	}
}

func TestResolveUsers(t *testing.T) {
	ts, d := newWiredServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)
	_ = alice
	c := loginClient(t, ts, "alice", "alicepw12")

	resp := mustPost(t, c, ts.URL+"/api/users/resolve", `{"handles":["@alice","ghost","alice"]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resolve status=%d", resp.StatusCode)
	}
	defer resp.Body.Close()
	var got struct {
		Users []resolvedUser `json:"users"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// @alice and alice dedupe to one; ghost doesn't exist.
	if len(got.Users) != 1 || got.Users[0].Handle != "alice" {
		t.Fatalf("got %+v, want just alice", got.Users)
	}
}
