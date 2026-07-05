-- 0067_selfhost_license_refresh.sql — stable per-subscription handle for the
-- cloud license-refresh flow. A self-hosted instance polls the cloud with its
-- current key; the cloud reads the key's embedded lid (= this refresh_id) and
-- returns the renewed token, so EE doesn't lapse on renewal without a manual
-- re-paste. Opaque, stable across renewals, not a secret.
ALTER TABLE selfhost_licenses ADD COLUMN refresh_id TEXT;
CREATE UNIQUE INDEX uq_selfhost_licenses_refresh
  ON selfhost_licenses (refresh_id) WHERE refresh_id IS NOT NULL;
