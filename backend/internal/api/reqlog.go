package api

import (
	"bufio"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"
)

// requestLogger emits one structured access-log line per request (method, path,
// status, duration) AND records the Prometheus HTTP metrics — one place so the
// access log and the metrics can never disagree. There was no request/access
// log before — this is the first observability primitive. The /api/health probe
// is skipped to avoid spamming the log (and the metrics) with load-balancer
// /healthcheck noise.
//
// It sits OUTERMOST (around auth.Middleware) so it sees the final status. The
// mux is passed in only to resolve the matched route pattern (mux.Handler(r))
// for a low-cardinality metric label — per-id paths collapse to one series
// (see routePattern). The statusRecorder delegates Hijack/Flush so it doesn't
// break the Yjs websocket upgrade or any streaming response that passes through.
func requestLogger(mux *http.ServeMux, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		dur := time.Since(start)
		route := routePattern(mux, r)
		httpRequests.WithLabelValues(r.Method, route, strconv.Itoa(status)).Inc()
		httpDuration.WithLabelValues(r.Method, route).Observe(dur.Seconds())
		slog.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"dur_ms", dur.Milliseconds(),
		)
	})
}

// statusRecorder captures the response status code for the access log while
// transparently forwarding Hijack (websocket upgrades) and Flush (streaming) to
// the wrapped ResponseWriter.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := r.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("response writer does not support hijacking")
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
