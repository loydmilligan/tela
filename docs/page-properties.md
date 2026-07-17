# Page properties / frontmatter

Status: **shipped (Phases 1–2).** Frontmatter is a first-class **page-properties**
system: `pages.props JSONB` + GIN (migration `0005_page_props.sql`), read/written
end-to-end (`pages.go` `createPageCore`/`applyUpdateTx`/`getPageCore`), imported
from frontmatter via `pagemd.Decode` (dropping only the reserved keys; the old
`StripFrontmatter` regex behaviour below is history), and carried across MCP
`get_page`/`create_page`/`update_page`. On top of it, two blocks read props live
in the page: the **field block** (` ```field `) — an interactive widget that
writes a value back to a page prop (via `PATCH /api/pages/{id}/props`) — and the
**query block** (` ```query `) — a live table of other pages filtered by their
props (see "Querying props" below). Phase 3 (human-facing props panel, graph
coloring, FTS folding) is the remaining deferred work.

The rest of this doc is the authoritative design: it holds for the shipped system
except where a subsection is marked deferred.

This doc specifies frontmatter as a single source of truth for page metadata that
import/sync populates automatically and that agents, search, and the graph can
query — without violating the `pages.body` "canonical markdown forever" rule.


## Goals

1. **Stop discarding.** Imported frontmatter is preserved, not dropped.
2. **Single source of truth.** Each piece of metadata has exactly one owner —
   either a tela column or the property bag — never both.
3. **Clean ser/deser, non-hacky queries.** Storage is idiomatic Postgres
   (JSONB + GIN), not embedded YAML text and not an EAV join-fest.
4. **Round-trip.** db → frontmatter-text reproduces a valid, value-faithful YAML
   block (canonical key order; comments / exact ordering are not preserved).
5. **Agent-first payoff.** Agents can filter pages by property
   (`list_pages` where `status=draft`) instead of reading every body.

## Why not raw frontmatter in `pages.body`

`pages.body` is canonical markdown for the page's **content**. Frontmatter is
metadata *about* the page, not content — keeping it inline would force a YAML
parse on every query and tangle metadata edits into the Yjs collab document.

Instead, properties follow the **comments precedent**: comments are already
"SQL-only, decoupled from body/Yjs" (`0001_init.sql`). Properties ride the same
lane — structured, edited via REST, not part of the collaborative doc. You don't
need real-time OT on a status dropdown. The body stays pure prose.

This also mirrors `search_tsv`: a queryable projection of the page, derived and
indexed, that is never a second source of truth.

## Representation

```sql
-- 0005_page_props.sql
ALTER TABLE pages ADD COLUMN props JSONB NOT NULL DEFAULT '{}';
CREATE INDEX idx_pages_props ON pages USING GIN (props jsonb_path_ops);

ALTER TABLE page_revisions ADD COLUMN props JSONB NOT NULL DEFAULT '{}';
```

- **Ser/deser** is symmetric and built on the existing `gopkg.in/yaml.v3` dep:
  YAML → `map[string]any` → JSONB on the way in; JSONB → `map[string]any` →
  `yaml.Marshal` on the way out. No custom format, no regex.
- **Queries are idiomatic, GIN-indexed Postgres** — not joins:
  - `WHERE props @> '{"status":"draft"}'` — containment
  - `WHERE props->>'owner' = 'cagdas'` — scalar
  - `WHERE props->'aliases' ? 'rfc'` — array membership
  - `ORDER BY (props->>'due')::date` — typed sort (add an expression index for
    hot keys)
- **EAV (`page_properties(page_id,key,value)`) is rejected** — multi-predicate
  filters become self-joins; that is the hacky/slow path JSONB avoids.

## Querying props — the query block

A ` ```query ` block (a Dataview analog) renders a live table of pages filtered
by their props. It is the read-facing payoff of the GIN index: `props @> where`
containment is indexed, so a dashboard page listing "every `type: incident`"
stays cheap.

**Spec (v1, deliberately small — equality/containment only, no operators):**

```yaml
where: { type: incident, status: active }   # props @> containment
space: here            # 'here' (this space) | <space id> | omit = all readable
columns: [title, status, updated]           # title/space/created/updated or a prop key
sort: -updated         # (-)updated | (-)created | (-)title  ('-' = descending)
limit: 25
```

**Backend — `POST /api/pages/query`** (`api/pages_query.go` `QueryPages` /
`queryPagesCore`). Session-gated. The frontend parses the block's YAML and POSTs
the structured spec; the handler returns the matching page rows.

- **Access (docs/access-model.md invariant 4 — one resolution path):** every row
  is JOINed to `(SELECT DISTINCT space_id FROM space_access WHERE user_id = $1)`,
  so a query can **never** surface a page from a space the caller can't read. An
  API-key-scoped caller is additionally confined to its one space.
- **Injection-safe by construction:** the `where` bag binds as `$N::jsonb`; the
  sort key is looked up in a fixed `key → ORDER BY fragment` map (unknown → 400,
  never interpolated); the limit is a bound, clamped int.
- **`space: here`** resolves to the block's own page space server-side (from the
  `page_id` the frontend sends), still AND-gated by `space_access`.
- `where` matches **props only** (containment) — not columns like `title`, which
  live outside the bag. Empty `where` lists every readable page.

**Frontend** — the read-view widget (`QueryBlockView` in `MarkdownView.tsx`)
renders the result as an owned-token table with explicit loading / empty / error /
signed-out states; the editor shows a static preview of the spec
(`milkdown-query.ts`, spec parser in `lib/blocks/query-spec.ts`). Manifest entry
`query` (agent-authorable).

**Agents — `query_pages`** (`api/mcp_props_query.go` `mcpQueryPages`) is the MCP
twin of this endpoint: same `queryPagesCore`, same `space_access` join, so an
agent gets the identical rows the block does. It exposes `where` / `space_id` /
`sort` / `limit`, but **not** the block's `columns` (a render-side concern of the
table — the tool returns the whole row and the agent picks) and not `space:
"here"` (there is no page context in a tool call).

### Querying comments — `target: comments`

The same ` ```query ` block takes `target: comments` (`api/comments_query.go`
`queryCommentsCore`, `POST /api/comments/query`, MCP `query_comments`). It filters
**comment** props instead of page props and returns comment rows (body, author,
the page it lives on). Keep the lanes straight: **`pages.props` is a page's own
data; `comments.props` is metadata about a timestamped, authored event on it.**

It is a **sibling core, not a flag on `queryPagesCore`**: the two return different
row shapes, and both Go's typed rows and the MCP typed output schema want one
concrete type per tool. The *block* still exposes a single `query` surface and
routes to the right endpoint — unified authoring, typed backend.

Same access gate, one level indirect: JOIN `comments → pages → space_access`, so a
comment is visible only if its **page** is readable.

Two scoping knobs, deliberately distinct:

- `space:` — `here` | id | omit, exactly as for pages.
- `page:` — `here` | id | omit. Scopes to **one page's** comments (the changelog
  case). It is separate from the `page_id` the block sends as *context* for
  resolving `here`; conflating them would silently narrow every in-page comment
  block to that page's own comments even when the author asked for the whole
  space.

A change-comment's headline prop is **`change_summary`**, never `summary` —
`props.summary` is the page's own abstract (see the auto-summarizer above), and
sharing the key would invite conflating a page's description with a record of
what changed in it.

## Reserved-key policy (the actual spec)

Frontmatter is **not** a greenfield bag — several conventional keys map onto
things tela already owns. Model every key along two axes: does it come **IN**
from frontmatter, and does it go **OUT** to frontmatter?

| key | accepted IN? | written OUT? | source of truth |
|---|---|---|---|
| `id` | no | yes | `pages.id` |
| `slug` | no | yes | derived `pageSlug(title)` |
| `link` / URL | no | yes | derived `id` + title |
| `created` | no | yes | `created_at` column |
| `updated` | no | yes | `updated_at` column |
| `title` | seed-only (import precedence; never via the bag) | yes | `title` column |
| `space`, `parent`, `position` | no | **no (for now)** | columns |
| `published`/`draft`/`tags`/`*` | yes (bag) | yes (bag) | the bag |

The **emit-only set** — `id`, `slug`, `link`, `created`, `updated` — only ever
flows *out*: never accepted as input, always (re)written from the source of truth
when we generate frontmatter text. `title` is the near-exception (emitted from
the column; its only input path is the import title-precedence seed).

> **Silent-drop rule (the consistency guarantee).** If inbound frontmatter
> contains *any* reserved key (`id`, `slug`, `created`, `title`-as-bag-key, …) it
> is **silently dropped** — not stored, not errored. It isn't authoritative and
> will be regenerated on emit. So importing a file with `id: 999` or a hand-edited
> `slug:` is safe: the value is discarded in, the real one written back out. The
> reserved-key list is a **fixed namespace**; everything outside it is free-form.

The three tiers below are these same keys grouped by ownership.

### Tier 1 — column-derived
`title`, `created`/`date`, `updated`/`modified`, `slug`, `link`/canonical URL,
`id`.

- **Source of truth is a tela column** (`title`, `created_at`, `updated_at`) or a
  pure derivation (`slug` = `pageSlug(title)` from `slug.go`; `link` =
  `tela://page/{id}` + public URL).
- **Stripped on import** — they never enter the bag (storing a stale `created`
  that disagrees with `created_at` is exactly the double-source-of-truth we are
  avoiding). `title:` is still consumed as a **seed** through the existing import
  precedence (frontmatter → first H1 → filename; index pages use dir basename).
- **Synthesized on emit** from the columns, so exported frontmatter always shows
  correct, consistent values.

> Decision (with PO): `created_at`/`updated_at` stay 100% native tela. Frontmatter
> dates do **not** seed or drive them on import — they are only *produced* on the
> db → frontmatter-text conversion.

### Tier 2 — column-owned, ignored from frontmatter
`position`, `parent`, `space`.

Tree, ordering, and space come from tela (the import flatten / README-as-index
rules and `MAX(position)+1`). Frontmatter cannot drive them. **Not accepted in,
and not emitted out for now** (decision: keep the emitted block lean; revisit if
portability needs them).

### Tier 3 — free-form bag
Everything else, stored verbatim in `props`, queryable and round-tripped.

- `published` / `draft` / `public` — **no tela equivalent; no mapping.** They sit
  in the bag as ordinary data: stored, round-tripped, **never interpreted**. This
  is deliberate — interpreting them against `exposure`/`share_links` would be an
  accidental-publish footgun on import. Safety here comes from *not acting*, not
  from a guard.
- `tags` — stored in the bag for now (see Deferred).
- arbitrary user/agent keys — the queryable long tail.

## Two operations

- **import (frontmatter-text → db):** `yaml.v3` parse → silent-drop every
  reserved key (Tier 1 & Tier 2) → store the remainder in `props`. Title seeds via
  existing precedence.
- **emit (db → frontmatter-text):** synthesize the emit-only set + `title` from
  columns (`id`, `title`, `slug`, `link`, `created`, `updated`) + splice the
  `props` bag → canonical YAML block (key order: system keys first, then bag keys
  alphabetical), prepended to the body. This is where `slug`/`link` "fill in".
  **The system block is always emitted, even when the bag is empty** (consistent,
  round-trip-portable). `space`/`parent`/`position` are **not** emitted.

### The body invariant (every ingress)

**Frontmatter never lives in `pages.body` — at any ingress, not just import.**
`createPageCore`/`updatePageCore` reuse `StripFrontmatter` on the incoming `body`
exactly like import does (and like `stripLeadingTitleH1` already strips a leading
H1). This is the *lower-debt* choice: the parser already exists, and it removes
the path-asymmetry where the same markdown stored via import vs API would land
differently. Precedence when a request carries both:

> **explicit `props` field > frontmatter found in `body` > existing props.**
> An explicit `props` field is authoritative; otherwise frontmatter in the body
> is absorbed; the stored body is always pure prose.

### Detection & parse rules (locked)

- **Frontmatter = "parses to a YAML mapping," else it is not frontmatter.** Only
  strip/store when the delimited block `yaml.v3`-parses to an *object*. This one
  rule prevents malformed YAML from crashing import AND stops a legitimate leading
  `---` thematic break (`---\nsome text\n---`) from being mis-eaten.
- **JSON-safe coercion (accepted normalization).** Parsed YAML is coerced to
  JSON-safe (string keys) before storage. Value-faithful, not byte-faithful:
  YAML timestamps normalize to RFC3339 (`due: 2026-01-01` → `2026-01-01T00:00:00Z`)
  and numbers to JSON numbers. Known wrinkle, consistent with the canonical-emit
  decision.

### Update semantics — Replace (PUT)

`update_page(props=…)` and `PATCH /api/pages/{id}` with `{props:…}` **replace the
whole bag**, mirroring how `title`/`body` already overwrite in `updatePageCore`.
No null-sentinel/merge convention. The update guard (today "at least one of title,
body") is relaxed to allow a **props-only** update.

### Single-key merge — `PATCH /api/pages/{id}/props`

The targeted `set_prop` verb foreshadowed above is **shipped**
(`api/page_props.go` `SetPageProp` / `setPagePropCore`). It writes exactly one key
with a **server-side shallow-merge**, not Replace:

```
PATCH /api/pages/{id}/props     body: { "key": "result", "value": "pass" }
UPDATE pages SET props = props || $1::jsonb, updated_at = tela_now() WHERE id = $2
```

- **Race-safe.** The merge is atomic per-statement, so one field flip can't clobber
  a concurrent write to a *different* key (the failure mode of "GET the bag, merge
  one key client-side, PATCH it all back" against the Replace endpoint). Same-key
  writes are last-write-wins — fine for a UAT toggle; no optimistic-concurrency
  token in v1.
- **Editor-gated, in-tx.** Same access path as every page mutation:
  `selectPageByIDTx` → `requireEditTx` (viewer/non-member → `403`).
- **Reserved keys rejected** (`400`), not silently dropped — a targeted verb is
  explicit about what it won't write (`pagemd.FilterReserved`).
- **Churn-free**, like the poll vote: no revision snapshot, no notification, no
  reindex. Unlike the poll vote it does **not** reset the Yjs room — props ride the
  comments lane (REST-only, outside the collaborative doc), the body is unchanged,
  and readers refresh via query invalidation, so there is no overlay to drop.

This is the write-back path for the **bound-field block** (` ```field `): a block
that renders an interactive widget bound to `props[key]`, persisting on
interaction (see `blocks-manifest.json` `field`, `FieldWidget.tsx`).

**Agents — `set_prop`** (`api/mcp_props_query.go` `mcpSetProp`) is the MCP twin of
this endpoint, calling the same `setPagePropCore`. It exists because the only
other MCP path to a prop is `update_page(props=…)`, which Replaces the bag
(above) — so an agent flipping one key would wipe every key it didn't resend,
including the ones the shipped `field` blocks read. Its tool description says so
explicitly; that steer is the point of the tool. It returns the **full merged
bag**, so a caller can see its siblings survived in the same round trip.

## Versioning — capture on snapshot

`page_revisions` (today `title + body` only) gains a `props` column. There is a
single revision write point (`page_revisions.go` `insertPageRevision`, the manual
snapshot path). Props are **captured whenever a snapshot is taken** — a
props-only edit does not itself force a new revision. This is the honest v1 scope;
"snapshot on every props edit" is deferred.

## Product-wide change map

| Surface | Change |
|---|---|
| **Schema** | `0005_page_props.sql`: `pages.props` + GIN; `page_revisions.props`. |
| **Import** (`mdimport/frontmatter.go`, `markdown.go`) | `StripFrontmatter` → full `yaml.v3` parse returning `(body, props)`; title still seeds via existing precedence; persist `props`. |
| **Emit** (new helper) | `EmitFrontmatter(page)` — emit-only set (`id`/`title`/`slug`/`link`/`created`/`updated`) from columns + bag, canonical order; always emits the system block. |
| **Model / CRUD** (`models.go`, `pages.go`) | `Page.Props`; create/update accept props (Replace) + absorb body frontmatter (invariant); props-only guard; revision writes capture props. Only `selectPageByID`/`selectPageByIDTx` scan the full model — the other ~13 `FROM pages` reads are narrow and untouched. |
| **MCP** (`mcp_tools.go`, `mcp_props_query.go`) | `get_page`/`create_page`/`update_page` carry props. The agent payoff shipped as two dedicated twins rather than as flags on `list_pages`: `set_prop` (single-key merge) + `query_pages` (containment `props @>`), each mirroring its REST route. Rides the REST cores — no duplicate logic. |
| **Egress** | Optional frontmatter-on flag in MCP `get_page`/`fetch`; a real `.md` export does not exist yet (open question). |
| **Graph / FTS / FE panel** | Deferred (Phase 3); seams left. |

## Implementation gotchas (verified against the code)

- **`yaml.v3` is a transitive dep, not direct** — `go get gopkg.in/yaml.v3` to
  promote it in `go.mod` before importing.
- **pgx JSONB binding** (per the CLAUDE.md pgx-strictness gotcha): write props as a
  **JSON string with `::jsonb`**, not `[]byte` (pgx encodes `[]byte` as `bytea` →
  type error). Read via `[]byte` + `json.Unmarshal`.
- **`StripFrontmatter` signature change** 2-return → 3-return
  `(body, title, props)`; the `insertPage` closure (`markdown.go:313`) + 3 call
  sites thread `props`. **Index/README pages:** the README's props attach to the
  dir-index page, but dir-basename still wins for the *title*.
- **Test fallout:** `frontmatter_test.go`, `markdown_test.go`, import tests. The
  locked behaviors must still pass — dir-name title override, title precedence,
  frontmatter-stripped-from-body, `(2)/(3)` dedupe.

## Phasing

- **Phase 1 — store + don't lose.** Schema, import parse, model/CRUD, revisions.
  After this nothing is discarded and it is all in the DB. Small and safe.
- **Phase 2 — agent reach.** MCP props in/out, `list_pages` filtering, emit
  helper. This is where it becomes *useful*.
- **Phase 3 — human reach.** FE properties panel (Notion/Obsidian-style, above
  the Milkdown doc, edits via REST — comments lane, no Yjs), graph coloring by
  property, tags-table promotion, optional prop-value folding into FTS.

## Deferred / open

- **Tags as a feature.** Tags-as-*data* are free in the bag from Phase 1
  (`props->'tags' ? 'x'`). Tags-as-a-*feature* — relational `tags`/`page_tags`,
  graph edges, clustering, the legend filters, a tag picker — is a later,
  non-destructive migration (backfill `page_tags` from `props->'tags'`). No
  regret cost in deferring.
- **Markdown export.** "Emit real frontmatter" implies a `.md` download path that
  does not exist today. Decide whether Phase 2 adds one or only reattaches
  frontmatter in the MCP egress.
- **FTS over property values.** `search_tsv` is title/body only. Folding selected
  prop values in (so `status: blocked` is findable) is a Phase 3 call.
- **Typing/coercion.** Frontmatter scalars are loose (strings, lists, dates,
  bools). v1 keeps them as parsed by `yaml.v3`; revisit if typed filters need
  stricter coercion.
</content>
</invoke>

## The untyped-`value` bug — a schema that could not constrain anything

Shipped and live for one day (PR 4, 2026-07-16 → 07-17). Worth recording in full,
because the fix is a declaration and the lesson is not.

**Symptom.** `set_prop` via MCP could only ever write **strings**. Observed on
prod, three for three through a real client:

| sent | stored |
| --- | --- |
| `["tela","agents"]` | `"[\"tela\",\"agents\"]"` |
| `42` | `"42"` |
| `true` | `"true"` |

**Why it was worse than a type slip.** A string-typed value can never match
`props @> {"tags":["tela"]}`, never compare numerically, never sort. So the query
block, `query_pages`, and every dashboard built on containment silently returned
**fewer rows than the truth** — no error, no warning. `set_prop`'s description
promised the value is "stored verbatim as JSON so props containment filters stay
predictable"; that was the one guarantee it made and the one it broke. It could
not do the thing it advertised, and never could.

**Where it was NOT.** Not the server. `setPagePropCore` marshals `value any`
through `json.Marshal` and stores it verbatim — no coercion anywhere
(`page_props.go`, `propsJSON` in `pages.go`). A Go test passing a real `bool`
through a real `mcp.Client` over a real transport round-trips correctly. Every
other write path — the sheet editor, imports, `update_page` — preserved genuine
bools, arrays and numbers *during the same window*. Three separate diagnoses put
the bug in the server; all three were wrong.

**Where it was.** `setPropIn.Value` is `any`, which reflects to a schema property
carrying a description and **no `type`**:

```json
"value": { "description": "string, number, boolean, null, or a nested object/array; stored verbatim as JSON…" }
```

The type rule lived in **prose**. A client cannot validate or serialize against
prose. Handed an untyped field, it must guess how to encode the value — and it
guessed string, every time, for everything. **A schema that looks like a contract
and is structurally incapable of constraining anything.**

The sibling fields prove the SDK emits unions perfectly well when told to:
`query_pages`' `space_id` reflects to `["null","integer"]`. The declaration was
simply never written.

**The fix.** Hand-write `set_prop`'s `InputSchema` (`setPropInputSchema` in
`mcp_props_query.go`) declaring the union in the field a machine reads:
`["string","number","boolean","object","array","null"]`. The SDK uses an explicit
`InputSchema` as-is and only reflects when it is nil (`setSchema`, go-sdk
`server.go`).

**Why there is no test for the bug itself — and why that is the right answer.**
The coercion happens inside a client *outside this repo*. Nothing here can make
that client guess on demand. A server-side round-trip test constructs a real Go
`[]any` and hands it to the SDK — but the real client never sends a real array,
*which is the entire bug*, so such a test stays green forever while prod fills
with `"[\"tela\",\"agents\"]"`. Its scope (the server stores what it is given) is
narrower than its claim (values round-trip and stay queryable).

So the guard is `TestMCP_SetPropSchemaDeclaresValueType`: it asserts the
**published** schema declares a type union for `value`. It cannot watch the client
lie; it pins the contract whose absence forces the guess. **A schema that cannot
lie beats a test that watches it lie.** Verified by negative control — with the
explicit schema removed, the test fails and prints the untyped `value` as its
diagnostic.

**When adding any MCP tool:** an `any`/`interface{}` field reflects to an empty
constraint. If a field accepts more than one JSON type, declare the union
explicitly. Prose in a description is documentation, not a contract.

### The test that should have caught it

`mcp_props_query_test.go` had a subtest named **"non-string values round-trip
verbatim"** whose body tested exactly one `bool`. **Its name certified all
non-string values; its body checked one.** It passed for the same reason the bug
survived: it exercised the server, which was never the thing under test. Both it
and the array subtest are now named for the claim their bodies can actually reach
("the server stores a bool/array verbatim").

The recurring shape, seen eight times across the fleet in one day: **a
verification narrower than the claim it certified**. Empty output is not evidence
of absence; a passing assertion is not a working feature. Ask not "did the check
pass" but "is the check as wide as the sentence I am about to write". Knowing the
antipattern buys no immunity — the first draft of the guard *for this bug* was
itself a fresh instance of it.
