-- 0020_instance_settings.sql — a key/value store for instance-level runtime config.
--
-- This is the foundation the admin "configure the instance" surface, persisted
-- secrets, per-plan feature flags, and the cloud-connect token all hang off.
-- Before this, instance config was scattered env-only `os.Getenv` reads, so a
-- generated secret (api-key / share) was random per-process and every restart
-- invalidated outstanding PATs and share cookies. Persisting it here fixes that.
--
-- Read precedence (enforced in code, not here): env override → instance_settings
-- → code default. Env, when set, always wins and is never written back, so an
-- operator can still pin anything from the environment.
--
-- Secret values live under a `secret/` key prefix and are NEVER returned by the
-- admin settings API (the handler filters that prefix); everything else is
-- operator-editable. `value` is TEXT (JSON-encoded for non-string settings),
-- matching the hand-written database/sql + TEXT-datetime conventions.

CREATE TABLE instance_settings (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT tela_now(),
  updated_by INTEGER REFERENCES users(id)
);
