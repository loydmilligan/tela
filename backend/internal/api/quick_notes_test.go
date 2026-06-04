package api

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func postQuickNotes(t *testing.T, c *http.Client, ts *httptest.Server) struct {
	ID      int64  `json:"id"`
	SpaceID int64  `json:"space_id"`
	Title   string `json:"title"`
} {
	t.Helper()
	resp, err := c.Post(ts.URL+"/api/users/me/quick-notes", "application/json", nil)
	if err != nil {
		t.Fatalf("post quick-notes: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("quick-notes: status=%d body=%s", resp.StatusCode, b)
	}
	var out struct {
		Page struct {
			ID      int64  `json:"id"`
			SpaceID int64  `json:"space_id"`
			Title   string `json:"title"`
		} `json:"page"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode quick-notes: %v", err)
	}
	return out.Page
}

// TestQuickNotesFindOrCreate — first call creates the page in the caller's
// personal space; the second returns the same page (idempotent).
func TestQuickNotesFindOrCreate(t *testing.T) {
	ts, d := newWiredServer(t)
	seedUser(t, d, "alice", "alicepw12", false)
	c := loginClient(t, ts, "alice", "alicepw12")

	first := postQuickNotes(t, c, ts)
	if first.Title != "Quick Notes" {
		t.Fatalf("title = %q, want Quick Notes", first.Title)
	}
	if first.ID == 0 || first.SpaceID == 0 {
		t.Fatalf("missing ids: %+v", first)
	}

	second := postQuickNotes(t, c, ts)
	if second.ID != first.ID {
		t.Fatalf("not idempotent: first=%d second=%d", first.ID, second.ID)
	}

	// The page must live in alice's *personal* space.
	var personalUser sql.NullInt64
	if err := d.QueryRow(`SELECT personal_user_id FROM spaces WHERE id = ?`, first.SpaceID).
		Scan(&personalUser); err != nil {
		t.Fatalf("lookup space: %v", err)
	}
	if !personalUser.Valid {
		t.Fatalf("quick notes landed in non-personal space %d", first.SpaceID)
	}

	// Exactly one Quick Notes page exists (no duplicate from the second call).
	var n int
	if err := d.QueryRow(
		`SELECT COUNT(*) FROM pages WHERE space_id = ? AND title = 'Quick Notes'`, first.SpaceID).
		Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("want exactly 1 Quick Notes page, got %d", n)
	}
}

// TestQuickNotesRequiresAuth — the endpoint is gated.
func TestQuickNotesRequiresAuth(t *testing.T) {
	ts, _ := newWiredServer(t)
	resp, err := http.Post(ts.URL+"/api/users/me/quick-notes", "application/json", nil)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauth quick-notes: want 401, got %d", resp.StatusCode)
	}
}
