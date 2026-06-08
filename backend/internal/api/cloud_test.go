package api

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/zcag/tela/backend/internal/auth"
	"github.com/zcag/tela/backend/internal/rag"
)

type fakeCloudEmbedder struct{}

func (fakeCloudEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return []float32{0.1, 0.2, 0.3}, nil
}
func (fakeCloudEmbedder) Model() string { return "fake" }

const cloudTestSecret = "deadbeef00112233445566778899aabbccddeeff00112233445566778899aabb"

func TestCloudEntitlements_ReturnsPlanFeatures(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", cloudTestSecret)
	auth.ResetAPIKeySecretCache()
	ts, d, _ := newWiredServerOnDiskWithSrv(t)
	uid := seedUser(t, d, "sub", "subpw1234", false)
	if _, err := d.Exec(`UPDATE users SET plan_key='personal_plus' WHERE id=$1`, uid); err != nil {
		t.Fatalf("set plan: %v", err)
	}
	rawKey, _ := seedAPIKeyForUser(t, d, uid, auth.ScopeRead, nil)

	resp := bearerRequest(t, http.MethodGet, ts.URL+"/api/cloud/entitlements", rawKey, "")
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%q want 200", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"managed_rag":true`) || !strings.Contains(string(body), `personal_plus`) {
		t.Fatalf("entitlements body missing plan/feature: %q", body)
	}
}

func TestCloudEmbed_GatedByEntitlement(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", cloudTestSecret)
	auth.ResetAPIKeySecretCache()
	ts, d, _ := newWiredServerOnDiskWithSrv(t)
	uid := seedUser(t, d, "freeuser", "freepw1234", false) // default personal_free, no managed_rag
	rawKey, _ := seedAPIKeyForUser(t, d, uid, auth.ScopeRead, nil)

	resp := bearerRequest(t, http.MethodPost, ts.URL+"/api/cloud/ollama/api/embed", rawKey,
		`{"model":"x","input":"hello"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("free plan embed status=%d want 403", resp.StatusCode)
	}
}

func TestCloudEmbed_EntitledReturnsEmbedding(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", cloudTestSecret)
	auth.ResetAPIKeySecretCache()
	ts, d, srv := newWiredServerOnDiskWithSrv(t)
	srv.rag = rag.NewServiceWithEmbedder(d, fakeCloudEmbedder{}) // inject so the proxy has an embedder
	uid := seedUser(t, d, "plususer", "pluspw1234", false)
	if _, err := d.Exec(`UPDATE users SET plan_key='personal_plus' WHERE id=$1`, uid); err != nil {
		t.Fatalf("set plan: %v", err)
	}
	rawKey, _ := seedAPIKeyForUser(t, d, uid, auth.ScopeRead, nil)

	resp := bearerRequest(t, http.MethodPost, ts.URL+"/api/cloud/ollama/api/embed", rawKey,
		`{"model":"x","input":"hello world"}`)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%q want 200", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"embeddings":[[`) {
		t.Fatalf("embed body not Ollama-shaped: %q", body)
	}
}

func TestCloudEndpoints_RejectNoToken(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", cloudTestSecret)
	auth.ResetAPIKeySecretCache()
	ts, _, _ := newWiredServerOnDiskWithSrv(t)

	resp := bearerRequest(t, http.MethodGet, ts.URL+"/api/cloud/entitlements", "tela_pat_bogus", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bogus token status=%d want 401", resp.StatusCode)
	}
}
