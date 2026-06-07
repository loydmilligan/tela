package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestPublicUser_Home verifies /api/public/users/{username}: it lists the user's
// PUBLIC spaces + their top-level posts, never private spaces, and 404s when the
// user is missing or has nothing public.
func TestPublicUser_Home(t *testing.T) {
	ts, d := newWiredServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)
	seedUser(t, d, "bob", "bobpw1234", false)

	pub := seedSpace(t, d, "Field Notes", "field-notes", alice)
	priv := seedSpace(t, d, "Secret", "secret", alice)
	if _, err := d.Exec(`UPDATE spaces SET visibility = 'public' WHERE id = $1`, pub); err != nil {
		t.Fatalf("publish space: %v", err)
	}
	if _, err := d.Exec(`INSERT INTO pages (space_id, parent_id, title, body, position)
	                     VALUES ($1, NULL, 'Hello', 'public body', 0)`, pub); err != nil {
		t.Fatalf("seed public page: %v", err)
	}
	if _, err := d.Exec(`INSERT INTO pages (space_id, parent_id, title, body, position)
	                     VALUES ($1, NULL, 'Hidden', 'secret body', 0)`, priv); err != nil {
		t.Fatalf("seed private page: %v", err)
	}

	anon := &http.Client{}

	// alice → 200 with her one public space + its post; never the private one.
	resp, _ := anon.Get(ts.URL + "/api/public/users/alice")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /u/alice status=%d want 200", resp.StatusCode)
	}
	var got struct {
		User   struct{ Username string } `json:"user"`
		Spaces []struct {
			ID    int64  `json:"id"`
			Name  string `json:"name"`
			Pages []struct {
				Title string `json:"title"`
			} `json:"pages"`
		} `json:"spaces"`
	}
	json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if got.User.Username != "alice" {
		t.Fatalf("username=%q want alice", got.User.Username)
	}
	if len(got.Spaces) != 1 || got.Spaces[0].ID != pub {
		t.Fatalf("spaces=%+v want only the public space id=%d", got.Spaces, pub)
	}
	if len(got.Spaces[0].Pages) != 1 || got.Spaces[0].Pages[0].Title != "Hello" {
		t.Fatalf("pages=%+v want one 'Hello'", got.Spaces[0].Pages)
	}

	// Case-insensitive handle resolves to the same profile.
	if r, _ := anon.Get(ts.URL + "/api/public/users/ALICE"); r.StatusCode != http.StatusOK {
		t.Fatalf("GET /u/ALICE status=%d want 200 (case-insensitive)", r.StatusCode)
	}

	// bob exists but has no public spaces → 404.
	if r, _ := anon.Get(ts.URL + "/api/public/users/bob"); r.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /u/bob status=%d want 404 (no public content)", r.StatusCode)
	}
	// unknown user → 404.
	if r, _ := anon.Get(ts.URL + "/api/public/users/nobody"); r.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /u/nobody status=%d want 404", r.StatusCode)
	}
}
