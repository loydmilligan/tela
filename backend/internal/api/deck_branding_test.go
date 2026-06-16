package api

import (
	"context"
	"testing"

	"github.com/zcag/tela/backend/internal/models"
)

// A deck in an org space with no per-deck brand props inherits the org's
// branding (logo/accent/preferred variant). Explicit props override it; a
// personal space (no org) inherits nothing.
func TestDeckThemeConfig_OrgBrandInheritance(t *testing.T) {
	_, d, srv := newWiredServerOnDiskWithSrv(t)
	ctx := context.Background()

	org := seedOrg(t, d, "Acme", "acme")
	if _, err := d.ExecContext(ctx,
		`INSERT INTO org_branding (org_id, logo_url, accent, deck_variant)
		 VALUES ($1, 'https://acme.example/logo.svg', '#0A5FBD', 'brutalist')`, org); err != nil {
		t.Fatalf("seed branding: %v", err)
	}
	orgSpace := seedOrgSpace(t, d, "Brand", "brand", org)

	// No props → fully inherited.
	cfg := srv.deckThemeConfig(ctx, models.Page{SpaceID: orgSpace})
	if cfg.Logo != "https://acme.example/logo.svg" || cfg.Accent != "#0A5FBD" || cfg.Variant != "brutalist" {
		t.Fatalf("inherited cfg = %+v, want org brand", cfg)
	}

	// Explicit props win over the org brand; logoInvert read as bool.
	cfg = srv.deckThemeConfig(ctx, models.Page{SpaceID: orgSpace, Props: map[string]any{
		"variant": "minimal", "accent": "#FF0000", "logo": "https://acme.example/other.svg", "logoInvert": true,
	}})
	if cfg.Variant != "minimal" || cfg.Accent != "#FF0000" ||
		cfg.Logo != "https://acme.example/other.svg" || !cfg.LogoInvert {
		t.Fatalf("override cfg = %+v, want per-deck props", cfg)
	}

	// Partial props: keep the prop, inherit the rest.
	cfg = srv.deckThemeConfig(ctx, models.Page{SpaceID: orgSpace, Props: map[string]any{"accent": "#00FF00"}})
	if cfg.Accent != "#00FF00" || cfg.Logo != "https://acme.example/logo.svg" || cfg.Variant != "brutalist" {
		t.Fatalf("partial cfg = %+v, want accent override + inherited logo/variant", cfg)
	}

	// Personal space (no org) inherits nothing.
	personal := seedSpace(t, d, "Mine", "mine", seedUser(t, d, "owner", "pw", false))
	cfg = srv.deckThemeConfig(ctx, models.Page{SpaceID: personal})
	if cfg.Logo != "" || cfg.Accent != "" || cfg.Variant != "" {
		t.Fatalf("personal cfg = %+v, want empty (tahta defaults)", cfg)
	}
}
