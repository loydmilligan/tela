package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

type syncConnResp struct {
	Connection struct {
		ID      int64  `json:"id"`
		Scope   string `json:"scope"`
		SpaceID *int64 `json:"space_id"`
		Key     string `json:"key"`
	} `json:"connection"`
	Rclone struct {
		RemotePath          string `json:"remote_path"`
		ConfigCreateCommand string `json:"config_create_command"`
		SyncCommand         string `json:"sync_command"`
	} `json:"rclone"`
}

func TestSyncConnections_CreateListRevoke(t *testing.T) {
	ts, d := newWiredServer(t)
	uid := seedUser(t, d, "member", "memberpw12", false)
	space := seedSpace(t, d, "Engineering", "engineering", uid)
	other := seedSpace(t, d, "Secret", "secret", 0) // user is NOT a member
	c := loginClient(t, ts, "member", "memberpw12")

	// Space-pinned write connection.
	resp, _ := postJSON(c, ts.URL+"/api/sync/connections", fmt.Sprintf(`{"name":"laptop","space_id":%d}`, space))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d, want 201", resp.StatusCode)
	}
	var got syncConnResp
	json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if !strings.HasPrefix(got.Connection.Key, "tela_pat_") {
		t.Fatalf("missing raw key: %q", got.Connection.Key)
	}
	if got.Connection.Scope != "write" || got.Connection.SpaceID == nil || *got.Connection.SpaceID != space {
		t.Fatalf("connection scope/space wrong: %+v", got.Connection)
	}
	if !strings.Contains(got.Rclone.ConfigCreateCommand, got.Connection.Key) {
		t.Fatalf("config command missing the key:\n%s", got.Rclone.ConfigCreateCommand)
	}
	if !strings.Contains(got.Rclone.SyncCommand, "bisync") || !strings.Contains(got.Rclone.SyncCommand, "--ignore-size") {
		t.Fatalf("two-way sync command should be a bisync with --ignore-size:\n%s", got.Rclone.SyncCommand)
	}
	if !strings.HasSuffix(got.Rclone.RemotePath, ":engineering") {
		t.Fatalf("remote path not scoped to the space slug: %q", got.Rclone.RemotePath)
	}

	// Read-only, whole-workspace connection.
	resp, _ = postJSON(c, ts.URL+"/api/sync/connections", `{"name":"phone","read_only":true}`)
	var ro syncConnResp
	json.NewDecoder(resp.Body).Decode(&ro)
	resp.Body.Close()
	if ro.Connection.Scope != "read" || ro.Connection.SpaceID != nil {
		t.Fatalf("read-only workspace connection wrong: %+v", ro.Connection)
	}
	if ro.Rclone.RemotePath != "tela:" {
		t.Fatalf("workspace remote path = %q, want tela:", ro.Rclone.RemotePath)
	}
	if strings.Contains(ro.Rclone.SyncCommand, "bisync") {
		t.Fatalf("read-only should be a one-way pull, not bisync:\n%s", ro.Rclone.SyncCommand)
	}

	// Pinning to a space the user can't access is refused.
	resp, _ = postJSON(c, ts.URL+"/api/sync/connections", fmt.Sprintf(`{"name":"x","space_id":%d}`, other))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("pin to non-member space = %d, want 403", resp.StatusCode)
	}
	resp.Body.Close()

	// List shows the caller's two connections, without the raw key.
	resp, _ = c.Get(ts.URL + "/api/sync/connections")
	var list struct {
		Connections []struct {
			Key string `json:"key"`
		} `json:"connections"`
	}
	json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list.Connections) != 2 {
		t.Fatalf("list returned %d connections, want 2", len(list.Connections))
	}
	for _, conn := range list.Connections {
		if conn.Key != "" {
			t.Fatal("list must never re-expose the raw key")
		}
	}

	// Owner self-revoke via the shared api_keys delete.
	req, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/api/api_keys/%d", ts.URL, got.Connection.ID), nil)
	if r, _ := c.Do(req); r.StatusCode != http.StatusNoContent {
		t.Fatalf("self-revoke = %d, want 204", r.StatusCode)
	}
}
