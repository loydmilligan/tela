package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// streamServer is a tiny OpenAI-compatible streaming endpoint: it flushes each of
// the given frames, sleeping gap between them, so a test can shape a stream's
// timing (slow-but-alive, keepalive-only, or a dead stall).
func streamServer(t *testing.T, gap time.Duration, frames ...string) *OpenAIClient {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		fl.Flush()
		for _, f := range frames {
			time.Sleep(gap)
			fmt.Fprint(w, f)
			fl.Flush()
		}
	}))
	t.Cleanup(ts.Close)
	return NewOpenAIClient(ts.URL, "m", "", 0)
}

func token(s string) string { return `data: {"choices":[{"delta":{"content":"` + s + `"}}]}` + "\n\n" }

// A stream that keeps producing bytes well within the stall window completes and
// delivers every delta.
func TestCompleteStream_HealthyStreamSucceeds(t *testing.T) {
	c := streamServer(t, 20*time.Millisecond, token("Run "), token("make "), token("deploy."), "data: [DONE]\n\n")
	c.stall = 300 * time.Millisecond

	var got strings.Builder
	if err := c.CompleteStream(context.Background(), "", "q", func(tok string) error {
		got.WriteString(tok)
		return nil
	}); err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	if got.String() != "Run make deploy." {
		t.Fatalf("answer=%q want %q", got.String(), "Run make deploy.")
	}
}

// The core of the fix: the upstream's keepalive comments (sent during slow prompt
// processing) count as liveness, so a stream that emits ONLY keepalives for longer
// than the stall window — then a token — must NOT time out.
func TestCompleteStream_KeepalivesPreventStall(t *testing.T) {
	c := streamServer(t, 40*time.Millisecond,
		": keepalive\n\n", ": keepalive\n\n", ": keepalive\n\n", ": keepalive\n\n", // 160ms of comments
		token("answer"), "data: [DONE]\n\n")
	c.stall = 100 * time.Millisecond // shorter than the keepalive run

	var got strings.Builder
	if err := c.CompleteStream(context.Background(), "", "q", func(tok string) error {
		got.WriteString(tok)
		return nil
	}); err != nil {
		t.Fatalf("keepalives should keep the stream alive, got: %v", err)
	}
	if got.String() != "answer" {
		t.Fatalf("answer=%q want %q", got.String(), "answer")
	}
}

// A stream that opens then goes silent is aborted after roughly the stall window,
// not left to hang.
func TestCompleteStream_DeadStreamTimesOut(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		time.Sleep(2 * time.Second) // headers, then dead air
	}))
	t.Cleanup(ts.Close)
	c := NewOpenAIClient(ts.URL, "m", "", 0)
	c.stall = 100 * time.Millisecond

	start := time.Now()
	err := c.CompleteStream(context.Background(), "", "q", func(string) error { return nil })
	if err == nil {
		t.Fatal("want a stall error, got nil")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("stall took %v, want ~stall (100ms)", elapsed)
	}
}
