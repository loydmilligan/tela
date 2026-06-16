package api

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestClientError_Records204 — an authed POST returns 204 and lands a
// client.error event row with the page target + a detail blob carrying the kind,
// message, and stack.
func TestClientError_Records204(t *testing.T) {
	ts, d := newWiredServer(t)
	seedUser(t, d, "admin", "testpass123", true)
	c := loginClient(t, ts, "admin", "testpass123")

	body := `{"kind":"collab","message":"sync wedged","stack":"at foo (a.js:1)","url":"https://x/p/9","page_id":9}`
	resp, err := c.Post(ts.URL+"/api/client-errors", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, b)
	}

	var (
		typ        string
		detail     string
		targetKind string
		targetID   int64
	)
	err = d.QueryRow(`SELECT type, detail, target_kind, target_id FROM events WHERE type = $1`, evtClientError).
		Scan(&typ, &detail, &targetKind, &targetID)
	if err != nil {
		t.Fatalf("query event: %v", err)
	}
	if targetKind != "page" || targetID != 9 {
		t.Fatalf("target=%s/%d want page/9", targetKind, targetID)
	}
	for _, want := range []string{"collab", "sync wedged", "at foo (a.js:1)"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail %q missing %q", detail, want)
		}
	}
}

// TestClientError_EmptyMessage400 — a report with no message is rejected and
// writes no event.
func TestClientError_EmptyMessage400(t *testing.T) {
	ts, d := newWiredServer(t)
	seedUser(t, d, "admin", "testpass123", true)
	c := loginClient(t, ts, "admin", "testpass123")

	resp, err := c.Post(ts.URL+"/api/client-errors", "application/json", strings.NewReader(`{"message":"  "}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d want 400", resp.StatusCode)
	}
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM events WHERE type = $1`, evtClientError).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("event rows=%d want 0", n)
	}
}

// TestClientError_RequiresAuth — no session ⇒ 401, per the non-public route.
func TestClientError_RequiresAuth(t *testing.T) {
	ts, _ := newWiredServer(t)
	resp, err := http.Post(ts.URL+"/api/client-errors", "application/json", strings.NewReader(`{"message":"x"}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", resp.StatusCode)
	}
}
