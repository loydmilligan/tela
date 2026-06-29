# Editions & Pricing — the model

Canonical model for how tela is licensed, packaged, gated, and priced across **cloud**
and **self-host**. This is the source of truth for the strategy; the landing
(`CONTENT.md` + `landing/`), the backend `plans` table, the EE module, and Polar
products all implement *this*. Supersedes the scratch `pricing-handoff.md` (research
only) and the prior split-ladder model frozen in `CONTENT.md` §8b.

> Status: **design locked, not yet implemented.** The live product + Polar still run the
> previous split personal/org ladder. Building this = backend `plans` rework + a
> proprietary `ee/` module + license keys + repriced Polar products. Do not deploy a
> landing that advertises this until the backend catches up (checkout would mismatch).

---

## 1. The one-line model

> **Where it runs decides the AI. Which edition decides the company-layer.**
>
> - **Cloud** = we run it, **AI included** (managed, metered).
> - **Self-host** = you run it, **bring your own AI** (any OpenAI-compatible LLM + embedder) — always, Community *and* Enterprise.
> - **Enterprise** = the same company-of-record feature layer (identity, governance, compliance, scale, support), bought bundled on cloud or as a license key on self-host.

We never gate the wiki or the AI *capability*. We charge companies for the things only
companies need. That is the whole posture, and it's a marketing asset — say it out loud.

## 2. Three revenue levers

| Lever | What it is | Captures |
|---|---|---|
| **1. Cloud subscriptions** | The Free / Personal / Team / Enterprise ladder | Individuals + teams who want managed AI + zero-ops |
| **2. Enterprise module (self-host)** | Closed `ee/` features unlocked by a per-seat **license key** | Companies self-hosting that need identity / governance / scale |
| **3. Commercial license** | Legal relief from the core's AGPL copyleft | Companies whose lawyers won't allow AGPL — even without EE |

The current AGPL-only / one-binary state has only lever 1 (and a vague, un-operationalized
lever 3). This model turns on all three.

## 3. License & packaging

- **Core stays AGPL-3.0**, now **dual-licensed**. AGPL is deliberate: a company that
  won't ship AGPL must buy a **commercial license** (lever 3) — even if it never touches
  an Enterprise feature. The core is *already* AGPL, so this is **additive, not a
  relicense** — no existing code changes license, no community blast radius.
- **Enterprise module = proprietary, closed-source, in a carved `ee/` directory**,
  compiled into the same binary but **inert without a valid license key**. One image
  ships to everyone; the key unlocks EE features + the seat cap (the GitLab model —
  simplest distribution).
- **License keys = signed offline tokens** (tier + seat cap + expiry) so air-gapped
  self-host works with **no phone-home**. Optional periodic online check.
- **One EE codebase powers both** cloud Enterprise and self-host Enterprise — build the
  enterprise features once.
- **Brand/name reserved** separately (`TRADEMARK.md`) — unchanged.

## 4. The gating line — core vs Enterprise

Principle: the **community core must be genuinely lovable** (it's the distribution), so it
keeps the crown jewels — including AI *capability*. Enterprise gates what **companies need
and individuals don't**.

| Capability | Core (AGPL, free) | Enterprise (closed `ee/`, key) |
|---|:---:|:---:|
| Wiki, Milkdown editor, history, page properties | ✅ | ✅ |
| Live collaboration (Yjs) | ✅ | ✅ |
| Decks, public spaces / blog, custom domains | ✅ | ✅ |
| WebDAV sync, MCP / agents | ✅ | ✅ |
| **Atlas** (sources → maintained wiki) | ✅ | ✅ |
| **Ask + semantic search** | ✅ | ✅ |
| Keyword search, orgs, basic roles (admin/member), per-space access | ✅ | ✅ |
| **SSO / SAML / OIDC** | — | ✅ |
| **SCIM provisioning** | — | ✅ |
| **Audit logs** | — | ✅ |
| **Advanced RBAC / custom roles / granular permissions** | — | ✅ |
| **Retention / legal hold / compliance** | — | ✅ |
| **Premium Atlas connectors** (Slack / Drive / GitHub / Confluence; scheduled + live sync) | — | ✅ |
| **HA / clustering, white-label** | — | ✅ |
| **Priority support + SLA** | — | ✅ |

Note the deliberate placement of **Atlas / Ask / semantic search in the free core**. On
self-host they need BYO inference (a cost the operator already pays), so they aren't ours
to meter there; keeping them free is what makes the community edition spread. We monetize
that AI through **cloud (managed/included)**, not by crippling self-host.

> **Change from today (verified against code, 2026-06-30):** SSO (social + per-org OIDC,
> `sso_handlers.go`/`org_sso.go`/migration `0016`) and audit (`events` feed + `access_audit`/
> `api_key_audit`, migration `0033`) are **already built and ungated** — free to everyone
> today. The work is to put them behind the **unified entitlement** below, not to build them.
> tela is **pre-release with no self-serve customers**, so this is a clean greenfield gating
> change — no grandfathering, no rug-pull. Basic orgs + per-space roles stay in core.

### One entitlement, two unlock paths

A single `entitled(ctx, acct, feature)` gates every paid feature (`sso`, `audit`, `scim`,
`premium_connectors`, `retention`, …). It returns true if **either**:
- **cloud:** the account's plan grants it (`plans.features[feature]`, the existing
  `featureEnabled` path), **or**
- **self-host:** a valid installed **license key** grants it.

So the same feature code is gated once; cloud unlocks via the plan, self-host via the key.
No duplicate gating, no `ee/`-only forks of feature logic — just where the entitlement
comes from.

## 5. AI packaging

- **Cloud:** managed AI is **included**, metered by a **monthly answer allowance** + an
  **Atlas source cap** per tier; overage handled by **purchasable credit top-ups**, not a
  forced tier jump. AI is a **first-class, visible line** in every cloud tier — because AI
  usage (not seats) is the real cloud COGS.
- **Self-host (all editions):** **BYO** — point tela at any OpenAI-compatible LLM + an
  Ollama-compatible embedder. To kill the cold-start, ship a **one-command "AI on"**
  (`docker-compose.ai.yml`: a recommended local model, `keep_alive` pinned) so Community
  is magical out of the box without "now go integrate an LLM."
- We do **not** resell/meter AI to self-hosters. That's a deliberate giveaway, not an
  oversight — the single biggest thing left on the table, traded for distribution.

## 6. Cloud ladder

AI is the metered axis. Pages / spaces / storage are **not** headline gates (cheap to run,
nobody gates on them) — keep generous, cap only for abuse.

| | **Free** | **Personal** | **Team** | **Enterprise** |
|---|---|---|---|---|
| Who | Trying tela | Individual power user | Any team (the real business) | Compliance / scale |
| Price | **$0** | **$8/mo** ($72/yr → $6/mo) | **$10/seat/mo** ($96/yr → $8/seat/mo) | Custom |
| Seats | 1 | 1 | 2+ | Negotiated |
| Managed AI answers / mo | 50 | 1,000 | 2,000 pooled (+credits) | Negotiated |
| Atlas sources | 1 | 5 | 20 | Unlimited |
| Wiki / decks / public spaces / WebDAV / MCP | ✅ | ✅ | ✅ | ✅ |
| Pages / spaces / storage | Soft ceiling | Generous | Generous | Unlimited |
| Custom domain | — | ✅ | ✅ | ✅ |
| Basic RBAC / member mgmt | — | — | ✅ | ✅ advanced |
| SSO / SCIM / audit / governance | — | — | — | ✅ |
| Support | Community | Community | Email | Priority + SLA |

**Open numbers (flagged):** Team moves **$6 → $10/seat** (below-band today + real AI COGS;
market mid-tier is $8–15). Free tier carries managed-AI COGS with no revenue — capped to a
**taste**; the "I want real AI for free" crowd is routed to **self-host Community** (their
inference, zero cost to us). Trial: new accounts get a 30-day Personal trial, then settle.

## 7. Self-host options

| | **Community** | **Enterprise (self-host)** | **Commercial license (core only)** |
|---|---|---|---|
| Price | **Free** | Per-seat, annual (license key); self-serve, contact-sales for whales | Flat annual |
| License | AGPL-3.0 | Commercial + EE key | Commercial (AGPL relief) |
| AI | BYO | BYO | BYO |
| Gets | Full core (§4) — wiki, Atlas, Ask, decks, sync, MCP, custom domains | Core + all Enterprise features + support/SLA | Full core, no EE, no copyleft obligation |
| For | Individuals, OSS, small teams | Companies self-hosting at scale/compliance | Copyleft-averse companies that don't need EE |

**Make it a funnel, not a phone number** (GitLab playbook):
- EE features ship **visible-but-locked** in the Community binary ("SSO → Upgrade"), so the
  upsell lives in-product where the admin feels the pain.
- **Self-serve EE keys** (buy online, per-seat, key issued instantly) + a **self-serve EE
  trial key** (14–30 days) to feel SSO before paying.
- This lets self-host stay **two tiers** (Community + Enterprise) instead of inventing a
  mid rung — one paid SKU, self-serve, negotiated lane on top.

**Open number (flagged):** self-host Enterprise indicative **from ~$12/user/mo billed
annually**. Commercial-license flat-annual figure: TBD.

## 8. Naming / how it reads (kills the old confusion)

Present **two orthogonal axes**, never a flat list:

|  | **tela Cloud** (we run it · **AI included**) | **tela Self-Hosted** (you run it · **BYO AI**) |
|---|---|---|
| Free | Free | **Community** |
| Individual / team | Personal · Team | — *(use Community)* |
| Enterprise layer | Cloud Enterprise | Self-host Enterprise (key) |

One read: **the column picks who-runs-it-and-who-brings-the-AI; the row picks how much
company-layer you need.** "Enterprise" = the same feature layer in both columns.

## 9. Anti-reseller stance (decided, not defaulted)

Could someone take Community and resell tela-as-a-service? AGPL deters but doesn't forbid a
compliant reseller. A source-available core (BSL/FSL, time-converting) would forbid it, at
the cost of OSI "open source" cred + goodwill. For a wiki this size the reseller risk is
low → **stay AGPL-dual**, accept it. Revisit only if a reseller actually appears.

## 10. Build order (phased)

Landing copy is **done** (`CONTENT.md` §8b, `Pricing.astro`, `SelfHostPricing.astro`).
SSO + audit + feature-flag infra + Polar + quotas already exist — see §4 note. Remaining:

**Phase 1 — Cloud ladder + entitlement gating (unblocks the landing deploy).**
- Migration: rationalize the `plans` catalog — rename `personal_plus`→Personal, `org_team`→
  Team at **$10/$8**; raise storage/page caps off the headline (keep as anti-abuse); set
  `features = {sso, audit}` true on Enterprise; confirm AI caps (Free 50/1 · Personal 1000/5
  · Team 2000/20 · Ent ∞).
- Add `entitled()` wrapping `featureEnabled` (cloud path; key path stubbed for now).
- Gate SSO config/use + the audit screen behind `entitled(…, 'sso'/'audit')`.
- Polar: repriced Team products ($10/mo, $96/yr); swap `TELA_POLAR_PRODUCTS` on the box.
- Frontend: collapse the hardcoded personal/org grouping into the 4-tier ladder; drop SSO
  from "every plan includes"; scaffold the visible-but-locked affordance.
- Verify checkout end-to-end → **`make deploy-landing`** (cloud half of the site is now true).

**Phase 2 — License keys + `ee/` module (self-host capture infra).**
- Signed offline key (Ed25519: tier + seat cap + expiry + `features[]`); embedded public
  key; one binary, key-gated (no build tags). Admin "License" tab: paste → verify → unlock.
- Wire the key path into `entitled()`; seat enforcement vs the key.

**Phase 3 — Net-new EE features + self-serve keys.**
- The clean greenfield EE: **SCIM**, **premium Atlas connectors** (Slack/Drive/GitHub/
  Confluence), retention/legal-hold, white-label polish.
- Self-serve key issuance (Polar product → webhook mints + emails a key) + trial keys;
  visible-but-locked EE UI fully wired.

**Phase 4 — Commercial license + BYO-AI + docs + full deploy.**
- Published commercial-license (AGPL-relief) SKU + terms; dual-license docs.
- `docker-compose.ai.yml` one-command BYO-AI default for self-host.
- Update `docs/` + tela Docs space 16; deploy the self-host half of `/pricing`.

**Follow-on (not blocking):** AI credit top-ups for cloud overage (a Polar one-time product
+ `cloud_usage` credit balance) — slots after Phase 1 whenever overage volume justifies it.
