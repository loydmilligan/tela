-- Drop the Plus annual price $80 → $72/yr so the per-month equivalent is a clean
-- $6/mo (= 25% off the $8/mo monthly price; was $80/yr, i.e. "2 months free" →
-- $6.67/mo, which read awkwardly). Display price only — the ACTUAL charge is the
-- Polar `prod_plus_yearly` product, which must be repriced to $72 to match (see
-- docs/billing.md). Team is unchanged at $60/seat/yr ($5/mo, still 2 months free).
UPDATE plans SET price_cents_yearly = 7200 WHERE key = 'personal_plus'; -- $72/yr ($6/mo)
