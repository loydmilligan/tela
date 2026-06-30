package api

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"

	"github.com/zcag/tela/backend/internal/billing"
)

// verifyBillingPrices cross-checks every wired Polar product's live price against
// the plans table, so a reprice in the Polar dashboard that never made it into
// tela (or a plans edit not mirrored in Polar) is caught loudly at boot instead
// of silently charging an amount the UI never shows. Advisory only: it logs and
// returns, never blocking boot or mutating anything. No-op when billing is off.
func (s *Server) verifyBillingPrices(ctx context.Context) {
	if !s.billing.Enabled() {
		return
	}
	for key, productID := range s.billing.ConfiguredProducts() {
		plan := strings.TrimSuffix(key, "@year")
		interval := billing.IntervalMonth
		if strings.HasSuffix(key, "@year") {
			interval = billing.IntervalYear
		}

		var monthly, yearly sql.NullInt64
		if err := s.DB.QueryRowContext(ctx,
			`SELECT price_cents, price_cents_yearly FROM plans WHERE key = $1`, plan).
			Scan(&monthly, &yearly); err != nil {
			slog.Warn("billing price guard: wired product has no matching plan",
				"plan", plan, "product", productID, "err", err)
			continue
		}
		expected := monthly
		if interval == billing.IntervalYear {
			expected = yearly
		}
		if !expected.Valid {
			// No price recorded for this cadence (free/custom tier) — nothing to compare.
			continue
		}

		prod, err := s.billing.GetProduct(ctx, productID)
		if err != nil {
			slog.Warn("billing price guard: could not fetch Polar product",
				"plan", plan, "interval", interval, "product", productID, "err", err)
			continue
		}
		// A product wired as "@year" must actually be the yearly product, etc.
		if prod.RecurringInterval != "" && prod.RecurringInterval != interval {
			slog.Error("billing price guard: product cadence mismatch — wrong product wired",
				"plan", plan, "wired_as", interval, "polar_interval", prod.RecurringInterval, "product", productID)
		}
		actual, ok := prod.FixedPriceCents()
		if !ok {
			slog.Warn("billing price guard: Polar product has no fixed price",
				"plan", plan, "interval", interval, "product", productID)
			continue
		}
		if int64(actual) != expected.Int64 {
			slog.Error("billing price MISMATCH — Polar product price differs from the plans table; checkout would charge the Polar amount while the UI shows the plans amount",
				"plan", plan, "interval", interval, "product", productID,
				"plans_cents", expected.Int64, "polar_cents", actual)
		} else {
			slog.Debug("billing price guard: ok",
				"plan", plan, "interval", interval, "cents", actual)
		}
	}
}
