# Ideas worth looking into

A running shortlist of features worth evaluating for tela. Pulled from the full
feature-ideas board — **https://mira.cagdas.io/p/tela-feature-ideas** — where every
idea is numbered, scored, and grouped (10 themes + a by-horizon kanban). The `#N`
here is that board number, so anything is easy to cross-reference back.

> Shortlisted 2026-05-31. These are candidates to look into — not committed scope.
> "Source" = the tool(s) the idea is mined from ("novel" = net-new to the category).

## Authoring & editor

- **#3 — Tabs & code-group blocks** — tabbed content / multi-language code switcher. _(Mintlify, Docusaurus)_
- **#4 — Synced blocks / transclusion** — reuse one block's content across pages, edit once. _(Notion, Roam)_
- **#7 — Multi-column layout blocks** — side-by-side content columns. _(Notion, Craft)_

## Knowledge & linking

- **#10 — Page owners + freshness / expiry** — assign an owner, set a "review by" date, nag when stale. _(Notion, Confluence)_
- **#11 — Graph view** — visual node-graph of links & backlinks (backlinks already exist; this draws them). _(Obsidian, Nuclino)_
- **#13 — Related "see also" pages** — auto-suggest related pages by links + similarity. _(Nuclino, Slab)_
- **#14 — Block-level references** — link / transclude a specific block by a stable id. _(Roam, Logseq)_
- **#15 — Canvas / infinite whiteboard** — spatial board of cards + live note embeds (pairs with Excalidraw). _(Obsidian Canvas)_

## Search & AI

- **#17 — "Ask your docs" (cited answers)** — natural-language question → answer cited from the wiki (RAG over FTS5 + LLM). _(Outline, Rovo, kapa.ai)_
- **#18 — Semantic / vector search** — embedding search alongside FTS5 keyword search. _(emerging — sqlite-vec)_
- **#20 — AI auto-summary at top of page** — one-click, regenerable TL;DR block. _(Notion, Nuclino)_
- **#21 — AI search-gap finder** — mine unanswered queries → suggest pages to write. _(Mintlify, Slite)_
- **#22 — Conversational search (follow-ups)** — multi-turn chat grounded in the wiki. _(Algolia Ask AI)_

## Structured data

- **#28 — Forms → page / row creation** — a form feeds a collection (intake without editor access). _(Notion, Confluence)_
- **#30 — Charts from page data** — bar / line / etc. over page property values. _(Notion, Confluence)_

## Publishing & docs

- **#40 — PDF / static export of a space** — export a page tree to PDF / HTML / zip. _(GitBook, Confluence)_

## Org & admin

- **#45 — Audit log (incl. agents)** — who did what, when — humans and agents alike. _(Confluence, Notion)_
- **#46 — Per-page view counts & trends** — reads per page; popular vs. dead pages. _(Confluence, GitBook)_
- **#47 — Polish transactional emails (visual)** — the verify/reset emails (`internal/mailer/templates.go`)
  are functional inline-hex HTML, deliberately minimal. Make them a proper branded visual: logo/wordmark
  lockup, refined type + spacing, light/dark-aware, tested across clients (Gmail/Apple/Outlook). Email can't
  use the OKLCH tokens, so translate the palette to an email-safe set. _(Cagdas — post-launch polish)_
- **#49 — SSO / OIDC / SCIM** — external identity + provisioning (unlocks larger self-host teams). _(enterprise)_

## Automation & integrations

- **#54 — Smart links / rich unfurls** — paste a URL → live card (Jira, GitHub, Figma). _(Confluence, Outline)_
- **#55 — Slack / chat integration** — search & post wiki content from chat; notify on changes. _(Outline, Slab)_

## Agent-native

- **#62 — Wiki-as-agent-memory** — a first-class store/recall API so agents persist memory in pages.
  **Note (Cagdas):** would be great for **sharing memory across devs in the team** — a shared, durable
  agent memory everyone's agents read and write. _(Notion 3.0 "memory in pages")_
- **#65 — "Context bundle" retrieval primitive** — one MCP call returns a page + its backlinks + related
  pages as a ready-made context pack for an agent. _(novel)_
