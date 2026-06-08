package api

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// M7.1 LiveCollab — /ws/pages/{id} dumb-relay + Yjs persistence.
//
// The server has no understanding of Yjs CRDT internals. It treats every
// inbound binary frame as an opaque blob, appends it to page_yjs_updates with
// a monotonic per-page seq, and rebroadcasts it to every other peer in the
// same page-room. A connected peer is asked for a fresh full-state snapshot
// every ~snapshotEvery updates; once the snapshot arrives, pre-snapshot
// updates linger for snapshotGCGrace and are then GC'd.
//
// Storage rule (locked in plan): pages.body stays canonical markdown forever.
// page_yjs_updates + page_yjs_snapshots are an overlay that can be dropped
// without data loss.
//
// Wire protocol (1-byte tag + payload, all multi-byte ints big-endian):
//
//	0x01  update           peer↔server : raw Yjs update blob
//	0x02  snapshot-request server→peer : no payload
//	0x03  snapshot-response peer→server: full Y.Doc state blob
//	0x04  sync-init        server→peer : on connect, packs latest snapshot +
//	                                      tail of updates so the peer can
//	                                      reconstruct the doc with one frame.
//	                                      layout:
//	                                        u32 snapLen | snapLen bytes
//	                                        u32 nUpd    | (u32 len + bytes) * n
//	0x05  awareness        peer↔server : ephemeral y-protocols/awareness blob.
//	                                      Server treats the payload as opaque
//	                                      and rebroadcasts to every OTHER peer
//	                                      in the room. Never persisted —
//	                                      awareness state lives only as long
//	                                      as the ws sessions that produced it.
//
// Any unknown tag is ignored — keeps the protocol forward-extensible.

const (
	tagUpdate       byte = 0x01
	tagSnapshotReq  byte = 0x02
	tagSnapshotResp byte = 0x03
	tagSyncInit     byte = 0x04
	tagAwareness    byte = 0x05
	// tagReset server→peer: the page body was rewritten out-of-band (an agent
	// MCP write); the peer must drop its stale Y.Doc and reload from pages.body.
	tagReset byte = 0x06
)

// wsReadLimit bounds a single inbound ws message. Yjs full-state sync vectors
// can exceed the default 32 KiB cap (and 64 KiB ws-frame boundary). 16 MiB is
// well above realistic page sizes and below memory-pressure risk.
const wsReadLimit = 16 * 1024 * 1024

// wsPingInterval keeps the ws alive across Cloudflare's 100s idle-drop. Each
// peer runs a per-connection ticker that calls Conn.Ping; if Ping fails the
// connection is torn down and the read loop unblocks.
const wsPingInterval = 60 * time.Second

// pingWriteTimeout caps how long a single Ping waits before declaring the
// peer dead.
const pingWriteTimeout = 10 * time.Second

// snapshotEvery is the number of updates persisted to a single page before
// the server asks a connected peer for a fresh snapshot. Variable rather than
// const so tests can shorten the interval.
var snapshotEvery int64 = 100

// snapshotGCGrace is how long pre-snapshot updates linger after a snapshot
// persists, before being GC'd. Variable for tests.
var snapshotGCGrace = 15 * time.Minute

// roomRegistry is the in-memory map of active page rooms. Lifecycle is
// process-local; multi-replica deployments are out of v0 scope.
type roomRegistry struct {
	mu    sync.Mutex
	rooms map[int64]*room
}

func newRoomRegistry() *roomRegistry {
	return &roomRegistry{rooms: map[int64]*room{}}
}

// peer wraps one ws connection in a room. The conn pointer doubles as the
// peer's identity inside room.peers.
type peer struct {
	conn *websocket.Conn
}

// room is the in-memory live state for a single page-edit session.
type room struct {
	pageID int64

	mu              sync.Mutex
	peers           map[*peer]struct{}
	initialized     bool
	nextSeq         int64
	lastSnapSeq     int64
	snapInFlight    bool
	pendingSnapSeq  int64
	snapRequestedOf *peer // peer holding the outstanding snapshot-request
}

// acquire registers p in the room for pageID, creating the room if needed.
// Lock order: registry.mu → room.mu. release() follows the same order.
func (rr *roomRegistry) acquire(pageID int64, p *peer) *room {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	rm, ok := rr.rooms[pageID]
	if !ok {
		rm = &room{pageID: pageID, peers: map[*peer]struct{}{}}
		rr.rooms[pageID] = rm
	}
	rm.mu.Lock()
	rm.peers[p] = struct{}{}
	rm.mu.Unlock()
	return rm
}

// release removes p from rm. If rm becomes empty and the registry still
// holds this room instance, drop it.
func (rr *roomRegistry) release(rm *room, p *peer) {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	rm.mu.Lock()
	delete(rm.peers, p)
	// Drop any outstanding snapshot-request that was tied to this peer —
	// otherwise snapInFlight stays stuck and future snapshots never fire.
	if rm.snapRequestedOf == p {
		rm.snapRequestedOf = nil
		rm.snapInFlight = false
	}
	empty := len(rm.peers) == 0
	rm.mu.Unlock()
	if !empty {
		return
	}
	if cur, ok := rr.rooms[rm.pageID]; ok && cur == rm {
		delete(rr.rooms, rm.pageID)
	}
}

// resetPage drops a page's Yjs overlay (updates + snapshots) and tears down any
// live room, so the next editor open re-seeds from pages.body. Called when an
// agent rewrites pages.body out-of-band (MCP update_page): the editor's
// in-memory Y.Doc would otherwise mask the new body indefinitely and clobber it
// on the next human save. DB-wins, per the agent-backend sync design. Connected
// peers are sent a reset frame (reload) and closed FIRST (stops further appends
// at the now-stale seq), then the overlay tables are cleared.
func (rr *roomRegistry) resetPage(ctx context.Context, db *sql.DB, pageID int64) error {
	rr.mu.Lock()
	rm, active := rr.rooms[pageID]
	if active {
		delete(rr.rooms, pageID)
	}
	rr.mu.Unlock()

	if active {
		for _, p := range rm.peerList() {
			_ = p.conn.Write(ctx, websocket.MessageBinary, []byte{tagReset})
			_ = p.conn.Close(websocket.StatusNormalClosure, "page reset")
		}
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM page_yjs_updates WHERE page_id = $1`, pageID); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM page_yjs_snapshots WHERE page_id = $1`, pageID); err != nil {
		return err
	}
	return nil
}

// initFromDB lazy-loads nextSeq + lastSnapSeq from the database on first
// peer joining. No-op on subsequent calls.
func (r *room) initFromDB(ctx context.Context, db *sql.DB) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.initialized {
		return nil
	}
	var maxUpd sql.NullInt64
	if err := db.QueryRowContext(ctx,
		`SELECT MAX(seq) FROM page_yjs_updates WHERE page_id = $1`,
		r.pageID).Scan(&maxUpd); err != nil {
		return err
	}
	if maxUpd.Valid {
		r.nextSeq = maxUpd.Int64 + 1
	} else {
		r.nextSeq = 1
	}
	var maxSnap sql.NullInt64
	if err := db.QueryRowContext(ctx,
		`SELECT MAX(seq) FROM page_yjs_snapshots WHERE page_id = $1`,
		r.pageID).Scan(&maxSnap); err != nil {
		return err
	}
	if maxSnap.Valid {
		r.lastSnapSeq = maxSnap.Int64
	}
	r.initialized = true
	return nil
}

// peerList returns a snapshot of the current peer set so the caller can
// iterate without holding r.mu through a slow ws write.
func (r *room) peerList() []*peer {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*peer, 0, len(r.peers))
	for p := range r.peers {
		out = append(out, p)
	}
	return out
}

// appendUpdate persists blob with the next per-page seq. Returns the assigned
// seq and a flag indicating whether the caller should now ask a peer for a
// fresh snapshot (threshold crossed and no request already in flight).
func (r *room) appendUpdate(ctx context.Context, db *sql.DB, blob []byte) (int64, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	seq := r.nextSeq
	if _, err := db.ExecContext(ctx,
		`INSERT INTO page_yjs_updates(page_id, seq, payload) VALUES ($1, $2, $3)`,
		r.pageID, seq, blob); err != nil {
		return 0, false, err
	}
	r.nextSeq++
	shouldRequest := !r.snapInFlight && (seq-r.lastSnapSeq) >= snapshotEvery
	return seq, shouldRequest, nil
}

// markSnapshotInFlight sets the snapshot-request bookkeeping. Caller must
// hold no locks; this acquires r.mu.
func (r *room) markSnapshotInFlight(target *peer, seq int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snapInFlight = true
	r.pendingSnapSeq = seq
	r.snapRequestedOf = target
}

// applySnapshot persists a peer-supplied full-state blob at the seq the
// in-flight request was tagged with, then schedules a GC pass to drop the
// now-redundant pre-snapshot updates after snapshotGCGrace.
func (r *room) applySnapshot(ctx context.Context, db *sql.DB, state []byte) error {
	r.mu.Lock()
	if !r.snapInFlight {
		r.mu.Unlock()
		return nil
	}
	seq := r.pendingSnapSeq
	pageID := r.pageID
	if _, err := db.ExecContext(ctx,
		`INSERT INTO page_yjs_snapshots(page_id, seq, state) VALUES ($1, $2, $3)`,
		pageID, seq, state); err != nil {
		r.snapInFlight = false
		r.snapRequestedOf = nil
		r.mu.Unlock()
		return err
	}
	r.lastSnapSeq = seq
	r.snapInFlight = false
	r.snapRequestedOf = nil
	r.mu.Unlock()

	go gcPreSnapshotUpdates(db, pageID, seq)
	return nil
}

// gcPreSnapshotUpdates sleeps for snapshotGCGrace then deletes every update
// row strictly older than seq. The row at seq itself is retained as a safety
// buffer — see the doc comment on the join handshake.
func gcPreSnapshotUpdates(db *sql.DB, pageID, seq int64) {
	time.Sleep(snapshotGCGrace)
	if _, err := db.ExecContext(context.Background(),
		`DELETE FROM page_yjs_updates WHERE page_id = $1 AND seq < $2`,
		pageID, seq); err != nil {
		slog.Error("ws: GC pre-snapshot updates failed", "page_id", pageID, "seq_lt", seq, "err", err)
	}
}

// encodeFrame wraps payload with the single tag byte; the result is what we
// pump through the ws as a Binary message.
func encodeFrame(tag byte, payload []byte) []byte {
	out := make([]byte, 1+len(payload))
	out[0] = tag
	copy(out[1:], payload)
	return out
}

// encodeSyncInit packs a snapshot + ordered tail of updates into a single
// frame the client applies in one shot.
func encodeSyncInit(snapshot []byte, updates [][]byte) []byte {
	size := 1 + 4 + len(snapshot) + 4
	for _, u := range updates {
		size += 4 + len(u)
	}
	buf := make([]byte, 0, size)
	buf = append(buf, tagSyncInit)
	var u32 [4]byte
	binary.BigEndian.PutUint32(u32[:], uint32(len(snapshot)))
	buf = append(buf, u32[:]...)
	buf = append(buf, snapshot...)
	binary.BigEndian.PutUint32(u32[:], uint32(len(updates)))
	buf = append(buf, u32[:]...)
	for _, u := range updates {
		binary.BigEndian.PutUint32(u32[:], uint32(len(u)))
		buf = append(buf, u32[:]...)
		buf = append(buf, u...)
	}
	return buf
}

// GetPageYjsState — GET /api/pages/{id}/yjs. Returns the page's persisted Yjs
// state (latest snapshot + tail updates) in the sync-init layout WITHOUT the
// leading tag byte, so the client can feed it straight to decodeSyncInit. The
// editor applies this on mount to paint content instantly from REST instead of
// waiting for the WS handshake; the WS sync-init then re-delivers the same
// state, which is a no-op (Yjs CRDT update application is idempotent). Member
// of the page's space required; a missing page collapses to 403 like GetPage.
func (s *Server) GetPageYjsState(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	page, err := selectPageByID(r.Context(), s.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusForbidden, "forbidden", "not a member")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup page failed")
		return
	}
	if _, ok := s.requireMembership(w, r, page.SpaceID); !ok {
		return
	}

	ctx := r.Context()
	var (
		snapSeq  int64
		snapshot []byte
	)
	err = s.DB.QueryRowContext(ctx,
		`SELECT seq, state FROM page_yjs_snapshots WHERE page_id = $1 ORDER BY seq DESC LIMIT 1`,
		id).Scan(&snapSeq, &snapshot)
	if errors.Is(err, sql.ErrNoRows) {
		snapSeq, snapshot = 0, nil
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "load snapshot failed")
		return
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT payload FROM page_yjs_updates
		 WHERE page_id = $1 AND seq >= $2
		 ORDER BY seq ASC`, id, snapSeq)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "load updates failed")
		return
	}
	defer rows.Close()
	var updates [][]byte
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan update failed")
			return
		}
		updates = append(updates, b)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate updates failed")
		return
	}

	// encodeSyncInit prefixes the tag byte; strip it so the body is exactly a
	// decodeSyncInit payload.
	frame := encodeSyncInit(snapshot, updates)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(frame[1:])
}

// WSPage is the HTTP handler behind GET /ws/pages/{id}. It authenticates +
// authorises before upgrading, then hands off to the long-lived per-conn
// loop in handleWSConn.
//
// Auth: cookie validated by auth.Middleware. Path: editor or owner only —
// viewers intentionally have NO live session in v1 (collab is edit-only;
// read-only viewers continue to refetch via GET /api/pages/{id}).
func (s *Server) WSPage(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}

	page, err := selectPageByID(r.Context(), s.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		// Match GET /api/pages/{id}: collapse missing→403 so page ids can't
		// be enumerated cross-space.
		writeError(w, http.StatusForbidden, "forbidden", "not a member")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch page failed")
		return
	}
	if !enforceAPIKeySpaceScope(w, r, page.SpaceID) {
		return
	}
	role, err := spaceRole(r.Context(), s.DB, u.ID, page.SpaceID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusForbidden, "forbidden", "not a member")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup membership failed")
		return
	}
	if !canEdit(role) {
		writeError(w, http.StatusForbidden, "viewer_no_write", "editor or owner role required")
		return
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		// Accept already wrote the error response.
		return
	}
	conn.SetReadLimit(wsReadLimit)
	// Replace the request's context (invalid after Accept per coder/websocket
	// docs) with a fresh one tied to the conn lifecycle.
	s.handleWSConn(conn, id)
}

// handleWSConn owns the lifecycle of a single ws connection: room
// registration, sync-init, ping loop, read loop, cleanup.
func (s *Server) handleWSConn(conn *websocket.Conn, pageID int64) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer conn.CloseNow()

	p := &peer{conn: conn}
	rm := s.rooms.acquire(pageID, p)
	defer s.rooms.release(rm, p)

	if err := rm.initFromDB(ctx, s.DB); err != nil {
		slog.Error("ws: init room", "page_id", pageID, "err", err)
		return
	}
	if err := s.sendSyncInit(ctx, conn, rm); err != nil {
		slog.Error("ws: sync-init", "page_id", pageID, "err", err)
		return
	}

	go pingLoop(ctx, cancel, conn)

	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageBinary || len(data) < 1 {
			continue
		}
		s.handleWSFrame(ctx, rm, p, data)
	}
}

// pingLoop pings the peer every wsPingInterval and cancels the connection's
// context on the first failure — needed because Cloudflare drops idle ws
// after 100s (M7.0 finding, #61).
func pingLoop(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn) {
	t := time.NewTicker(wsPingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pctx, pcancel := context.WithTimeout(ctx, pingWriteTimeout)
			err := conn.Ping(pctx)
			pcancel()
			if err != nil {
				cancel()
				return
			}
		}
	}
}

// handleWSFrame dispatches a single inbound frame to the protocol handler
// for its tag byte.
func (s *Server) handleWSFrame(ctx context.Context, rm *room, sender *peer, frame []byte) {
	tag := frame[0]
	payload := frame[1:]
	switch tag {
	case tagUpdate:
		seq, shouldRequest, err := rm.appendUpdate(ctx, s.DB, payload)
		if err != nil {
			slog.Error("ws: append update", "page_id", rm.pageID, "err", err)
			return
		}
		broadcastToOthers(ctx, rm, sender, encodeFrame(tagUpdate, payload))
		if shouldRequest {
			rm.markSnapshotInFlight(sender, seq)
			_ = sender.conn.Write(ctx, websocket.MessageBinary, []byte{tagSnapshotReq})
		}
	case tagSnapshotResp:
		if err := rm.applySnapshot(ctx, s.DB, payload); err != nil {
			slog.Error("ws: apply snapshot", "page_id", rm.pageID, "err", err)
		}
	case tagAwareness:
		// Awareness is ephemeral: rebroadcast to other peers, never persist.
		// The server is opaque to y-protocols/awareness payload format — peers
		// decode it on receipt via applyAwarenessUpdate.
		broadcastToOthers(ctx, rm, sender, encodeFrame(tagAwareness, payload))
	default:
		// Unknown / future tag: ignore so the protocol stays forward-extensible.
	}
}

// broadcastToOthers writes frame to every peer in rm except sender. Best-effort:
// a write that fails because a peer disconnected mid-broadcast is silently
// dropped — that peer's own read loop will surface the close and trigger the
// deferred release().
func broadcastToOthers(ctx context.Context, rm *room, sender *peer, frame []byte) {
	for _, other := range rm.peerList() {
		if other == sender {
			continue
		}
		_ = other.conn.Write(ctx, websocket.MessageBinary, frame)
	}
}

// sendSyncInit packages the latest snapshot (if any) plus every update with
// seq >= lastSnapSeq into a single tagSyncInit frame. Replaying seq=lastSnapSeq
// is harmless — Yjs CRDT update application is idempotent — and acts as a
// safety buffer against the window where a snapshot is taken before its
// triggering update has been confirmed applied on the snapshotting peer.
func (s *Server) sendSyncInit(ctx context.Context, conn *websocket.Conn, rm *room) error {
	rm.mu.Lock()
	snapSeq := rm.lastSnapSeq
	pageID := rm.pageID
	rm.mu.Unlock()

	var snapshot []byte
	if snapSeq > 0 {
		err := s.DB.QueryRowContext(ctx,
			`SELECT state FROM page_yjs_snapshots WHERE page_id = $1 AND seq = $2`,
			pageID, snapSeq).Scan(&snapshot)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT payload FROM page_yjs_updates
		 WHERE page_id = $1 AND seq >= $2
		 ORDER BY seq ASC`, pageID, snapSeq)
	if err != nil {
		return err
	}
	defer rows.Close()
	var updates [][]byte
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return err
		}
		updates = append(updates, b)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageBinary, encodeSyncInit(snapshot, updates))
}
