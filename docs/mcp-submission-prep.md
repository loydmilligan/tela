# MCP directory submission — master prep checklist

Grounded in the **actual** submission surfaces + policies (researched 2026-06-05):
- Claude form: the real 6-page Google Form behind `clau.de/mcp-directory-submission` (parsed from its own field data) + [Software Directory Policy](https://support.claude.com/en/articles/13145358-anthropic-software-directory-policy) + [review criteria](https://claude.com/docs/connectors/building/review-criteria) + [auth](https://www.claude.com/docs/connectors/building/authentication) + [IP ranges](https://platform.claude.com/docs/en/api/ip-addresses).
- ChatGPT: [app-submission-guidelines](https://developers.openai.com/apps-sdk/app-submission-guidelines) + [deploy/submission](https://developers.openai.com/apps-sdk/deploy/submission) (via Wayback — OpenAI TLS-blocks this host) + dashboard form per secondary walkthroughs.

Legend: ✅ done · 🔨 I can do now · 👤 Cagdas (account/dashboard) · 🖥️ in-host (needs a live Claude/ChatGPT render) · 🔎 verify

---

## A. Published artifacts (both hosts)
| Item | Claude | ChatGPT | Status |
|---|---|---|---|
| Privacy policy (public URL) | mandatory | mandatory | ✅ `tela.cagdas.io/privacy` |
| **Terms of Service (public URL, same domain)** | form attestation (Page 6) | reported required form field | 🔨 **MISSING — build `/terms`** |
| Public documentation | mandatory | (docs link) | ✅ `tela.cagdas.io/mcp` — but 🔨 **add Troubleshooting + Limitations** (Claude requires both) |
| Support contact | mandatory | mandatory | ✅ `robot@cagdas.io` |
| Security vulnerability reporting mechanism | mandatory (ongoing) | — | 🔨 add a security-contact line to privacy/docs |

## B. Assets
| Item | Spec | Status |
|---|---|---|
| Square logo SVG (Claude) | 1:1 SVG, served at a URL | ✅ `tela.cagdas.io/favicon.svg` |
| Favicon verification (Claude) | `s2/favicons?domain=tela.cagdas.io&sz=64` must show tela's mark | 🔎 verify it resolves to the indigo mark, not a default |
| App icon 64×64 PNG (ChatGPT, opt. light/dark) | 64×64 PNG | 🔨 generate from the SVG (have `icon-512.png`) |
| Widget screenshots | Claude: 3–5 PNG ≥1000px, cropped to the app response. ChatGPT: 1–4 PNG (~706px wide, unconfirmed), **no chat prompt in frame** | 🖥️ capture page-reader + search-results in a live host |
| Promo/demo (optional) | Drive link; ChatGPT may want an MP4 demo on same domain | 👤/🖥️ optional |

## C. Account / dashboard
| Item | Host | Status |
|---|---|---|
| OAuth 2.1 + S256 PKCE + DCR + form `/token` | both | ✅ verified live (issuer `decisive-relation-32`) |
| Anthropic egress `160.79.104.0/21` allowlisted | Claude | ✅ Cloudflare rule `0b545114` live |
| Org identity verification (individual) | ChatGPT | 🟡 in progress — `platform.openai.com/settings/organization/general` |
| Billing: $5 prepaid, auto-recharge OFF | ChatGPT | 👤 in progress |
| `api.apps.write` (org owner) | ChatGPT | 👤 |
| Global (non-EU) data-residency project | ChatGPT | 👤 |
| Final submit | both | 👤 Claude: `clau.de/mcp-directory-submission` · ChatGPT: `platform.openai.com/apps-manage` |

## D. Content to write
| Item | For | Status |
|---|---|---|
| Field-by-field answers | Claude | ✅ render `mira.cagdas.io/r/wqchm6` + `docs/mcp-submission-claude.md` |
| App name/desc/category/tagline | both | ✅ in submission docs |
| 20-tool list + human names + annotations | both | ✅ |
| `openWorldHint`/`destructiveHint` per-tool **written justifications** | ChatGPT (required) | ✅ in `docs/mcp-submission-chatgpt.md` |
| Test cases: **5 positive + 3 negative**, with prompt + tool + expected output | ChatGPT | 🔨 write |
| Demo-account reviewer script | both | ✅ |

## E. Verify (engineering)
| Item | Status |
|---|---|
| Tool annotations correct over the wire | ✅ Inspector-verified live |
| Response size cap (≤~25k tokens) | ✅ `get_page`/`fetch` capped 80k chars |
| Read/write split, names ≤64, actionable errors | ✅ |
| **Origin-header validation** on `/api/mcp` (DNS-rebind guard) | 🔎 confirm the MCP endpoint validates Origin |
| **Data minimization** — tool outputs carry NO telemetry/internal IDs (session/trace/request IDs, logging metadata) | 🔎 audit get_page/search/fetch outputs (page timestamps/space_id are content, likely fine) |
| Widgets render in-host | ✅ verified (other agent) |

## F. Done (engineering, confirmed live)
Transport (Streamable HTTP) · OAuth chain · 20 annotated tools · resources · widgets · search+fetch · privacy + docs live · demo account seeded · Cloudflare allowlist · MCP Inspector pass.

---

## The actual gap list (what's genuinely left to *prepare*)
**I can build now (🔨):** Terms of Service page · Troubleshooting + Limitations in the docs · security-contact line · 64×64 PNG icon · ChatGPT test-cases doc · the two 🔎 verifies (Origin validation, output data-minimization).
**In-host (🖥️):** widget screenshots (both directories).
**Cagdas (👤):** finish OpenAI org verification + billing + residency project + `api.apps.write`; verify the favicon; the two final submits.
