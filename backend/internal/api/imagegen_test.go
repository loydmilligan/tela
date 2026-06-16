package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// generate POSTs an OpenAI Images request and decodes the b64_json result.
func TestImageGen_Generate(t *testing.T) {
	pngB64 := base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\nFAKE"))
	var gotPath, gotPrompt, gotSize, gotModel string
	var gotN, gotSteps int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var req imageGenReq
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		gotPrompt, gotSize, gotModel, gotN, gotSteps = req.Prompt, req.Size, req.Model, req.N, req.Steps
		_ = json.NewEncoder(w).Encode(imageGenResp{Data: []struct {
			B64JSON string `json:"b64_json"`
			URL     string `json:"url"`
		}{{B64JSON: pngB64}}})
	}))
	defer srv.Close()

	g := imageGen{baseURL: srv.URL, model: "flux2-klein-4b"}
	img, err := g.generate(context.Background(), "a misty harbour at dawn", "", "", 4, 0)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if string(img) != "\x89PNG\r\n\x1a\nFAKE" {
		t.Fatalf("decoded bytes mismatch: %q", img)
	}
	if gotPath != "/images/generations" {
		t.Errorf("path = %q, want /images/generations", gotPath)
	}
	if gotPrompt != "a misty harbour at dawn" || gotN != 1 || gotSteps != 4 {
		t.Errorf("req = prompt:%q n:%d steps:%d", gotPrompt, gotN, gotSteps)
	}
	if gotSize != "1280x720" { // default applied
		t.Errorf("size default = %q, want 1280x720", gotSize)
	}
	if gotModel != "flux2-klein-4b" { // configured default used when none passed
		t.Errorf("model = %q, want configured default", gotModel)
	}
}

// generate surfaces a non-200 from the endpoint as an error.
func TestImageGen_GenerateError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not loaded", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	if _, err := (imageGen{baseURL: srv.URL}).generate(context.Background(), "x", "", "", 0, 0); err == nil {
		t.Fatal("expected error on 503")
	}
}

// imageGenEnabled honours the ai.disabled kill-switch even when configured.
func TestImageGenEnabled_KillSwitch(t *testing.T) {
	t.Setenv("TELA_IMAGE_GEN_URL", "http://example:11437/v1")
	_, _, srv := newWiredServerOnDiskWithSrv(t)
	if !srv.imageGenEnabled() {
		t.Fatal("should be enabled when configured + not paused")
	}
	if err := srv.settings.Set(context.Background(), "ai.disabled", "1", nil); err != nil {
		t.Fatalf("set kill-switch: %v", err)
	}
	if srv.imageGenEnabled() {
		t.Fatal("ai.disabled=1 must disable image generation")
	}
}
