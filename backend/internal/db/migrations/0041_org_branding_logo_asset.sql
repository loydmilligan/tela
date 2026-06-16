-- 0041_org_branding_logo_asset.sql — store the org logo as a tela-served asset.
--
-- org_branding.logo_url used to hold an arbitrary EXTERNAL https URL. That works
-- in the browser (white-label login/app shell) but NOT in server-side deck
-- rendering: the deck sidecar's Chromium fetches from its own origin, and an
-- external host may be unreachable/unresolvable from there (it bit a real org).
-- So a logo is now always stored IN tela and served from tela's own origin; both
-- upload and import-from-URL just get the bytes into these columns. logo_url is
-- repurposed to hold the internal serve route (content-addressed by logo_hash);
-- a legacy external URL stays until the org re-uploads/imports.
ALTER TABLE org_branding ADD COLUMN logo_data  BYTEA;
ALTER TABLE org_branding ADD COLUMN logo_mime  TEXT NOT NULL DEFAULT '';
ALTER TABLE org_branding ADD COLUMN logo_hash  TEXT NOT NULL DEFAULT '';
