-- 0065_enterprise_display_price.sql — give cloud Enterprise a published floor so
-- the in-app Plan & Usage card matches the landing ("from $15/seat/mo") instead of
-- "let's talk". Display only: Enterprise isn't self-serve (no Polar product in the
-- map), so nothing charges off this — checkout is still contact-sales. The price
-- guard skips it (only wired products are compared).
UPDATE plans SET price_cents = 1500, price_period = 'per seat / month', updated_at = tela_now()
 WHERE key = 'org_enterprise';
