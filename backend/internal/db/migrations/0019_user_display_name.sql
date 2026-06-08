-- 0019_user_display_name.sql — a human-readable name, distinct from the handle.
--
-- The username is a slug: it has to be URL-safe and unique, so SSO derives it by
-- lowercasing + dashing the IdP's name claim ("Ekrem Mert Esen" → ekrem-mert-esen).
-- That slug then leaked into every place we greet the user by name. display_name
-- holds the IdP's original, properly-cased name (captured at SSO provisioning,
-- self-editable from profile settings) so the UI can address people by it and
-- fall back to the username only when it's blank.
--
-- Defaults to '' (NOT NULL) so every existing row reads cleanly with no backfill;
-- pre-existing SSO accounts stay on the username until the user sets a name.

ALTER TABLE users ADD COLUMN display_name TEXT NOT NULL DEFAULT '';
