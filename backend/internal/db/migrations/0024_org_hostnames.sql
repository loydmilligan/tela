-- 0024_org_hostnames.sql — per-org custom domains (white-label front doors).
--
-- A hostname (a subdomain the org controls, e.g. tela.ngss.io) maps to exactly
-- ONE org. For that org's users the hostname IS the app: login + private spaces
-- + editing + shares, branded as theirs. tela's marketing/docs/MCP/sync stay on
-- the canonical host. Distinct from org_email_domains (0001), which maps an
-- *email* domain to an org for auto-join — that's identity routing, this is web
-- hosting. Never conflate the two.
--
-- status flow: 'pending' (added, ownership not yet proven) → 'active' (DNS TXT
-- challenge verified, or an instance-admin forced it). Caddy on-demand TLS only
-- issues a certificate for a hostname that is 'active' (the /api/internal/
-- tls-check ask-endpoint gates on it), so a pending row can't get a cert and
-- can't scope the app. verify_token is the per-host DNS-TXT challenge value.
CREATE TABLE org_hostnames (
  hostname     TEXT PRIMARY KEY,
  org_id       BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','active')),
  verify_token TEXT NOT NULL,
  verified_at  TEXT,
  created_at   TEXT NOT NULL DEFAULT tela_now(),
  updated_at   TEXT NOT NULL DEFAULT tela_now()
);

CREATE INDEX idx_org_hostnames_org ON org_hostnames(org_id);
-- The host→org middleware resolves active hostnames on the request path; the
-- TLS ask-endpoint filters the same way. Index the hot predicate.
CREATE INDEX idx_org_hostnames_active ON org_hostnames(hostname) WHERE status = 'active';
