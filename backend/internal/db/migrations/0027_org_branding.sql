-- 0027_org_branding.sql — per-org visual branding for the white-label surface
-- (custom-domain login screen + app shell).
--
-- logo_url: an https URL to the org's logo image (shown in place of the tela
-- wordmark). accent: a CSS color the SPA injects as --accent at runtime (hex or
-- oklch/rgb). Both empty by default ⇒ fall back to tela's branding. Sibling to
-- org_login_settings (0026): that governs which sign-in methods show; this
-- governs how the surface looks. One row per org.
CREATE TABLE org_branding (
  org_id     BIGINT PRIMARY KEY REFERENCES orgs(id) ON DELETE CASCADE,
  logo_url   TEXT NOT NULL DEFAULT '',
  accent     TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT tela_now()
);
