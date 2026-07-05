-- 0066_enterprise_no_price.sql — revert 0065. Cloud Enterprise is contact-sales
-- with NO published price (decided 2026-07-05): show "let's talk", not "$15", in
-- the in-app Plan & Usage card, matching the landing. NULL price_cents = custom.
UPDATE plans SET price_cents = NULL, price_period = 'let''s talk', updated_at = tela_now()
 WHERE key = 'org_enterprise';
