package api

import (
	"context"
	"encoding/binary"
	"fmt"
	"net/http"
	"testing"
)

// decodeStatePayload parses the tag-less sync-init payload the endpoint returns:
// u32 snapLen | snap | u32 nUpd | (u32 len + bytes)*nUpd.
func decodeStatePayload(t *testing.T, b []byte) (snap []byte, updates [][]byte) {
	t.Helper()
	off := 0
	snapLen := int(binary.BigEndian.Uint32(b[off:]))
	off += 4
	snap = b[off : off+snapLen]
	off += snapLen
	n := int(binary.BigEndian.Uint32(b[off:]))
	off += 4
	for i := 0; i < n; i++ {
		l := int(binary.BigEndian.Uint32(b[off:]))
		off += 4
		updates = append(updates, b[off:off+l])
		off += l
	}
	return snap, updates
}

func TestGetPageYjsState(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	uid := seedUser(t, d, "alice", "pw", true)
	sid := seedSpace(t, d, "S", "s", uid)
	pid := seedPageRow(t, d, sid, nil, "Doc")

	// Snapshot at seq 5, plus tail updates at 5,6,7 (5 overlaps snapshot — kept
	// as the idempotent safety buffer).
	if _, err := d.ExecContext(context.Background(),
		`INSERT INTO page_yjs_snapshots (page_id, seq, state) VALUES (?, 5, ?)`,
		pid, []byte("SNAPSHOT")); err != nil {
		t.Fatal(err)
	}
	for _, u := range []struct {
		seq int
		b   string
	}{{5, "u5"}, {6, "u6"}, {7, "u7"}} {
		if _, err := d.ExecContext(context.Background(),
			`INSERT INTO page_yjs_updates (page_id, seq, payload) VALUES (?, ?, ?)`,
			pid, u.seq, []byte(u.b)); err != nil {
			t.Fatal(err)
		}
	}

	req := userRequest(http.MethodGet, fmt.Sprintf("/api/pages/%d/yjs", pid), "", authUser(uid, "alice", true))
	rec := routedRecorder("GET /api/pages/{id}/yjs", srv.GetPageYjsState, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	snap, updates := decodeStatePayload(t, rec.Body.Bytes())
	if string(snap) != "SNAPSHOT" {
		t.Errorf("snapshot = %q, want SNAPSHOT", snap)
	}
	if len(updates) != 3 {
		t.Fatalf("expected 3 tail updates (seq>=5), got %d", len(updates))
	}

	// Non-member → 403.
	bob := seedUser(t, d, "bob", "pw", false)
	req2 := userRequest(http.MethodGet, fmt.Sprintf("/api/pages/%d/yjs", pid), "", authUser(bob, "bob", false))
	rec2 := routedRecorder("GET /api/pages/{id}/yjs", srv.GetPageYjsState, req2)
	if rec2.Code != http.StatusForbidden {
		t.Errorf("non-member status = %d, want 403", rec2.Code)
	}
}

func TestGetPageYjsState_Empty(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	uid := seedUser(t, d, "alice", "pw", true)
	sid := seedSpace(t, d, "S", "s", uid)
	pid := seedPageRow(t, d, sid, nil, "Fresh")

	req := userRequest(http.MethodGet, fmt.Sprintf("/api/pages/%d/yjs", pid), "", authUser(uid, "alice", true))
	rec := routedRecorder("GET /api/pages/{id}/yjs", srv.GetPageYjsState, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	snap, updates := decodeStatePayload(t, rec.Body.Bytes())
	if len(snap) != 0 || len(updates) != 0 {
		t.Errorf("fresh page should yield empty state, got snap=%d updates=%d", len(snap), len(updates))
	}
}
