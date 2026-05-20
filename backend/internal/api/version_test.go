package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/zcag/tela/backend/internal/auth"
)

// TestVersion_DefaultsShape — handler returns the three string fields with
// usable defaults when no ldflags were supplied at build time. Confirms the
// shape locked by M16.B.1's compat-check contract.
func TestVersion_DefaultsShape(t *testing.T) {
	ts, _ := newWiredServer(t)

	// Force the "no ldflag" branch regardless of how this binary was built.
	prevVer, prevCommit, prevBT := Version, Commit, BuildTime
	Version, Commit, BuildTime = "dev", "unknown", ""
	t.Cleanup(func() { Version, Commit, BuildTime = prevVer, prevCommit, prevBT })

	resp, err := http.Get(ts.URL + "/api/version")
	if err != nil {
		t.Fatalf("GET /api/version: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s want 200", resp.StatusCode, b)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type=%q want application/json", ct)
	}
	var got VersionInfo
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Version != "dev" {
		t.Errorf("version=%q want dev", got.Version)
	}
	if got.Commit != "unknown" {
		t.Errorf("commit=%q want unknown", got.Commit)
	}
	if got.BuiltAt == "" {
		t.Fatal("built_at is empty; expected RFC3339 fallback")
	}
	if _, err := time.Parse(time.RFC3339, got.BuiltAt); err != nil {
		t.Errorf("built_at=%q not RFC3339: %v", got.BuiltAt, err)
	}
}

// TestVersion_HonoursLdflagInjectedValues — when Version/Commit/BuildTime are
// set (mimics `-ldflags "-X .Version=v1.2.3 ..."`), the handler echoes them
// verbatim instead of using the process-start fallback.
func TestVersion_HonoursLdflagInjectedValues(t *testing.T) {
	ts, _ := newWiredServer(t)

	prevVer, prevCommit, prevBT := Version, Commit, BuildTime
	Version = "v1.2.3"
	Commit = "abc1234"
	BuildTime = "2026-05-20T07:30:00Z"
	t.Cleanup(func() { Version, Commit, BuildTime = prevVer, prevCommit, prevBT })

	resp, err := http.Get(ts.URL + "/api/version")
	if err != nil {
		t.Fatalf("GET /api/version: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200", resp.StatusCode)
	}
	var got VersionInfo
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Version != "v1.2.3" {
		t.Errorf("version=%q want v1.2.3", got.Version)
	}
	if got.Commit != "abc1234" {
		t.Errorf("commit=%q want abc1234", got.Commit)
	}
	if got.BuiltAt != "2026-05-20T07:30:00Z" {
		t.Errorf("built_at=%q want injected RFC3339", got.BuiltAt)
	}
}

// TestVersion_UnauthenticatedSucceeds — bare client, no cookie, no bearer.
// Mirrors /api/health: the route MUST be reachable for an MCP client doing
// startup compat-check before it knows the API key is valid.
func TestVersion_UnauthenticatedSucceeds(t *testing.T) {
	ts, _ := newWiredServer(t)

	// Plain client (no cookie jar, no Authorization header).
	resp, err := http.Get(ts.URL + "/api/version")
	if err != nil {
		t.Fatalf("GET /api/version: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s want 200 (auth bypass broken?)", resp.StatusCode, b)
	}
}

// TestVersion_IsPublicPathBypass — defence in depth on the middleware itself.
// The handler is registered behind auth.Middleware, so if IsPublicPath ever
// loses /api/version the unauth GET above would 401. Pin the IsPublicPath
// behaviour explicitly here too.
func TestVersion_IsPublicPathBypass(t *testing.T) {
	if !auth.IsPublicPath("/api/version") {
		t.Fatal("/api/version not in IsPublicPath — middleware will 401 unauth requests")
	}
}
