-- 0054_plan_yearly.sql — annual (yearly) billing prices. Sibling to 0018's
-- monthly price_cents: the display amount for a yearly subscription. NULL when a
-- tier has no yearly option (free / custom / internal). Pricing = "2 months free"
-- (10× the monthly), so the yearly toggle is a real saving, not just a cadence.
-- The yearly *period* qualifier is derived in the UI (per year / per member / year);
-- only the amount is data here. The matching Polar products are wired via
-- TELA_POLAR_PRODUCTS (the `<plan>@year` entries) — see docs/billing.md.

ALTER TABLE plans ADD COLUMN price_cents_yearly INTEGER;  -- annual display price; NULL = no yearly option

UPDATE plans SET price_cents_yearly = 8000 WHERE key = 'personal_plus'; -- $80/yr  ($8/mo × 10)
UPDATE plans SET price_cents_yearly = 6000 WHERE key = 'org_team';      -- $60/seat/yr ($6/mo × 10)
