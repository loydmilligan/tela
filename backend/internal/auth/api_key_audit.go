package auth

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
)

// auditBufferSize is the depth of the unbuffered insert channel feeding the
// audit-writer goroutine. Sized so a brief INSERT stall (DB contention, GC
// pause) doesn't immediately spill events, while still bounding the worst
// case at 256 outstanding rows. Drop-on-full keeps the request path
// non-blocking — losing a handful of audit entries during a stampede is
// strictly preferable to backpressure on every bearer-authed request.
const auditBufferSize = 256

// AuditEvent is the per-request audit-log row queued by the bearer-auth path
// in Middleware. Pure value type so the goroutine doesn't share request-state
// with the caller. Method + Path + StatusCode are captured verbatim from the
// final response — including 4xx/5xx from scope refusals or downstream
// handler errors — so the trail covers blocked attempts and failures.
type AuditEvent struct {
	APIKeyID   int64
	Method     string
	Path       string
	StatusCode int
}

// AuditWriter drains AuditEvents into the api_key_audit table via a single
// background goroutine. The request path Submits events on a buffered channel
// (drop-on-full) so a slow DB write can never block a bearer request.
//
// Lifecycle: created by NewAuditWriter, stopped by Close (drains in-flight
// events first). A nil receiver is legal — every method short-circuits — so
// test paths that don't care about audit can pass nil to Middleware without
// changing the call shape.
type AuditWriter struct {
	db      *sql.DB
	ch      chan auditMsg
	wg      sync.WaitGroup
	dropped atomic.Uint64
	closed  atomic.Bool
}

// auditMsg is the channel payload. A non-nil barrier signals a flush
// checkpoint — the worker closes it after every preceding event has been
// written. Callers use Flush to wait on a barrier.
type auditMsg struct {
	ev      AuditEvent
	barrier chan struct{}
}

// NewAuditWriter creates an AuditWriter bound to d and launches its single
// drain goroutine. Always pair with a Close call to drain the buffer on
// shutdown — without it, in-flight events queued at process exit are lost.
func NewAuditWriter(d *sql.DB) *AuditWriter {
	aw := &AuditWriter{
		db: d,
		ch: make(chan auditMsg, auditBufferSize),
	}
	aw.wg.Add(1)
	go aw.run()
	return aw
}

// Submit enqueues ev for asynchronous insertion. Never blocks: when the
// channel is full the event is dropped and a counter advances. A best-effort
// log line fires every 64 drops so a misconfigured deploy surfaces the
// pressure without spamming on every loss. Safe to call on a nil receiver.
func (aw *AuditWriter) Submit(ev AuditEvent) {
	if aw == nil || aw.closed.Load() {
		return
	}
	select {
	case aw.ch <- auditMsg{ev: ev}:
	default:
		if n := aw.dropped.Add(1); n%64 == 1 {
			slog.Warn("auth: api_key audit buffer full, dropping events", "dropped_total", n)
		}
	}
}

// Flush blocks until every event submitted before this call has been
// written. Used by tests (to assert audit rows post-request) and by Close
// (to drain on shutdown). Sends a barrier through the same channel — because
// the worker is single-threaded, when the barrier is processed every prior
// event has already been INSERT'd. Safe to call on a nil receiver.
//
// Returns the total number of dropped events seen so far, as a debugging aid
// for tests that want to assert "no drops happened".
func (aw *AuditWriter) Flush() uint64 {
	if aw == nil {
		return 0
	}
	if aw.closed.Load() {
		return aw.dropped.Load()
	}
	barrier := make(chan struct{})
	aw.ch <- auditMsg{barrier: barrier}
	<-barrier
	return aw.dropped.Load()
}

// Close stops accepting new events, waits for the in-flight buffer to drain,
// and joins the worker. Idempotent + safe on a nil receiver. Subsequent
// Submits become no-ops.
func (aw *AuditWriter) Close() {
	if aw == nil || !aw.closed.CompareAndSwap(false, true) {
		return
	}
	close(aw.ch)
	aw.wg.Wait()
}

// auditResponseWriter is the http.ResponseWriter wrapper Middleware uses on
// the bearer-auth path so we can capture the final status code (including
// the implicit 200 when a handler writes a body without calling
// WriteHeader). Used only inside the audit-emit defer; downstream handlers
// see a normal ResponseWriter.
//
// Deliberately minimal: no Hijacker / Pusher forwarding. Bearer tokens are
// only used by headless clients (MCP server, CLIs) — never browsers — so
// the WS-upgrade path is unreachable here. Adding Hijacker would force the
// wrapper to mirror the underlying type, which is unnecessary complexity.
type auditResponseWriter struct {
	http.ResponseWriter
	code    int
	written bool
}

func newAuditResponseWriter(w http.ResponseWriter) *auditResponseWriter {
	return &auditResponseWriter{ResponseWriter: w, code: http.StatusOK}
}

func (w *auditResponseWriter) WriteHeader(code int) {
	if w.written {
		return
	}
	w.code = code
	w.written = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *auditResponseWriter) Write(b []byte) (int, error) {
	if !w.written {
		// http.ResponseWriter implicitly sends 200 on first Write — mirror
		// that here so the audit log doesn't show 0 for handlers that skip
		// WriteHeader (the common 200-body path).
		w.code = http.StatusOK
		w.written = true
	}
	return w.ResponseWriter.Write(b)
}

// Flush forwards to the underlying writer when it implements http.Flusher.
// Streaming handlers (none today, but cheap insurance) depend on this.
func (w *auditResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *auditResponseWriter) statusCode() int {
	return w.code
}

func (aw *AuditWriter) run() {
	defer aw.wg.Done()
	for msg := range aw.ch {
		if msg.barrier != nil {
			close(msg.barrier)
			continue
		}
		// Fresh background context: the audit write outlives the request
		// that produced it, identical to the last_used_at stamp in
		// LookupAPIKey. A failed insert is logged and dropped — never bring
		// down the worker on a transient DB error.
		if _, err := aw.db.ExecContext(context.Background(),
			`INSERT INTO api_key_audit (api_key_id, method, path, status_code)
			 VALUES ($1, $2, $3, $4)`,
			msg.ev.APIKeyID, msg.ev.Method, msg.ev.Path, msg.ev.StatusCode); err != nil {
			slog.Error("auth: api_key audit insert failed",
				"key_id", msg.ev.APIKeyID, "method", msg.ev.Method, "path", msg.ev.Path, "status", msg.ev.StatusCode, "err", err)
		}
	}
}
