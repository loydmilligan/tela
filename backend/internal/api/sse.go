package api

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// sseWriter frames Server-Sent Events onto an http.ResponseWriter, flushing each
// event so the client receives data (e.g. answer tokens) as it's produced rather
// than buffered to the end. Used by the streaming ask endpoint (RAGAskStream).
type sseWriter struct {
	w  http.ResponseWriter
	fl http.Flusher
}

// newSSEWriter sets the SSE response headers and returns a writer, or ok=false
// when the ResponseWriter can't stream (no http.Flusher). On success the 200 +
// headers are already written, so callers must signal further failures as SSE
// `error` events, not writeError.
func newSSEWriter(w http.ResponseWriter) (*sseWriter, bool) {
	fl, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	// Belt-and-suspenders against a buffering proxy (Caddy auto-flushes
	// text/event-stream, but an nginx-shaped intermediary needs the hint).
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fl.Flush()
	return &sseWriter{w: w, fl: fl}, true
}

// event writes one named SSE event with a JSON data payload and flushes. A write
// error means the client disconnected — callers should stop generating.
func (s *sseWriter) event(name string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, payload); err != nil {
		return err
	}
	s.fl.Flush()
	return nil
}
