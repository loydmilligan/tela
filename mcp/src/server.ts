#!/usr/bin/env node
/**
 * tela-mcp — a thin stdio ↔ Streamable-HTTP proxy.
 *
 * The real MCP server now lives in the tela backend at {TELA_BASE_URL}/api/mcp
 * (Streamable HTTP, authenticated by a tela personal access token). This process
 * is a dumb pipe: it speaks stdio to the host and forwards every JSON-RPC
 * message, untouched, to the remote HTTP server with
 * `Authorization: Bearer {TELA_API_KEY}`. It holds NO tool / resource / prompt
 * knowledge, so the backend's MCP surface can grow without ever touching this
 * package — there is no second implementation to drift.
 *
 * Modern MCP hosts (Claude Code, Cursor, VS Code, Zed, the Anthropic Messages
 * API connector) can skip this package entirely and point their HTTP transport
 * straight at {TELA_BASE_URL}/api/mcp. This proxy exists only for stdio-only
 * hosts that can't speak HTTP transport.
 */
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import type { JSONRPCMessage } from "@modelcontextprotocol/sdk/types.js";

const baseUrl = process.env.TELA_BASE_URL?.replace(/\/+$/, "");
const apiKey = process.env.TELA_API_KEY;
if (!baseUrl || !apiKey) {
  console.error("tela-mcp: TELA_BASE_URL and TELA_API_KEY must both be set");
  process.exit(1);
}

const endpoint = new URL(`${baseUrl}/api/mcp`);

// One Authorization header is merged into the POST, the standalone GET SSE
// stream, AND the DELETE teardown by the transport's _commonHeaders(), so a
// static bearer here covers the whole session.
const remote = new StreamableHTTPClientTransport(endpoint, {
  requestInit: { headers: { Authorization: `Bearer ${apiKey}` } },
});
const local = new StdioServerTransport();

let closing = false;
function shutdown(): void {
  if (closing) return;
  closing = true;
  void Promise.allSettled([local.close(), remote.close()]).then(() => process.exit(0));
}

// host (stdin) → backend
local.onmessage = (msg: JSONRPCMessage) => {
  remote.send(msg).catch((err) => {
    console.error("tela-mcp: forward to backend failed:", err);
    shutdown();
  });
};

// backend → host (stdout)
remote.onmessage = (msg: JSONRPCMessage) => {
  // No Protocol layer here to call setProtocolVersion for us, so sniff the
  // initialize result and keep the MCP-Protocol-Version header correct on
  // subsequent POSTs. (Harmless against the tela backend regardless.)
  const result = (msg as { result?: { protocolVersion?: string } }).result;
  if (result?.protocolVersion) remote.setProtocolVersion(result.protocolVersion);
  local.send(msg).catch((err) => {
    console.error("tela-mcp: forward to host failed:", err);
    shutdown();
  });
};

local.onclose = shutdown;
remote.onclose = shutdown;
local.onerror = (err) => console.error("tela-mcp (stdio):", err);
remote.onerror = (err) => console.error("tela-mcp (http):", err);

await remote.start(); // arms the HTTP transport (no handshake of its own)
await local.start(); // begins reading stdin
console.error(`tela-mcp: proxying stdio ↔ ${endpoint.href}`);
