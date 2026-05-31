// Compiles the tela feature-ideas board into a mira spec and POSTs it to
// https://mira.cagdas.io/v1/render. Prints the permanent URL.

// ── block helpers ───────────────────────────────────────────────────────────
const T = (s, ann) => ({ type: 'text', text: { content: String(s) }, ...(ann ? { annotations: ann } : {}) });
const rt = (s) => (Array.isArray(s) ? s : [T(s)]);
// normalize a span that may be a string, a single text-segment object, or an array
const seg = (x) => (Array.isArray(x) ? x : typeof x === 'string' ? [T(x)] : [x]);
const spans2rt = (spans) => spans.flatMap(seg);
const h1 = (s) => ({ type: 'heading_1', heading_1: { rich_text: rt(s) } });
const h2 = (s) => ({ type: 'heading_2', heading_2: { rich_text: rt(s) } });
const h3 = (s) => ({ type: 'heading_3', heading_3: { rich_text: rt(s) } });
const p = (...spans) => ({ type: 'paragraph', paragraph: { rich_text: spans2rt(spans) } });
const div = () => ({ type: 'divider', divider: {} });
const callout = (emoji, ...spans) => ({ type: 'callout', callout: { icon: { type: 'emoji', emoji }, rich_text: spans2rt(spans) } });
const cell = (s) => (Array.isArray(s) ? s : [T(String(s))]);
const trow = (...cells) => ({ type: 'table_row', table_row: { cells: cells.map(cell) } });
const table = (headers, rows) => ({
  type: 'table',
  table: {
    table_width: headers.length,
    has_column_header: true,
    has_row_header: false,
    children: [trow(...headers), ...rows.map((r) => trow(...r))],
  },
});
const statGrid = (title, tiles) => ({ type: 'stat_grid', stat_grid: { title, columns: 'auto', tiles } });
const kanban = (title, columns) => ({
  type: 'kanban',
  kanban: {
    title,
    columns: columns.map((c) => ({
      ...c,
      cards: c.cards.map((card) => ({ ...card, description: rt(card.description) })),
    })),
  },
});
const bold = (s) => [T(s, { bold: true })];

// ── data ────────────────────────────────────────────────────────────────────
const THEMES = [
  ['Authoring & editor', [
    ['Templates & scaffolds', 'insert pre-structured pages from a gallery', 'Notion, Confluence', 'S'],
    ['Math (KaTeX) rendering', 'render equations inline (Mermaid: preserved, render soon)', 'Obsidian, Docusaurus', 'A'],
    ['Tabs & code-group blocks', 'tabbed content / multi-language code switch', 'Mintlify, Docusaurus', 'A'],
    ['Synced blocks / transclusion', 'reuse one block across pages, edit once', 'Notion, Roam', 'A'],
    ['Page-include / excerpt macro', 'embed another page or a named excerpt', 'Confluence', 'A'],
    ['Inline AI writing', 'select text → rewrite / expand / summarize', 'Notion AI, Rovo', 'A'],
    ['Multi-column layout blocks', 'side-by-side content columns', 'Notion, Craft', 'B'],
    ['Daily notes / journal', 'auto-dated default capture page', 'Roam, Logseq', 'B'],
  ]],
  ['Knowledge & linking', [
    ['Verified / trusted pages', 'owner-verified badge with an expiry', 'Notion, Slite', 'S'],
    ['Page owners + freshness/expiry', 'assign owner; "review by" date; nag stale', 'Notion, Confluence', 'S'],
    ['Graph view', 'visual node-graph of links & backlinks', 'Obsidian, Nuclino', 'A'],
    ['Unlinked mentions', 'pages that name a title without linking', 'Obsidian, Roam', 'A'],
    ['Related "see also" pages', 'auto-suggest related by links + similarity', 'Nuclino, Slab', 'A'],
    ['Block-level references', 'link / transclude a specific block by id', 'Roam, Logseq', 'B'],
    ['Canvas / infinite whiteboard', 'spatial board of cards + live embeds', 'Obsidian Canvas', 'B'],
    ['Aliases / alternate titles', 'several names resolve to one page', 'Obsidian', 'B'],
  ]],
  ['Search & AI', [
    ['"Ask your docs" (cited answers)', 'NL question → answer cited from the wiki', 'Outline, Rovo, kapa.ai', 'S'],
    ['Semantic / vector search', 'embedding search alongside FTS5', 'emerging (sqlite-vec)', 'A'],
    ['Hybrid search (BM25 + vector + rerank)', 'fuse keyword + semantic + LLM rerank', 'emerging', 'A'],
    ['AI auto-summary at top of page', 'one-click, regenerable TL;DR block', 'Notion, Nuclino', 'A'],
    ['AI search-gap finder', 'mine unanswered queries → pages to write', 'Mintlify, Slite', 'A'],
    ['Conversational search (follow-ups)', 'multi-turn chat grounded in the wiki', 'Algolia Ask AI', 'B'],
    ['Suggested AI tags / auto-categorize', 'LLM proposes tags & placement', 'Notion, Coda AI', 'B'],
  ]],
  ['Structured data', [
    ['Page properties (frontmatter)', 'typed metadata per page (YAML, stays markdown)', 'Obsidian, Notion', 'S'],
    ['Saved filtered views', 'persisted query, e.g. "all stale runbooks"', 'Obsidian Bases, Coda', 'A'],
    ['Database / table views over pages', 'query by property; table / board / calendar', 'Obsidian Bases, Notion', 'A'],
    ['Supertags (typed objects)', 'a tag is a schema; pages can hold several', 'Tana', 'B'],
    ['Forms → page/row creation', 'a form feeds a collection', 'Notion, Confluence', 'B'],
    ['Relations & rollups', 'link rows across DBs; aggregate values', 'Notion, Coda', 'C'],
    ['Charts from page data', 'bar / line over property values', 'Notion, Confluence', 'C'],
  ]],
  ['Collaboration', [
    ['Suggestion / edit-review mode', 'propose edits as suggestions; accept/reject', 'Confluence, GitBook', 'A'],
    ['Threaded + resolvable comments', 'threads, resolve, @ on anchored comments', 'Confluence, Notion', 'A'],
    ['@mentions → notifications', 'mention a person → their inbox', 'all', 'A'],
    ['Notification inbox / activity feed', 'per-user feed of changes & mentions', 'Notion, Outline', 'A'],
    ['Presence + live-cursor polish', 'avatars, cursor labels, "X is editing"', 'Yjs (have it)', 'B'],
    ['Reactions / emoji on blocks', 'lightweight ack on content', 'Notion, Slab', 'C'],
  ]],
  ['Publishing & docs', [
    ['Publish a space as a docs site', 'space → clean public site w/ nav + search', 'GitBook, Notion Sites', 'A'],
    ['Git sync (bi-directional)', 'wiki content ↔ a git repo of markdown', 'GitBook', 'A'],
    ['llms.txt + markdown content negotiation', 'serve clean markdown to agents & LLMs', 'Mintlify, GitBook', 'A'],
    ['PDF / static export of a space', 'export a tree to PDF / HTML / zip', 'GitBook, Confluence', 'B'],
    ['Versioned docs (branches = versions)', 'maintain v1 / v2 doc trees', 'GitBook, Docusaurus', 'B'],
    ['OpenAPI → auto API reference', 'import spec → generated endpoint docs', 'GitBook, Mintlify', 'B'],
    ['Custom domain + theming', 'branded public docs', 'GitBook, Notion', 'C'],
  ]],
  ['Org & admin', [
    ['Space analytics ("mission control")', 'inactive pages, stale owners, view trends', 'Confluence', 'A'],
    ['Audit log (incl. agents)', 'who did what, when — humans and agents', 'Confluence, Notion', 'A'],
    ['Per-page view counts & trends', 'reads; popular vs dead pages', 'Confluence, GitBook', 'B'],
    ['Content lifecycle / archive policies', 'auto-archive pages past freshness', 'Slite, Confluence', 'B'],
    ['Granular page/space permissions', 'restrict view/edit at page level', 'Confluence, Outline', 'B'],
    ['SSO / OIDC / SCIM', 'external identity + provisioning', 'enterprise', 'B'],
    ['Bulk ops + trash & restore', 'multi-select move/delete; soft-delete', 'Outline, Notion', 'B'],
  ]],
  ['Automation & integrations', [
    ['Outbound webhooks on events', 'fire HTTP on page create / edit / comment', 'Coda, Notion', 'A'],
    ['Scheduled / cron page jobs', 'recurring action, e.g. a weekly digest page', 'Notion automations', 'B'],
    ['Native automations (if-this-then)', 'rules: on status change → notify / assign', 'Notion, Confluence', 'B'],
    ['Smart links / rich unfurls', 'paste a URL → live card (Jira, GH, Figma)', 'Confluence, Outline', 'B'],
    ['Slack / chat integration', 'search & post wiki content from chat', 'Outline, Slab', 'B'],
    ['Issue-tracker integration', 'two-way links to GitHub / Jira issues', 'Confluence, GitBook', 'B'],
  ]],
  ['★ Agent-native (tela’s wedge — mostly only-tela)', [
    ['PR-style agent edit review', 'agent edits land as proposals a human approves', 'novel', 'S'],
    ['Auto-hosted MCP per space + llms.txt', 'expose any space as a ready MCP endpoint', 'Mintlify/GitBook + your MCP', 'S'],
    ['Agent activity feed', 'timeline of every agent action (tool, page, diff)', 'novel', 'S'],
    ['@mention-an-agent in a comment', 'comment "@agent do X" → it acts on that anchor', 'novel', 'S'],
    ['Agent-maintained pages', 'a page declares "an agent keeps me current"', 'Mintlify Workflows', 'A'],
    ['Wiki-as-agent-memory', 'first-class store/recall so agents persist memory', 'Notion 3.0 memory', 'A'],
    ['Per-agent scoped identity & views', 'each agent = an identity, authored-by, scope', 'novel', 'A'],
    ['MCP tool-call analytics', 'dashboard of which tools/pages agents hit', 'Mintlify', 'A'],
    ['"Context bundle" retrieval primitive', 'one call → page + backlinks + related pack', 'novel', 'A'],
    ['Human-in-the-loop approval gates', 'policy: some MCP writes need a human OK', 'novel', 'A'],
    ['Citation-enforced agent answers', 'answers must cite tela pages / anchors', 'kapa.ai, Mintlify', 'B'],
    ['Agent task queue / async jobs', 'hand an agent a long task; it reports back', 'Notion Agents', 'B'],
  ]],
  ['Portability & self-host', [
    ['Full markdown export / Git mirror', 'export the whole instance as markdown + assets', 'GitBook, Obsidian', 'S'],
    ['Importers (Notion / Confluence / Obsidian)', 'one-shot import from the major tools', 'Outline, Notion', 'A'],
    ['Single-binary / one-command deploy', 'binary + embedded assets, trivial deploy', 'self-host norm', 'A'],
    ['Backup / snapshot & restore', 'scheduled SQLite snapshots + restore UI', 'self-host norm', 'B'],
    ['Local-first / offline editing', 'edit offline, sync later (Yjs is offline-able)', 'Obsidian, Anytype', 'B'],
    ['End-to-end encrypted spaces', 'optional E2EE for sensitive spaces', 'Anytype', 'C'],
  ]],
];

// each entry references an exact feature name from THEMES (so it inherits the board #)
const TOP12 = [
  ['PR-style agent edit review', 'Lets teams trust agents to write — no rival does this well. Category-defining.'],
  ['"Ask your docs" (cited answers)', 'The one AI feature every rival ships; FTS5 + LLM gets 80% cheaply.'],
  ['Auto-hosted MCP per space + llms.txt', 'Turns "agent-native" from a claim into a one-click, demoable surface.'],
  ['Verified / trusted pages', 'Cheap, high-trust; fixes the universal stale-wiki rot (pair with owners + freshness).'],
  ['Page properties (frontmatter)', 'Foundation for structured data, saved views & dashboards — stays pure markdown.'],
  ['Agent activity feed', 'Makes agent behaviour visible and measurable (+ MCP analytics). Only tela can build it.'],
  ['@mention-an-agent in a comment', 'Reuses anchored comments + MCP for a delightful delegate-in-context move.'],
  ['Full markdown export / Git mirror', 'Core self-host promise (no lock-in, doc-as-code) — nearly free.'],
  ['Graph view', 'You already store backlinks — drawing them is a demo magnet, low effort.'],
  ['Publish a space as a docs site', 'Extends share links into a GitBook/Notion-Sites rival; markdown = LLM-ready.'],
  ['Space analytics ("mission control")', 'Surfaces stale pages & inactive owners; Confluence’s standout admin win.'],
  ['Importers (Notion / Confluence / Obsidian)', 'Migration friction is the #1 reason teams don’t switch.'],
];

const KANBAN_COLS = [
  { name: 'Quick wins — ship first', accent: 'positive', cards: [
    { title: 'Tabs, code-groups & math (KaTeX)', description: 'The remaining authoring gaps (slash, callouts, toggle already ship).' },
    { title: 'Templates & scaffolds', description: 'Markdown templates — runbooks, RFCs, postmortems.' },
    { title: 'Math (KaTeX) + Mermaid', description: 'Technical teams expect it; markdown-native.' },
    { title: 'Verified pages + freshness', description: 'Owner badge + "review by" date. High trust, low effort.' },
    { title: 'Page properties (frontmatter)', description: 'YAML metadata — the base for everything structured.' },
    { title: 'Full markdown / Git export', description: 'No lock-in; nearly free given canonical markdown.' },
  ] },
  { name: 'High impact — next', accent: 'info', cards: [
    { title: '"Ask your docs" (cited RAG)', description: 'RAG over FTS5 + an LLM, answers cite pages.' },
    { title: 'Saved filtered views', description: 'SQL over frontmatter → live dashboards.' },
    { title: 'Graph view', description: 'You already store backlinks — draw them.' },
    { title: 'Publish a space as a docs site', description: 'Extends share links; markdown = LLM-friendly.' },
    { title: 'Space analytics + lifecycle', description: 'Stale pages, inactive owners, auto-archive.' },
    { title: 'Importers + webhooks', description: 'Migration on-ramp + glue to anything.' },
  ] },
  { name: 'Bigger bets', accent: 'warning', cards: [
    { title: 'Database / table views over pages', description: 'Query pages by property — Obsidian Bases model.' },
    { title: 'Block references / transclusion', description: 'Granular reuse over canonical markdown.' },
    { title: 'Semantic / hybrid search', description: 'sqlite-vec + rerank alongside FTS5.' },
    { title: 'Versioned docs + OpenAPI reference', description: 'Owns the dev-docs use case.' },
    { title: 'SSO / OIDC + audit log', description: 'Unlocks larger self-host teams; compliance.' },
    { title: 'Local-first offline', description: 'Yjs is offline-able; resilience + "your data".' },
  ] },
  { name: '★ The agent-native wedge (only tela)', accent: 'default', cards: [
    { title: 'PR-style agent edit review', description: 'Agents propose; humans approve. The trust unlock.' },
    { title: 'Agent activity feed + MCP analytics', description: 'See & measure exactly what agents did.' },
    { title: '@mention-an-agent in a comment', description: 'Delegate a task in context, on a text anchor.' },
    { title: 'Auto-hosted MCP per space + llms.txt', description: 'One-click agent endpoint for any space.' },
    { title: 'Wiki-as-agent-memory', description: 'First-class store/recall — tela as durable memory.' },
    { title: 'Per-agent identity + context bundle', description: 'Attribution, least-privilege, optimized retrieval.' },
    { title: 'Human-in-the-loop approval gates', description: 'Policy gates on autonomous MCP writes.' },
  ] },
];

// ── assemble ────────────────────────────────────────────────────────────────
const blocks = [];

// stable, global feature numbers so they can be referenced back ("build #42").
let _n = 0; const idOf = {};
for (const [, rows] of THEMES) for (const r of rows) { _n++; idOf[r[0]] = _n; }
const total = _n;
const sCount = THEMES.flatMap(([, r]) => r).filter((r) => r[3] === 'S').length;
const agentCount = (THEMES.find(([nm]) => nm.includes('Agent'))?.[1] || []).length;

blocks.push(h1('tela — the feature ideas board'));
blocks.push(p(
  T('A roadmap board for '),
  ...bold('tela'),
  T(', the agent-native, self-hostable, markdown wiki. We mined the genuinely '),
  ...bold('nice'),
  T(' features from the alternatives (Notion, Confluence, Obsidian, Outline, Coda, GitBook, Mintlify, Tana, Roam/Logseq…), added fresh inspiration, and brainstormed the net-new agent-native moves only tela can make. Every idea is '),
  ...bold('numbered'),
  T(' so you can reply with the ones to build. Tiers: '),
  ...bold('S'), T(' ship-first · '), ...bold('A'), T(' high-impact · '), ...bold('B/C'), T(' later.'),
));
blocks.push(callout('✅',
  ...bold('Already in tela (not on this list): '),
  T('WYSIWYG markdown · a slash (/) command menu · callouts · collapsible / toggle blocks · code blocks · Excalidraw diagrams · live multiplayer (Yjs) · FTS5 full-text search + ⌘K palette · text-anchored comments · page history & revisions · public share links (optional password) · backlinks · bulk markdown import · spaces & nested pages · role-based access · PAT / REST API + the MCP server. The board below is what’s '),
  ...bold('not built yet'),
  T('.'),
));
blocks.push(callout('🧭',
  ...bold('The strategic read: '),
  T('tela’s highest-leverage, most unique wins cluster in the '),
  ...bold('agent-native'),
  T(' theme — agent edit-review, auto-hosted MCP per space, the agent activity feed, @mention-an-agent. Pair those "only-tela" plays with two fast parity bundles (verified-pages + freshness, ask-your-docs RAG) and the markdown/Git portability story that reinforces the self-host wedge.'),
));
blocks.push(statGrid('At a glance', [
  { label: 'Ideas to build', value: String(total), accent: 'info' },
  { label: 'Themes', value: '10', accent: 'default' },
  { label: 'Agent-native (only-tela)', value: String(agentCount), accent: 'positive' },
  { label: 'Ship-first (S-tier)', value: String(sCount), accent: 'positive' },
  { label: 'Sources verified', value: '2025–26', accent: 'info' },
]));

blocks.push(div());
blocks.push(h2('Top 12 — ranked'));
blocks.push(p(T('The '), ...bold('#'), T(' column is the board number — reply with those to pick.')));
blocks.push(table(
  ['Rank', '#', 'Feature', 'Why it wins'],
  TOP12.map(([name, why], i) => [String(i + 1), '#' + (idOf[name] || '?'), name, why]),
));

blocks.push(div());
blocks.push(h2('Roadmap, by horizon'));
blocks.push(p(T('The same ideas, grouped by when they’re worth doing. The fourth column is the wedge — the moves no competitor can copy.')));
blocks.push(kanban('Build order', KANBAN_COLS));

blocks.push(div());
blocks.push(h2('The full board'));
blocks.push(p(T('Every idea, '), ...bold('numbered'), T(' — reply with the # to pick. '), ...bold('Source'), T(' = where it’s mined from ("novel" = net-new). '), ...bold('Tier'), T(' is the overall pick.')));
for (const [name, rows] of THEMES) {
  blocks.push(h3(name));
  blocks.push(table(
    ['#', 'Feature', 'What it does', 'Source', 'Tier'],
    rows.map(([f, w, s, t]) => [String(idOf[f]), f, w, s, t]),
  ));
}

blocks.push(div());
blocks.push(callout('🛠️',
  ...bold('Next: '),
  T('reply with the numbers you want — to pull into the landing’s feature showcase (visual demos) and/or to actually build into tela. The S-tier + agent wedge are the obvious first cut.'),
));

const spec = { template: 'page', blocks, persistent: 'tela-feature-ideas' };

// ── publish ─────────────────────────────────────────────────────────────────
const body = JSON.stringify(spec);
const spans = JSON.stringify(blocks).match(/"type":"text"/g)?.length || 0;
console.error(`blocks=${blocks.length} rich_text_spans≈${spans} bytes=${body.length}`);

const res = await fetch('https://mira.cagdas.io/v1/render', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body,
});
const text = await res.text();
console.error('status', res.status);
console.log(text);
