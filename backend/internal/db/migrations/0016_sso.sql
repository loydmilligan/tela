-- 0016_sso.sql — federated sign-in: social login (Google/Microsoft/GitHub) and
-- per-org OIDC SSO.
--
-- Login was email/username + password only (api/auth.go). The org layer already
-- exists (orgs, org_members, org_email_domains auto-join, space_grants), so SSO
-- is purely a new front door: an external identity is mapped to a tela user and
-- the existing provisioning chain (EnsurePersonalSpace → applyAutoJoin →
-- CreateSession) takes over unchanged.
--
-- Two tables: sso_identities maps an IdP subject to a tela user (the durable
-- link, so a returning SSO user lands on the same account); org_sso holds one
-- OIDC connection per org for enterprise SSO. Social providers are configured
-- instance-wide via env (TELA_SSO_*), so they need no table.

-- An external identity bound to a tela user. provider is the social provider
-- name ('google'|'microsoft'|'github') or 'org:<id>' for an org connection;
-- subject is the IdP's stable user id (OIDC `sub`, or the GitHub numeric id).
-- UNIQUE(provider, subject) makes "have I seen this identity before?" a single
-- indexed lookup and prevents two tela users claiming the same external id.
CREATE TABLE sso_identities (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider   TEXT   NOT NULL,
    subject    TEXT   NOT NULL,
    email      TEXT,
    created_at TEXT   NOT NULL DEFAULT tela_now(),
    UNIQUE (provider, subject)
);

CREATE INDEX idx_sso_identities_user ON sso_identities (user_id);

-- One OIDC connection per org (v1). client_secret is stored plaintext — the
-- same posture as the deploy/.env secrets this instance already trusts; it is
-- never returned by the read API. enforced=1 blocks password login for the
-- org's auto-join domains, funnelling those users through SSO.
CREATE TABLE org_sso (
    org_id        BIGINT  PRIMARY KEY REFERENCES orgs(id) ON DELETE CASCADE,
    issuer        TEXT    NOT NULL,
    client_id     TEXT    NOT NULL,
    client_secret TEXT    NOT NULL,
    enforced      INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT    NOT NULL DEFAULT tela_now(),
    updated_at    TEXT    NOT NULL DEFAULT tela_now()
);
