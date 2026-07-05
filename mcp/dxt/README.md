# tela Claude Desktop extension (.dxt)

A one-click [Desktop Extension](https://github.com/anthropics/dxt) that wraps the
`tela-mcp` stdio proxy. Installing it in Claude Desktop prompts for your tela
base URL + API token — no hand-edited `claude_desktop_config.json`.

## Build & release

```bash
cd mcp/dxt
npx @anthropic-ai/mcpb pack .   # (formerly @anthropic-ai/dxt) → tela.dxt
```

Then:

1. Test locally: open `tela.dxt` in Claude Desktop, enter a base URL + PAT, and
   confirm the tela tools appear and a `search` call works.
2. Attach `tela.dxt` to the matching GitHub Release.
3. Add a one-click button to the root `README.md` "One-click install" block once
   the download URL exists (don't advertise it before it's uploaded).

Keep `version` in `manifest.json` in lockstep with `mcp/package.json` and
`mcp/server.json` on each release.

> `manifest.json` runs the published npm package via `npx -y tela-mcp`, so the
> `.dxt` itself carries no server code — it's just the config + user-prompt shell.
> `server.entry_point` is a schema formality here; Claude Desktop launches the
> `mcp_config.command` (`npx`).
