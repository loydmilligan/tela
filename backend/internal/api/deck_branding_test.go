package api

import (
	"context"
	"testing"

	"github.com/zcag/tela/backend/internal/models"
)

// A deck in an org space inherits the org's brand IDENTITY (logo + accent) when
// unset — but NEVER the variant: the variant is a conscious per-deck choice, not a
// default. Explicit props always win; a personal space inherits nothing.
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

	// No props → logo + accent inherited, but variant stays UNSET (never defaulted
	// from the org's deck_variant, even though one is stored).
	cfg := srv.deckThemeConfig(ctx, models.Page{SpaceID: orgSpace})
	if cfg.Logo != "https://acme.example/logo.svg" || cfg.Accent != "#0A5FBD" {
		t.Fatalf("cfg = %+v, want inherited logo + accent", cfg)
	}
	if cfg.Variant != "" {
		t.Fatalf("variant = %q, want empty — the org variant must NOT be applied as a default", cfg.Variant)
	}

	// Explicit props win over the org brand; logoInvert read as bool.
	cfg = srv.deckThemeConfig(ctx, models.Page{SpaceID: orgSpace, Props: map[string]any{
		"variant": "minimal", "accent": "#FF0000", "logo": "https://acme.example/other.svg", "logoInvert": true,
	}})
	if cfg.Variant != "minimal" || cfg.Accent != "#FF0000" ||
		cfg.Logo != "https://acme.example/other.svg" || !cfg.LogoInvert {
		t.Fatalf("override cfg = %+v, want per-deck props", cfg)
	}

	// Partial props: a chosen variant + accent override stand; logo still inherited.
	cfg = srv.deckThemeConfig(ctx, models.Page{SpaceID: orgSpace, Props: map[string]any{"variant": "soft", "accent": "#00FF00"}})
	if cfg.Variant != "soft" || cfg.Accent != "#00FF00" || cfg.Logo != "https://acme.example/logo.svg" {
		t.Fatalf("partial cfg = %+v, want chosen variant + accent override + inherited logo", cfg)
	}

	// Personal space (no org) inherits nothing.
	personal := seedSpace(t, d, "Mine", "mine", seedUser(t, d, "owner", "pw", false))
	cfg = srv.deckThemeConfig(ctx, models.Page{SpaceID: personal})
	if cfg.Logo != "" || cfg.Accent != "" || cfg.Variant != "" {
		t.Fatalf("personal cfg = %+v, want empty (tahta defaults)", cfg)
	}
}
