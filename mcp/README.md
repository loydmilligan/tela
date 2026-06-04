# tela-mcp

MCP (Model Context Protocol) server for [Tela](https://tela.cagdas.io). Exposes spaces (read + CRUD), pages, search, backlinks, page writes, comments, bulk markdown import, mira-page import, and feedback submission to MCP-capable clients (Claude Code, etc.) over stdio.

The server is a tiny TypeScript process: it bearer-auths against the Tela REST API using a personal access token and translates each MCP tool call into one HTTP request. No state is held client-side.

## Quick install

```sh
npx -y tela-mcp@latest
```

(npm installs and runs the published binary on demand — there is no separate install step for the MCP host.)

## Configure

Add to your `.mcp.json` (Claude Code, Claude Desktop, or any MCP host that speaks stdio):

```json
{
  "mcpServers": {
    "tela": {
      "command": "npx",
      "args": ["-y", "tela-mcp@latest"],
      "env": {
        "TELA_BASE_URL": "https://tela.cagdas.io",
        "TELA_API_KEY": "tela_pat_..."
      }
    }
  }
}
```

Required env vars:

| Var             | Purpose                                                                            |
|-----------------|------------------------------------------------------------------------------------|
| `TELA_BASE_URL` | Origin of the Tela instance, e.g. `https://tela.cagdas.io` or `http://localhost:8780`. No trailing slash needed. |
| `TELA_API_KEY`  | Personal access token. Format `tela_pat_<43 base64url chars>`. Create one in **Settings → API Keys** (instance-admin only in v0). |
| `TELA_PUBLIC_URL` | *Optional.* Public origin users browse, used to build the `url` field on page tool results. Defaults to `TELA_BASE_URL` — only set it when the API is reached at an internal host that differs from the public one. |

`TELA_BASE_URL` and `TELA_API_KEY` must be set at spawn time. If either is missing the server logs to stderr and exits non-zero before the MCP handshake.

Page tools (`get_page`, `create_page`, `update_page`) return a `url` field — the human-shareable in-app link `{TELA_PUBLIC_URL}/spaces/{spaceId}/pages/{pageId}/{slug}` (the slug is cosmetic; the id resolves the page).

A `tela://page/{id}` resource template is also registered, matching the wikilink scheme Tela writes into markdown bodies — resource @-mentions in your host round-trip with the ids the agent reads out of page bodies.

## Tool catalog

| Tool             | Description                                                                  | Required scope |
|------------------|------------------------------------------------------------------------------|----------------|
| `list_spaces`    | List every space the API key can access (id, name, slug).                    | read           |
| `list_pages`     | Flat page listing in a space. Optional `parent_id` for direct children.      | read           |
| `get_page`       | Full markdown body + metadata for a numeric page id.                         | read           |
| `search`         | FTS5 full-text search over title + body. Returns snippet-highlighted hits.   | read           |
| `search_bodies`  | Per-space fuzzy body search (no snippets). Re-fetch via `get_page`.          | read           |
| `list_backlinks` | Pages that link to a given page via `[[wikilink]]` / `tela://page/{id}`.     | read           |
| `create_page`    | Create a page. `{space_id, parent_id?, title, body}` → returns the new row.  | write          |
| `update_page`    | Patch `title` and/or `body`. Auto-snapshots a revision when body changes.    | write          |
| `delete_page`    | Delete a page. Backlinks from other pages are preserved with the last title. | write          |
| `add_comment`    | Attach a root comment, anchored by a `{prefix, exact, suffix}` text triplet. | write          |
| `import_markdown`| Walk a local directory, zip every `*.md` on the fly, bulk-import to a space. Pass `dry_run=true` to preview. 8 MiB total cap — split larger batches. | write          |
| `import_mira`    | Import a single [mira](https://mira.cagdas.io) page into a space via `source_url` (https-only host allowlist, fetched server-side) OR inline `payload` (raw block JSON). 1 MiB cap on both. | write          |
| `create_space`   | Create a Tela space. `{name, slug?}` → returns the new row. Creator auto-becomes owner. | write          |
| `update_space`   | Patch a space's `name` and/or `slug`. Owner role required within the space. | write          |
| `delete_space`   | Delete a space AND all its pages, comments, revisions, share links. Irreversible cascade. Owner role required. | **admin**      |
| `submit_feedback`| Submit free-text feedback about Tela / `tela-mcp` itself (friction, bugs, missing capabilities). NOT for page content notes — use `add_comment` for those. | read           |

### Scopes and space restriction

API keys are issued with one of three scopes:

- **read** — GET endpoints only. All read tools work; write tools 403 with `code=api_key_scope`.
- **write** — read + writes to pages / comments / imports.
- **admin** — write + user / space / API-key management. Required for the Settings → API Keys management UI itself.

A key may additionally be pinned to a single `space_id`. Cross-space requests return 403 `code=api_key_space_scope`. The MCP tool surface mirrors this verbatim — failures arrive as `{error, code, status}` text in the tool's `isError` content so the agent can react to specific codes.

## Compatibility

| `tela-mcp` version | Tela backend                       | Notes                                                |
|--------------------|------------------------------------|------------------------------------------------------|
| 0.1.x              | M16-AgentAPI and later             | Requires `/api/version`, `/api/api_keys`, bearer middleware, and `/api/search/bodies` (all shipped together as M16). |
| 0.2.x              | M16-AgentAPI + M17-Feedback and later | Adds `submit_feedback`, which requires `POST /api/feedback` (M17.A.1). |
| 0.3.x              | M16-AgentAPI + M17-Feedback + M18-MiraImport and later | Adds `import_mira`, which requires `POST /api/spaces/{id}/import-mira` (M18.A.3). |

On startup the server fires a one-shot `GET /api/version` probe (advisory — never blocks tool calls) and prints a stderr warning when the backend reports a strictly-greater semver than the version this `tela-mcp` was built against. Non-semver values like `dev` or short git SHAs short-circuit with a "skipping compat check" line instead of erroring.

## Troubleshooting

- **`missing required env: TELA_BASE_URL, TELA_API_KEY` and the process exits.** Set both env vars in your `.mcp.json` `env` block. Restart the MCP host so it re-spawns the child process.
- **`api_key_scope` on a write tool.** Your key is `read`-only. Issue a `write` (or `admin`) key from Settings → API Keys and update `TELA_API_KEY`.
- **`api_key_space_scope`.** The key is pinned to a single space; the tool was called against a different `space_id`. Either issue an unrestricted key or use the matching space.
- **Version mismatch advisory on stderr.** The backend is newer than this MCP build. Run `npm install -g tela-mcp@latest`, or pin a newer version in `.mcp.json`. Tool calls still proceed.
- **`local_path does not exist` from `import_markdown`.** The path is resolved on the *MCP server* host (i.e. wherever `npx tela-mcp` runs). For Claude Code on your laptop, that is your laptop. Use an absolute path if you are unsure.
- **`import bundle is X MiB which exceeds the 8 MiB cap`.** Split the directory and call `import_markdown` multiple times. Each call appends underneath the same `parent_id`.
- **5xx from the backend.** The client retries 5xx once with a short backoff. Persistent 5xx surfaces as `{code: 'http_error', status: 5xx}` in the tool result.

## See also

- Tela project Showcase, including the MCP section: <https://tela.cagdas.io/spaces/1/pages/19>

## Develop

```sh
cd mcp
npm install
npm run build
npm test              # unit tests with mocked fetch
npm run test:smoke    # spawns the stdio binary, exchanges initialize + tools/list
```

Smoke-boot the stdio server locally (needs a running Tela backend on `TELA_BASE_URL`):

```sh
TELA_BASE_URL=http://localhost:8780 TELA_API_KEY=tela_pat_xxx node dist/server.js
```

## License

MIT.
