package api

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestFeatureFlag_Precedence(t *testing.T) {
	_, _, srv := newWiredServerOnDiskWithSrv(t)
	ctx := context.Background()

	if srv.featureFlag("knowledge") {
		t.Fatal("a flag with no setting and no env must default OFF")
	}
	if err := srv.settings.Set(ctx, "feature.knowledge", "true", nil); err != nil {
		t.Fatalf("set: %v", err)
	}
	if !srv.featureFlag("knowledge") {
		t.Fatal("instance setting feature.knowledge=true should enable")
	}
	// Env overrides the setting (and wins even to turn it back off).
	t.Setenv("TELA_FEATURE_KNOWLEDGE", "off")
	if srv.featureFlag("knowledge") {
		t.Fatal("env TELA_FEATURE_KNOWLEDGE=off must override the setting")
	}
}

func TestRequireFeature_GatesWith404(t *testing.T) {
	_, _, srv := newWiredServerOnDiskWithSrv(t)
	ctx := context.Background()

	rec := httptest.NewRecorder()
	if srv.requireFeature(rec, "knowledge") {
		t.Fatal("requireFeature should block when the flag is off")
	}
	if rec.Code != 404 {
		t.Fatalf("blocked request should 404, got %d", rec.Code)
	}

	if err := srv.settings.Set(ctx, "feature.knowledge", "true", nil); err != nil {
		t.Fatalf("set: %v", err)
	}
	rec2 := httptest.NewRecorder()
	if !srv.requireFeature(rec2, "knowledge") {
		t.Fatal("requireFeature should pass when the flag is on")
	}
}
