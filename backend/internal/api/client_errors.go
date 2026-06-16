package api

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// client_errors.go — POST /api/client-errors. The frontend's global error
// reporter (window.onerror / unhandledrejection / the top-level React error
// boundary) beacons here so a crash in a real user's browser stops being
// invisible: each report lands as a `client.error` row in the unified events
// feed (admin Events screen) AND bumps a Prometheus counter for alerting. The
// motivating case was live-collab breaking client-side with zero server signal.
//
// Authed (session OR bearer) — NOT public: this writes to the events table, so
// keeping it behind auth gives reliable actor attribution and avoids opening an
// unauthenticated write hole. Pre-login crashes (login screen) aren't captured;
// that's a deliberate trade for not exposing an anonymous event-writer.

const (
	// Per-client throttle: a render loop or a retrying network layer can fire the
	// same error hundreds of times a second. The client already de-dupes, but the
	// server caps independently so a buggy/hostile tab can't flood the table.
	clientErrorRateWindow = 1 * time.Minute
	clientErrorRateLimit  = 30

	// Field caps — truncate rather than reject so we still capture a usable
	// report from an over-long stack instead of dropping it on the floor.
	clientErrMaxMessage   = 1000
	clientErrMaxStack     = 8000
	clientErrMaxURL       = 1000
	clientErrMaxKind      = 32
	clientErrMaxComponent = 200
	clientErrMaxBody      = 64 * 1024
)

// clientErrorRequest is the beacon payload. All fields optional except message;
// kind buckets the report (error | unhandledrejection | react | collab | …) and
// becomes the Prometheus label, so the client must keep it low-cardinality.
type clientErrorRequest struct {
	Kind      string `json:"kind"`
	Message   string `json:"message"`
	Stack     string `json:"stack"`
	URL       string `json:"url"`
	Component string `json:"component"`
	PageID    *int64 `json:"page_id"`
}

// CreateClientError records a browser-side error report. Best-effort and always
// cheap to the caller: validation failures and the throttle return small status
// codes, success is 204 with no body (it's a beacon, the client ignores the
// response).
func (s *Server) CreateClientError(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}

	// Per-user throttle (keyed on the authenticated actor, with the IP as a
	// fallback namespace) so one tab in an error loop can't fill the feed.
	key := strconv.FormatInt(u.ID, 10) + "@" + clientIPForRateLimit(r)
	if allowed, _ := s.clientErrorLimiter.allow("client_error", key); !allowed {
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, clientErrMaxBody)
	var req clientErrorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not parse request body")
		return
	}

	message := strings.TrimSpace(req.Message)
	if message == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "message is required")
		return
	}

	kind := truncate(strings.TrimSpace(req.Kind), clientErrMaxKind)
	if kind == "" {
		kind = "error"
	}
	message = truncate(message, clientErrMaxMessage)
	stack := truncate(strings.TrimSpace(req.Stack), clientErrMaxStack)
	url := truncate(strings.TrimSpace(req.URL), clientErrMaxURL)
	component := truncate(strings.TrimSpace(req.Component), clientErrMaxComponent)

	clientErrors.WithLabelValues(kind).Inc()

	// Compose a readable detail blob: a one-line summary the feed shows inline,
	// then the location and stack for the admin who opens it to debug.
	var b strings.Builder
	fmt.Fprintf(&b, "[%s] %s", kind, message)
	if component != "" {
		fmt.Fprintf(&b, "\ncomponent: %s", component)
	}
	if url != "" {
		fmt.Fprintf(&b, "\nurl: %s", url)
	}
	if stack != "" {
		fmt.Fprintf(&b, "\n%s", stack)
	}

	e := eventInput{
		Type:        evtClientError,
		Detail:      b.String(),
		Fingerprint: clientErrorFingerprint(kind, message, stack),
	}
	if req.PageID != nil {
		e.TargetKind = "page"
		e.TargetID = req.PageID
	}
	s.recordRequestEvent(r, e)

	w.WriteHeader(http.StatusNoContent)
}

// fpNoise matches the variable bits of an error message/stack that would
// otherwise split one logical error into many groups: long hex/uuid runs and
// any digit run (ids, line:col numbers, timestamps, ports). Both collapse to a
// placeholder before hashing.
var fpHex = regexp.MustCompile(`[0-9a-fA-F]{8,}`)
var fpNum = regexp.MustCompile(`\d+`)

// clientErrorFingerprint is the stable grouping key for the Issues view: a hash
// of the kind, the normalized message, and the first stack frame. Normalizing
// out ids/numbers means "page 123 not found" and "page 456 not found" — or the
// same crash at slightly different line numbers across builds — fold into one
// issue instead of a long tail of near-duplicates.
func clientErrorFingerprint(kind, message, stack string) string {
	firstFrame := ""
	if lines := strings.Split(stack, "\n"); len(lines) > 1 {
		firstFrame = strings.TrimSpace(lines[1]) // [0] is the message echo
	}
	norm := func(s string) string {
		s = fpHex.ReplaceAllString(s, "#")
		s = fpNum.ReplaceAllString(s, "#")
		return strings.TrimSpace(s)
	}
	sum := sha1.Sum([]byte(kind + "\n" + norm(message) + "\n" + norm(firstFrame)))
	return hex.EncodeToString(sum[:])
}

// truncate clamps s to at most n bytes, appending an ellipsis marker when it had
// to cut so a reader knows the report was longer than what's stored.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…(truncated)"
}
