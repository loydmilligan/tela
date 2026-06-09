package api

import (
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/zcag/tela/backend/internal/auth"
	"github.com/zcag/tela/backend/internal/rag"
)

// Regression for the namespacing bug: allow() checked the bucket under the
// namespaced (purpose, key) but recorded attempts under the raw key, so the
// checked bucket never accumulated and the limit never tripped.
func TestAuthRateLimiter_AllowAccumulates(t *testing.T) {
	rl := newAuthRateLimiter(time.Minute, 3)
	for i := 1; i <= 3; i++ {
		if ok, _ := rl.allow("p", "k"); !ok {
			t.Fatalf("attempt %d: want allowed", i)
		}
	}
	ok, retry := rl.allow("p", "k")
	if ok {
		t.Fatalf("attempt 4: want denied")
	}
	if retry <= 0 {
		t.Fatalf("retry=%v want > 0", retry)
	}
	// A different purpose on the same key has its own budget.
	if ok, _ := rl.allow("other", "k"); !ok {
		t.Fatalf("other purpose: want allowed")
	}
}

// The per-IP email throttle must trip on the email-sending auth endpoints.
func TestRequestPasswordReset_RateLimit(t *testing.T) {
	ts, _ := newAuthServer(t)

	for i := 1; i <= authRateLimit; i++ {
		resp := authPost(t, ts, "/api/auth/request-password-reset", `{"email":"nobody@example.com"}`)
		resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("attempt %d status=%d want 202", i, resp.StatusCode)
		}
	}
	resp := authPost(t, ts, "/api/auth/request-password-reset", `{"email":"nobody@example.com"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("attempt %d status=%d want 429 body=%s", authRateLimit+1, resp.StatusCode, b)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Fatalf("Retry-After header missing on 429")
	}
}

// The per-account managed-compute budget must trip on the cloud proxies.
// Exercised over HTTP with a fake embedder; the limiter is swapped for a
// 3-request one so the test doesn't need 61 calls.
func TestCloudEmbed_RateLimit(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", cloudTestSecret)
	auth.ResetAPIKeySecretCache()
	ts, d, srv := newWiredServerOnDiskWithSrv(t)
	srv.rag = rag.NewServiceWithEmbedder(d, fakeCloudEmbedder{})
	srv.cloudLimiter = newAuthRateLimiter(time.Minute, 3)
	uid := seedUser(t, d, "limituser", "limitpw123", false)
	if _, err := d.Exec(`UPDATE users SET plan_key='personal_plus' WHERE id=$1`, uid); err != nil {
		t.Fatalf("set plan: %v", err)
	}
	rawKey, _ := seedAPIKeyForUser(t, d, uid, auth.ScopeWrite, nil)

	for i := 1; i <= 3; i++ {
		resp := bearerRequest(t, http.MethodPost, ts.URL+"/api/cloud/ollama/api/embed", rawKey,
			`{"model":"x","input":"hello"}`)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("attempt %d status=%d want 200", i, resp.StatusCode)
		}
	}
	resp := bearerRequest(t, http.MethodPost, ts.URL+"/api/cloud/ollama/api/embed", rawKey,
		`{"model":"x","input":"hello"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("attempt 4 status=%d want 429 body=%s", resp.StatusCode, b)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Fatalf("Retry-After header missing on 429")
	}
}
