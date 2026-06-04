package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/zcag/tela/backend/internal/testdb"
)

// newWSTestDB returns a fresh, migrated throwaway Postgres database. The ws
// handler runs in a separate goroutine from the test (httptest.Server or `go`
// snapshots); a pool against one Postgres database is shared across all
// connections natively, so the concurrency hazard the old on-disk SQLite DB
// guarded against no longer applies.
func newWSTestDB(t *testing.T) *sql.DB {
	t.Helper()
	return testdb.New(t)
}

// newWSWiredServer mirrors newWiredServer but with the DB helper above.
func newWSWiredServer(t *testing.T) (*httptest.Server, *sql.DB) {
	t.Helper()
	d := newWSTestDB(t)
	ts := httptest.NewServer(Handler(d))
	t.Cleanup(ts.Close)
	return ts, d
}

// --- unit tests on the in-memory room registry / state machine --------------

// TestRoomRegistry_AcquireAndRelease_DropsWhenEmpty pins the lifecycle
// invariant: a room exists in the registry exactly while it has at least one
// peer. Two acquires return the same instance; the final release drops it so
// a later acquire constructs a fresh room (and thus re-runs initFromDB).
func TestRoomRegistry_AcquireAndRelease_DropsWhenEmpty(t *testing.T) {
	rr := newRoomRegistry()
	p1, p2 := &peer{}, &peer{}

	r1 := rr.acquire(7, p1)
	r2 := rr.acquire(7, p2)
	if r1 != r2 {
		t.Fatalf("acquire returned different room for same pageID: %p vs %p", r1, r2)
	}
	if got := len(r1.peers); got != 2 {
		t.Fatalf("peers count after two acquires = %d, want 2", got)
	}

	rr.release(r1, p1)
	if _, ok := rr.rooms[7]; !ok {
		t.Fatalf("room dropped while p2 still attached")
	}
	rr.release(r1, p2)
	if _, ok := rr.rooms[7]; ok {
		t.Fatalf("room not dropped after final release")
	}

	r3 := rr.acquire(7, &peer{})
	if r3 == r1 {
		t.Fatalf("acquire after drop reused stale room instance")
	}
}

// TestRoomRegistry_ReleaseClearsStuckSnapshotInFlight: the peer holding an
// outstanding snapshot-request leaves. snapInFlight must reset so the next
// threshold crossing can fire a fresh request.
func TestRoomRegistry_ReleaseClearsStuckSnapshotInFlight(t *testing.T) {
	rr := newRoomRegistry()
	p, other := &peer{}, &peer{}
	rm := rr.acquire(1, p)
	_ = rr.acquire(1, other) // keep room alive so we test the clear, not the drop
	rm.markSnapshotInFlight(p, 100)

	rr.release(rm, p)
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if rm.snapInFlight {
		t.Fatalf("snapInFlight stayed true after the requesting peer left")
	}
	if rm.snapRequestedOf != nil {
		t.Fatalf("snapRequestedOf not cleared after peer left")
	}
}

// TestRoom_InitFromDB_SeedsCountersFromExistingRows: a fresh room joining a
// page with existing persisted updates picks up the next seq + last snapshot
// seq so a reconnect doesn't overwrite history.
func TestRoom_InitFromDB_SeedsCountersFromExistingRows(t *testing.T) {
	d := newWSTestDB(t)
	spaceID := seedSpace(t, d, "S", "s", seedUser(t, d, "u", "userpw123", false))
	pageID := seedPage(t, d, spaceID, "P")
	insertUpdate(t, d, pageID, 1, []byte{0xaa})
	insertUpdate(t, d, pageID, 2, []byte{0xbb})
	insertUpdate(t, d, pageID, 3, []byte{0xcc})
	insertSnapshot(t, d, pageID, 2, []byte{0xff})

	rm := &room{pageID: pageID, peers: map[*peer]struct{}{}}
	if err := rm.initFromDB(context.Background(), d); err != nil {
		t.Fatalf("init: %v", err)
	}
	if rm.nextSeq != 4 {
		t.Fatalf("nextSeq = %d, want 4 (max(seq)=3 + 1)", rm.nextSeq)
	}
	if rm.lastSnapSeq != 2 {
		t.Fatalf("lastSnapSeq = %d, want 2", rm.lastSnapSeq)
	}
}

// TestRoom_InitFromDB_EmptyPage: nextSeq starts at 1 when nothing is persisted.
func TestRoom_InitFromDB_EmptyPage(t *testing.T) {
	d := newWSTestDB(t)
	spaceID := seedSpace(t, d, "S", "s", seedUser(t, d, "u", "userpw123", false))
	pageID := seedPage(t, d, spaceID, "P")

	rm := &room{pageID: pageID, peers: map[*peer]struct{}{}}
	if err := rm.initFromDB(context.Background(), d); err != nil {
		t.Fatalf("init: %v", err)
	}
	if rm.nextSeq != 1 {
		t.Fatalf("nextSeq for empty page = %d, want 1", rm.nextSeq)
	}
	if rm.lastSnapSeq != 0 {
		t.Fatalf("lastSnapSeq for empty page = %d, want 0", rm.lastSnapSeq)
	}
}

// TestRoom_AppendUpdate_MonotonicSeqAndPersistence: every append gets the
// next seq, the row lands in the DB, and the in-memory counter advances.
func TestRoom_AppendUpdate_MonotonicSeqAndPersistence(t *testing.T) {
	d := newWSTestDB(t)
	spaceID := seedSpace(t, d, "S", "s", seedUser(t, d, "u", "userpw123", false))
	pageID := seedPage(t, d, spaceID, "P")

	rm := &room{pageID: pageID, peers: map[*peer]struct{}{}}
	if err := rm.initFromDB(context.Background(), d); err != nil {
		t.Fatalf("init: %v", err)
	}
	ctx := context.Background()
	for i := 1; i <= 5; i++ {
		seq, shouldReq, err := rm.appendUpdate(ctx, d, []byte{byte(i)})
		if err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
		if seq != int64(i) {
			t.Fatalf("append %d assigned seq=%d, want %d", i, seq, i)
		}
		if shouldReq {
			t.Fatalf("snapshot request fired at seq=%d, threshold not yet reached", seq)
		}
	}
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM page_yjs_updates WHERE page_id = $1`, pageID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 5 {
		t.Fatalf("rows persisted = %d, want 5", n)
	}
}

// TestRoom_AppendUpdate_SnapshotTriggerAtThreshold: with snapshotEvery
// temporarily lowered, the threshold-crossing update fires shouldRequest and
// subsequent updates don't re-fire while the flag stays set.
func TestRoom_AppendUpdate_SnapshotTriggerAtThreshold(t *testing.T) {
	d := newWSTestDB(t)
	spaceID := seedSpace(t, d, "S", "s", seedUser(t, d, "u", "userpw123", false))
	pageID := seedPage(t, d, spaceID, "P")

	prev := snapshotEvery
	snapshotEvery = 3
	defer func() { snapshotEvery = prev }()

	rm := &room{pageID: pageID, peers: map[*peer]struct{}{}}
	if err := rm.initFromDB(context.Background(), d); err != nil {
		t.Fatalf("init: %v", err)
	}
	ctx := context.Background()

	for i := 1; i <= 2; i++ {
		_, shouldReq, err := rm.appendUpdate(ctx, d, []byte{byte(i)})
		if err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
		if shouldReq {
			t.Fatalf("seq=%d unexpectedly fired snapshot request", i)
		}
	}
	seq, shouldReq, err := rm.appendUpdate(ctx, d, []byte{3})
	if err != nil {
		t.Fatalf("append 3: %v", err)
	}
	if seq != 3 {
		t.Fatalf("third append seq=%d, want 3", seq)
	}
	if !shouldReq {
		t.Fatalf("seq=3 did not fire snapshot request (delta=%d, threshold=%d)", seq-rm.lastSnapSeq, snapshotEvery)
	}

	rm.markSnapshotInFlight(&peer{}, seq)
	_, shouldReq, err = rm.appendUpdate(ctx, d, []byte{4})
	if err != nil {
		t.Fatalf("append 4: %v", err)
	}
	if shouldReq {
		t.Fatalf("seq=4 fired a snapshot request while one was already in flight")
	}
}

// TestRoom_ApplySnapshot_PersistsAndSchedulesGC: a snapshot-response arriving
// while a request is in flight persists the state, advances lastSnapSeq,
// and (with snapshotGCGrace short-circuited for the test) GC's pre-snapshot
// updates.
func TestRoom_ApplySnapshot_PersistsAndSchedulesGC(t *testing.T) {
	d := newWSTestDB(t)
	spaceID := seedSpace(t, d, "S", "s", seedUser(t, d, "u", "userpw123", false))
	pageID := seedPage(t, d, spaceID, "P")

	prev := snapshotGCGrace
	snapshotGCGrace = 20 * time.Millisecond
	defer func() { snapshotGCGrace = prev }()

	rm := &room{pageID: pageID, peers: map[*peer]struct{}{}}
	if err := rm.initFromDB(context.Background(), d); err != nil {
		t.Fatalf("init: %v", err)
	}
	ctx := context.Background()
	for i := 1; i <= 4; i++ {
		if _, _, err := rm.appendUpdate(ctx, d, []byte{byte(i)}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	rm.markSnapshotInFlight(&peer{}, 4)
	if err := rm.applySnapshot(ctx, d, []byte{0xff, 0xfe, 0xfd}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	rm.mu.Lock()
	if rm.snapInFlight {
		t.Fatalf("snapInFlight not cleared after apply")
	}
	if rm.lastSnapSeq != 4 {
		t.Fatalf("lastSnapSeq = %d, want 4", rm.lastSnapSeq)
	}
	rm.mu.Unlock()

	var snapCount int
	if err := d.QueryRow(`SELECT COUNT(*) FROM page_yjs_snapshots WHERE page_id = $1`, pageID).Scan(&snapCount); err != nil {
		t.Fatalf("count snapshots: %v", err)
	}
	if snapCount != 1 {
		t.Fatalf("snapshots = %d, want 1", snapCount)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		if err := d.QueryRow(
			`SELECT COUNT(*) FROM page_yjs_updates WHERE page_id = $1 AND seq < $2`,
			pageID, 4).Scan(&n); err != nil {
			t.Fatalf("count pre-snap updates: %v", err)
		}
		if n == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	var pre, post int
	if err := d.QueryRow(`SELECT COUNT(*) FROM page_yjs_updates WHERE page_id = $1 AND seq < $2`, pageID, 4).Scan(&pre); err != nil {
		t.Fatalf("count pre: %v", err)
	}
	if err := d.QueryRow(`SELECT COUNT(*) FROM page_yjs_updates WHERE page_id = $1 AND seq >= $2`, pageID, 4).Scan(&post); err != nil {
		t.Fatalf("count post: %v", err)
	}
	if pre != 0 {
		t.Fatalf("pre-snapshot updates still present after grace: %d", pre)
	}
	if post != 1 {
		t.Fatalf("seq>=4 retained = %d, want 1 (safety buffer)", post)
	}
}

// TestRoom_ApplySnapshot_NoOpWhenNoneInFlight: stale or duplicate
// snapshot-response must NOT mutate state.
func TestRoom_ApplySnapshot_NoOpWhenNoneInFlight(t *testing.T) {
	d := newWSTestDB(t)
	spaceID := seedSpace(t, d, "S", "s", seedUser(t, d, "u", "userpw123", false))
	pageID := seedPage(t, d, spaceID, "P")
	rm := &room{pageID: pageID, peers: map[*peer]struct{}{}}
	if err := rm.initFromDB(context.Background(), d); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := rm.applySnapshot(context.Background(), d, []byte{0x01}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM page_yjs_snapshots WHERE page_id = $1`, pageID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("stale snapshot persisted (%d rows)", n)
	}
}

// --- wire protocol round-trip -----------------------------------------------

// TestEncodeSyncInit_RoundTrip: snapshot + updates pack must decode back to
// byte-identical inputs, including the empty-snapshot edge case.
func TestEncodeSyncInit_RoundTrip(t *testing.T) {
	cases := []struct {
		name    string
		snap    []byte
		updates [][]byte
	}{
		{"empty", nil, nil},
		{"snap only", []byte("snapshot blob"), nil},
		{"updates only", nil, [][]byte{[]byte("u1"), []byte("u2"), []byte("u3")}},
		{"snap and updates", []byte("S"), [][]byte{[]byte("A"), []byte("BB"), []byte("CCC")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frame := encodeSyncInit(tc.snap, tc.updates)
			snap, updates, err := decodeSyncInit(frame)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if !bytes.Equal(snap, tc.snap) {
				t.Fatalf("snap mismatch: got %x, want %x", snap, tc.snap)
			}
			if len(updates) != len(tc.updates) {
				t.Fatalf("updates count: got %d, want %d", len(updates), len(tc.updates))
			}
			for i := range updates {
				if !bytes.Equal(updates[i], tc.updates[i]) {
					t.Fatalf("update[%d] mismatch: got %x, want %x", i, updates[i], tc.updates[i])
				}
			}
		})
	}
}

// --- end-to-end through the wired stack -------------------------------------

// TestIntegration_WSPage_RejectsViewerWith403: viewer role gets a clean 403
// pre-upgrade. Editors / owners can upgrade.
func TestIntegration_WSPage_RejectsViewerWith403(t *testing.T) {
	ts, d := newWSWiredServer(t)
	owner := seedUser(t, d, "owner", "ownerpw1", false)
	viewer := seedUser(t, d, "viewer", "viewerpw1", false)
	space := seedSpace(t, d, "S", "s", owner)
	seedMember(t, d, space, viewer, roleViewer)
	pageID := seedPage(t, d, space, "P")

	c := loginClient(t, ts, "viewer", "viewerpw1")
	conn, resp, err := websocket.Dial(context.Background(), wsURLFor(ts, pageID),
		&websocket.DialOptions{HTTPClient: c})
	if err == nil {
		conn.CloseNow()
		t.Fatalf("viewer ws upgrade succeeded; want 403")
	}
	if resp == nil {
		t.Fatalf("viewer ws upgrade error has no response: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer ws upgrade status = %d, want 403", resp.StatusCode)
	}
}

// TestIntegration_WSPage_NonMemberGets403: non-member sees the same forbidden
// envelope a missing page would produce — no cross-space enumeration.
func TestIntegration_WSPage_NonMemberGets403(t *testing.T) {
	ts, d := newWSWiredServer(t)
	owner := seedUser(t, d, "owner", "ownerpw1", false)
	seedUser(t, d, "out", "outsidepw1", false)
	space := seedSpace(t, d, "S", "s", owner)
	pageID := seedPage(t, d, space, "P")

	c := loginClient(t, ts, "out", "outsidepw1")
	conn, resp, err := websocket.Dial(context.Background(), wsURLFor(ts, pageID),
		&websocket.DialOptions{HTTPClient: c})
	if err == nil {
		conn.CloseNow()
		t.Fatalf("non-member ws upgrade succeeded; want 403")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-member ws upgrade status: %+v, err=%v", resp, err)
	}
}

// TestIntegration_WSPage_PersistsLargeUpdate: a single >64KB binary frame
// must round-trip through Accept (which negotiates the extended payload
// length) and land in page_yjs_updates with the right size.
//
// This is the "frame size > 64KB" check called out in #62 — Yjs full-state
// sync vectors can exceed 64KB and the relay must handle 16-bit extended
// payload-length framing end-to-end.
func TestIntegration_WSPage_PersistsLargeUpdate(t *testing.T) {
	ts, d := newWSWiredServer(t)
	owner := seedUser(t, d, "owner", "ownerpw1", false)
	space := seedSpace(t, d, "S", "s", owner)
	pageID := seedPage(t, d, space, "P")

	c := loginClient(t, ts, "owner", "ownerpw1")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURLFor(ts, pageID),
		&websocket.DialOptions{HTTPClient: c})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(wsReadLimit)

	// Drain the server's sync-init frame so subsequent reads are clean.
	typ, frame, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read sync-init: %v", err)
	}
	if typ != websocket.MessageBinary || len(frame) < 1 || frame[0] != tagSyncInit {
		t.Fatalf("first frame: tag=%x typ=%d, want sync-init", frame, typ)
	}

	// 200KB blob — well above the 64KB extended-length boundary.
	blob := make([]byte, 200*1024)
	for i := range blob {
		blob[i] = byte(i % 251)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, encodeFrame(tagUpdate, blob)); err != nil {
		t.Fatalf("write large frame: %v", err)
	}

	var got []byte
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		err := d.QueryRow(
			`SELECT payload FROM page_yjs_updates WHERE page_id = $1 AND seq = 1`,
			pageID).Scan(&got)
		if err == nil {
			break
		}
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("query update: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	if len(got) != len(blob) {
		t.Fatalf("persisted size = %d, want %d", len(got), len(blob))
	}
	if !bytes.Equal(got, blob) {
		t.Fatalf("persisted blob does not match sent blob")
	}
}

// TestIntegration_WSPage_BroadcastsBetweenPeers: two peers join the same
// page. An update from peer A reaches peer B verbatim.
func TestIntegration_WSPage_BroadcastsBetweenPeers(t *testing.T) {
	ts, d := newWSWiredServer(t)
	owner := seedUser(t, d, "owner", "ownerpw1", false)
	editor := seedUser(t, d, "editor", "editorpw1", false)
	space := seedSpace(t, d, "S", "s", owner)
	seedMember(t, d, space, editor, roleEditor)
	pageID := seedPage(t, d, space, "P")

	ownerC := loginClient(t, ts, "owner", "ownerpw1")
	editorC := loginClient(t, ts, "editor", "editorpw1")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, _, err := websocket.Dial(ctx, wsURLFor(ts, pageID),
		&websocket.DialOptions{HTTPClient: ownerC})
	if err != nil {
		t.Fatalf("dial a: %v", err)
	}
	defer a.CloseNow()
	b, _, err := websocket.Dial(ctx, wsURLFor(ts, pageID),
		&websocket.DialOptions{HTTPClient: editorC})
	if err != nil {
		t.Fatalf("dial b: %v", err)
	}
	defer b.CloseNow()

	if _, _, err := a.Read(ctx); err != nil {
		t.Fatalf("a sync-init: %v", err)
	}
	if _, _, err := b.Read(ctx); err != nil {
		t.Fatalf("b sync-init: %v", err)
	}

	payload := []byte("hello yjs")
	if err := a.Write(ctx, websocket.MessageBinary, encodeFrame(tagUpdate, payload)); err != nil {
		t.Fatalf("a write: %v", err)
	}
	typ, got, err := b.Read(ctx)
	if err != nil {
		t.Fatalf("b read: %v", err)
	}
	if typ != websocket.MessageBinary || len(got) < 1 || got[0] != tagUpdate {
		t.Fatalf("b got tag=%x typ=%d, want update", got, typ)
	}
	if !bytes.Equal(got[1:], payload) {
		t.Fatalf("b payload = %x, want %x", got[1:], payload)
	}
}

// TestIntegration_WSPage_AwarenessRebroadcastsToOthersNotSender: tag 0x05
// awareness from peer A reaches peer B verbatim. Peer A must NOT see its own
// awareness echoed (otherwise applyAwarenessUpdate would treat its own state
// as a remote peer).
func TestIntegration_WSPage_AwarenessRebroadcastsToOthersNotSender(t *testing.T) {
	ts, d := newWSWiredServer(t)
	owner := seedUser(t, d, "owner", "ownerpw1", false)
	editor := seedUser(t, d, "editor", "editorpw1", false)
	space := seedSpace(t, d, "S", "s", owner)
	seedMember(t, d, space, editor, roleEditor)
	pageID := seedPage(t, d, space, "P")

	ownerC := loginClient(t, ts, "owner", "ownerpw1")
	editorC := loginClient(t, ts, "editor", "editorpw1")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, _, err := websocket.Dial(ctx, wsURLFor(ts, pageID),
		&websocket.DialOptions{HTTPClient: ownerC})
	if err != nil {
		t.Fatalf("dial a: %v", err)
	}
	defer a.CloseNow()
	b, _, err := websocket.Dial(ctx, wsURLFor(ts, pageID),
		&websocket.DialOptions{HTTPClient: editorC})
	if err != nil {
		t.Fatalf("dial b: %v", err)
	}
	defer b.CloseNow()

	if _, _, err := a.Read(ctx); err != nil {
		t.Fatalf("a sync-init: %v", err)
	}
	if _, _, err := b.Read(ctx); err != nil {
		t.Fatalf("b sync-init: %v", err)
	}

	payload := []byte("opaque-awareness-state")
	if err := a.Write(ctx, websocket.MessageBinary, encodeFrame(tagAwareness, payload)); err != nil {
		t.Fatalf("a write awareness: %v", err)
	}

	typ, got, err := b.Read(ctx)
	if err != nil {
		t.Fatalf("b read: %v", err)
	}
	if typ != websocket.MessageBinary || len(got) < 1 || got[0] != tagAwareness {
		t.Fatalf("b got tag=%x typ=%d, want awareness", got, typ)
	}
	if !bytes.Equal(got[1:], payload) {
		t.Fatalf("b payload = %x, want %x", got[1:], payload)
	}

	// Awareness must NOT be persisted to page_yjs_updates / page_yjs_snapshots.
	// Give the server a moment in case a misbehaving handler queued a write.
	time.Sleep(50 * time.Millisecond)
	var nUpd, nSnap int
	if err := d.QueryRow(`SELECT COUNT(*) FROM page_yjs_updates WHERE page_id = $1`, pageID).Scan(&nUpd); err != nil {
		t.Fatalf("count updates: %v", err)
	}
	if nUpd != 0 {
		t.Fatalf("awareness frame leaked into page_yjs_updates (%d rows)", nUpd)
	}
	if err := d.QueryRow(`SELECT COUNT(*) FROM page_yjs_snapshots WHERE page_id = $1`, pageID).Scan(&nSnap); err != nil {
		t.Fatalf("count snapshots: %v", err)
	}
	if nSnap != 0 {
		t.Fatalf("awareness frame leaked into page_yjs_snapshots (%d rows)", nSnap)
	}

	// Sender (peer A) must NOT see its own awareness echoed. Send a regular
	// update from B and expect A to receive THAT next, with no awareness
	// frame in between.
	if err := b.Write(ctx, websocket.MessageBinary, encodeFrame(tagUpdate, []byte("ping"))); err != nil {
		t.Fatalf("b write update: %v", err)
	}
	typ, got, err = a.Read(ctx)
	if err != nil {
		t.Fatalf("a read: %v", err)
	}
	if typ != websocket.MessageBinary || len(got) < 1 || got[0] != tagUpdate {
		t.Fatalf("a got tag=%x, want update (awareness must not have echoed)", got)
	}
}

// TestIntegration_WSPage_UnknownTagIgnored: forward-compat — an unknown tag
// is silently dropped, the conn stays open, and a subsequent valid update
// still round-trips.
func TestIntegration_WSPage_UnknownTagIgnored(t *testing.T) {
	ts, d := newWSWiredServer(t)
	owner := seedUser(t, d, "owner", "ownerpw1", false)
	editor := seedUser(t, d, "editor", "editorpw1", false)
	space := seedSpace(t, d, "S", "s", owner)
	seedMember(t, d, space, editor, roleEditor)
	pageID := seedPage(t, d, space, "P")

	ownerC := loginClient(t, ts, "owner", "ownerpw1")
	editorC := loginClient(t, ts, "editor", "editorpw1")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, _, err := websocket.Dial(ctx, wsURLFor(ts, pageID),
		&websocket.DialOptions{HTTPClient: ownerC})
	if err != nil {
		t.Fatalf("dial a: %v", err)
	}
	defer a.CloseNow()
	b, _, err := websocket.Dial(ctx, wsURLFor(ts, pageID),
		&websocket.DialOptions{HTTPClient: editorC})
	if err != nil {
		t.Fatalf("dial b: %v", err)
	}
	defer b.CloseNow()

	if _, _, err := a.Read(ctx); err != nil {
		t.Fatalf("a sync-init: %v", err)
	}
	if _, _, err := b.Read(ctx); err != nil {
		t.Fatalf("b sync-init: %v", err)
	}

	if err := a.Write(ctx, websocket.MessageBinary, []byte{0x99, 0xde, 0xad, 0xbe, 0xef}); err != nil {
		t.Fatalf("a write unknown: %v", err)
	}
	// Subsequent legitimate update must still flow A→B.
	if err := a.Write(ctx, websocket.MessageBinary, encodeFrame(tagUpdate, []byte("after-unknown"))); err != nil {
		t.Fatalf("a write update: %v", err)
	}
	typ, got, err := b.Read(ctx)
	if err != nil {
		t.Fatalf("b read: %v", err)
	}
	if typ != websocket.MessageBinary || len(got) < 1 || got[0] != tagUpdate {
		t.Fatalf("b got tag=%x, want update (unknown tag should be silently dropped)", got)
	}
	if !bytes.Equal(got[1:], []byte("after-unknown")) {
		t.Fatalf("b payload = %x, want %q", got[1:], "after-unknown")
	}

	// And the unknown tag itself never landed in storage.
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM page_yjs_updates WHERE page_id = $1`, pageID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("rows after one valid update + one unknown tag = %d, want 1", n)
	}
}

// --- helpers ----------------------------------------------------------------

// decodeSyncInit reverses encodeSyncInit. Test-only — production never
// decodes sync-init server-side.
func decodeSyncInit(buf []byte) (snapshot []byte, updates [][]byte, err error) {
	if len(buf) < 1 || buf[0] != tagSyncInit {
		return nil, nil, errors.New("not sync-init")
	}
	p := 1
	if len(buf) < p+4 {
		return nil, nil, errors.New("truncated snapLen")
	}
	snapLen := binary.BigEndian.Uint32(buf[p : p+4])
	p += 4
	if len(buf) < p+int(snapLen) {
		return nil, nil, errors.New("truncated snapshot")
	}
	if snapLen > 0 {
		snapshot = append([]byte(nil), buf[p:p+int(snapLen)]...)
	}
	p += int(snapLen)
	if len(buf) < p+4 {
		return nil, nil, errors.New("truncated nUpdates")
	}
	n := binary.BigEndian.Uint32(buf[p : p+4])
	p += 4
	updates = make([][]byte, 0, n)
	for i := uint32(0); i < n; i++ {
		if len(buf) < p+4 {
			return nil, nil, errors.New("truncated update len")
		}
		ul := binary.BigEndian.Uint32(buf[p : p+4])
		p += 4
		if len(buf) < p+int(ul) {
			return nil, nil, errors.New("truncated update")
		}
		updates = append(updates, append([]byte(nil), buf[p:p+int(ul)]...))
		p += int(ul)
	}
	return snapshot, updates, nil
}

// insertUpdate is a thin direct-insert helper for unit tests that need
// preexisting Yjs update rows. Production callers always go through
// room.appendUpdate.
func insertUpdate(t *testing.T, d *sql.DB, pageID, seq int64, blob []byte) {
	t.Helper()
	if _, err := d.ExecContext(context.Background(),
		`INSERT INTO page_yjs_updates(page_id, seq, payload) VALUES ($1, $2, $3)`,
		pageID, seq, blob); err != nil {
		t.Fatalf("insert update seq=%d: %v", seq, err)
	}
}

// insertSnapshot is a direct-insert helper for unit tests.
func insertSnapshot(t *testing.T, d *sql.DB, pageID, seq int64, state []byte) {
	t.Helper()
	if _, err := d.ExecContext(context.Background(),
		`INSERT INTO page_yjs_snapshots(page_id, seq, state) VALUES ($1, $2, $3)`,
		pageID, seq, state); err != nil {
		t.Fatalf("insert snapshot seq=%d: %v", seq, err)
	}
}

// wsURLFor converts an httptest.Server URL into the ws:// upgrade target.
func wsURLFor(ts *httptest.Server, pageID int64) string {
	return strings.Replace(ts.URL, "http://", "ws://", 1) + fmt.Sprintf("/ws/pages/%d", pageID)
}
