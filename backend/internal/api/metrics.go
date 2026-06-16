package api

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// metricsRegistry is the process-wide Prometheus registry. It is NOT the
// default global registry: a private registry keeps the exported surface to
// exactly what we register here (HTTP request metrics + the standard Go runtime
// and process collectors), and avoids picking up stray metrics any imported
// dependency might register on the global default.
var (
	metricsRegistry = prometheus.NewRegistry()

	// httpRequests counts HTTP requests by method, the matched route pattern,
	// and response status. The route pattern (e.g. "GET /api/pages/{id}") is
	// used instead of the raw path so per-id paths collapse to one low-
	// cardinality series; unmatched paths fall back to "other" (see
	// routePattern).
	httpRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tela_http_requests_total",
			Help: "Total HTTP requests by method, route pattern, and status code.",
		},
		[]string{"method", "route", "status"},
	)

	// httpDuration observes request latency in seconds, labelled the same way
	// (minus status) so it stays low-cardinality.
	httpDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "tela_http_request_duration_seconds",
			Help:    "HTTP request latency in seconds by method and route pattern.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "route"},
	)

	// clientErrors counts browser-side error reports beaconed to
	// /api/client-errors, by kind (error | unhandledrejection | react | collab).
	// The kind label is bounded by the client + the clientErrMaxKind truncation,
	// so cardinality stays low; this is the alertable signal for "users are
	// hitting crashes we never see server-side".
	clientErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tela_client_errors_total",
			Help: "Total browser-side error reports by kind.",
		},
		[]string{"kind"},
	)
)

func init() {
	metricsRegistry.MustRegister(
		httpRequests,
		httpDuration,
		clientErrors,
		// Go runtime + process collectors (goroutines, GC, memory, open FDs,
		// CPU, etc.).
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
}

// metricsHandler serves the Prometheus exposition format from our private
// registry. It is gated to instance-admins (session cookie OR an admin-scoped
// PAT as a bearer token) by requireInstanceAdmin — auth.Middleware has already
// resolved the caller onto the request context by the time this runs, since
// /metrics is NOT on auth.IsPublicPath. A Prometheus scraper authenticates with
//
//	Authorization: Bearer tela_pat_<admin-scoped-key>
func (s *Server) metricsHandler() http.Handler {
	promh := promhttp.HandlerFor(metricsRegistry, promhttp.HandlerOpts{})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requireInstanceAdmin(w, r); !ok {
			return
		}
		promh.ServeHTTP(w, r)
	})
}

// routePattern returns a low-cardinality label for the request: the Go 1.22+
// ServeMux pattern that matched (e.g. "GET /api/pages/{id}"), so per-id paths
// collapse to a single series. mux.Handler(r) resolves the matched pattern
// without dispatching. Unmatched requests (404) report "other" so a path-
// scanning probe can't blow up label cardinality.
func routePattern(mux *http.ServeMux, r *http.Request) string {
	if _, pattern := mux.Handler(r); pattern != "" {
		return pattern
	}
	return "other"
}
