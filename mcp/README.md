# tela-mcp

A **thin stdio↔HTTP proxy** to a [Tela](https://tela.cagdas.io) instance's
built-in MCP server.

As of v0.7, the MCP server lives **inside the Tela backend** at
`{TELA_BASE_URL}/api/mcp` (Streamable HTTP, spec-compliant). This npm package no
longer implements any tools itself — it's a dumb pipe that forwards the entire
MCP protocol (tools, resources, prompts, notifications) over stdio to that
endpoint, injecting your personal access token as a bearer header. Because it
holds no tool knowledge, the backend's MCP surface can grow without this package
ever changing — there is no second implementation to drift.

## Most hosts don't need this package

Modern MCP hosts speak **Streamable HTTP transport** directly — point them at the
endpoint and skip the proxy entirely:

```sh
# Claude Code
claude mcp add --transport http tela https://tela.cagdas.io/api/mcp \
  --header "Authorization: Bearer tela_pat_..."
```

Cursor, VS Code, Zed, and the Anthropic Messages API `mcp_servers` connector
likewise accept a URL + bearer header. Use this npm package **only for stdio-only
hosts** that can't speak HTTP transport.

## Using the proxy (stdio-only hosts)

Add to your `.mcp.json`:

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

| Var | Purpose |
|-----|---------|
| `TELA_BASE_URL` | Origin of the Tela instance (e.g. `https://tela.cagdas.io` or `http://localhost:8780`). The proxy connects to `{TELA_BASE_URL}/api/mcp`. |
| `TELA_API_KEY` | Personal access token (`tela_pat_…`), forwarded as `Authorization: Bearer`. Create one in **Settings → API Keys**. |

Both must be set at spawn time, or the process exits non-zero before the MCP
handshake.

## Tool & resource catalog

The tools (`list_spaces`, `get_page`, `search`, `semantic_search`,
`create_page`, `move_page`, `add_comment`, …), the `tela://page/{id}` /
`tela://space/{id}` resources, and the read/write/admin scope model are all
defined and documented **in the backend**, not here. Scope and per-space
restrictions are enforced server-side; failures arrive as the usual
`{error, code, status}` envelope in the tool result. See the project Showcase:
<https://tela.cagdas.io/spaces/1/pages/19>.

## Auth

This package authenticates with a **static personal access token** (PAT), which
works with the Messages API connector and any host that accepts a bearer header.
The OAuth "Connect" flow used by the Claude.ai / ChatGPT consumer apps is a
separate, server-side capability (it does not involve this package).

## Develop

```sh
cd mcp
npm install
npm run build            # tsc → dist/server.js
npm run test:integration # live proxy↔backend E2E (needs a running backend; use `make test-mcp-integration` from the repo root)
```

The proxy is ~40 lines over the official `@modelcontextprotocol/sdk` transports.
Exhaustive per-tool coverage lives in the Go backend's e2e MCP tests
(`backend/internal/api/mcp_test.go`).

## License

MIT.
