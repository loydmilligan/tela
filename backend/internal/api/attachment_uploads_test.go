package api

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/zcag/tela/backend/internal/auth"
)

// TestMCP_UploadHandshake exercises the signed-PUT upload handshake end-to-end:
// request a PUT URL over MCP → PUT the bytes out-of-band over plain HTTP (no
// session) → confirm over MCP. Also covers single-use and an invalid token.
func TestMCP_UploadHandshake(t *testing.T) {
	ts, d := newWiredServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)
	space := seedSpace(t, d, "Alice Space", "alice-space", alice)
	var pageID int64
	if err := d.QueryRow(`INSERT INTO pages (space_id, parent_id, title, body, position)
	                       VALUES ($1, NULL, 'P', 'body', 0) RETURNING id`, space).Scan(&pageID); err != nil {
		t.Fatalf("seed page: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	sess := mcpSession(t, ctx, ts, seedReadKey(t, d, alice, auth.ScopeWrite))

	// 1. Request a signed PUT URL.
	var req struct {
		Upload struct {
			UploadID string `json:"upload_id"`
			PutURL   string `json:"put_url"`
			MaxBytes int64  `json:"max_bytes"`
		} `json:"upload"`
	}
	mcpCallJSON(t, ctx, sess, "request_attachment_upload", map[string]any{
		"page_id": pageID, "name": "report.pdf", "mime": "application/pdf",
	}, &req)
	if req.Upload.UploadID == "" || req.Upload.PutURL == "" || req.Upload.MaxBytes <= 0 {
		t.Fatalf("request result = %+v", req.Upload)
	}

	// Rewrite the put_url host to the test server (the token is what matters).
	parts := strings.SplitN(req.Upload.PutURL, "/api/uploads/", 2)
	if len(parts) != 2 {
		t.Fatalf("put_url has no token: %s", req.Upload.PutURL)
	}
	putURL := ts.URL + "/api/uploads/" + parts[1]
	payload := []byte("%PDF-1.4 hello world")

	// Invalid token → 404 (not an enumeration oracle).
	if resp := putBytes(t, ts.URL+"/api/uploads/not.atoken", payload); resp != http.StatusNotFound {
		t.Fatalf("bad-token PUT status=%d want 404", resp)
	}

	// 2. PUT the bytes out-of-band, no session.
	if resp := putBytes(t, putURL, payload); resp != http.StatusOK {
		t.Fatalf("PUT status=%d want 200", resp)
	}
	// Single-use: replay is rejected.
	if resp := putBytes(t, putURL, payload); resp != http.StatusConflict {
		t.Fatalf("PUT replay status=%d want 409", resp)
	}

	// 3. Confirm returns the stored file's ref.
	var conf struct {
		Attachment struct {
			Mime string `json:"mime"`
			URL  string `json:"url"`
		} `json:"attachment"`
	}
	mcpCallJSON(t, ctx, sess, "confirm_attachment_upload", map[string]any{
		"upload_id": req.Upload.UploadID,
	}, &conf)
	if conf.Attachment.Mime != "application/pdf" {
		t.Fatalf("confirm mime = %q want application/pdf", conf.Attachment.Mime)
	}
	if !strings.HasPrefix(conf.Attachment.URL, "/api/files/") {
		t.Fatalf("confirm url = %q want /api/files/…", conf.Attachment.URL)
	}

	// The bytes are a real attachment on the page now.
	var ls struct {
		Attachments []struct {
			Name string `json:"name"`
		} `json:"attachments"`
	}
	mcpCallJSON(t, ctx, sess, "list_attachments", map[string]any{"page_id": pageID}, &ls)
	if len(ls.Attachments) != 1 || ls.Attachments[0].Name != "report.pdf" {
		t.Fatalf("list = %+v", ls.Attachments)
	}
}

func putBytes(t *testing.T, url string, data []byte) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("new PUT: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", url, err)
	}
	resp.Body.Close()
	return resp.StatusCode
}
