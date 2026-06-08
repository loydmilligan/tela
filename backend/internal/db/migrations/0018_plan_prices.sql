-- 0018_plan_prices.sql — display prices for the plan catalog (in-app + the
-- public /api/plans). price_cents NULL = custom/contact ("talk to us"); 0 = free.
-- price_period is the qualifier shown under the amount. These are display values
-- only — there's no billing engine yet; they keep the in-app catalog and the
-- landing in sync from one source.

ALTER TABLE plans ADD COLUMN price_cents  INTEGER;          -- NULL = custom, 0 = free
ALTER TABLE plans ADD COLUMN price_period TEXT NOT NULL DEFAULT '';

UPDATE plans SET price_cents = 0,    price_period = 'free forever'       WHERE key = 'personal_free';
UPDATE plans SET price_cents = 800,  price_period = 'per month'          WHERE key = 'personal_plus';
UPDATE plans SET price_cents = 0,    price_period = 'up to 5 members'    WHERE key = 'org_free';
UPDATE plans SET price_cents = 600,  price_period = 'per member / month' WHERE key = 'org_team';
UPDATE plans SET price_cents = NULL, price_period = 'let''s talk'        WHERE key = 'org_enterprise';
-- personal_unlimited stays NULL / '' — it's an internal comp tier, never shown.
