// Live proxy ↔ backend integration test. The Makefile target
// `make test-mcp-integration` boots the stack and runs this file, which:
//
//   1. waits for GET /api/health
//   2. logs in as the bootstrap admin, mints an admin PAT, creates a space + page
//   3. spawns the built stdio proxy (dist/server.js) via the SDK client
//   4. asserts initialize + tools/list + tool calls + a resource read all
//      round-trip THROUGH the proxy to the backend's /api/mcp
//
// The proxy holds no tool knowledge — it's a dumb pipe — so a representative
// handful of calls proves the whole protocol forwards (with the static bearer
// and the Mcp-Session-Id handled inside the transport). Exhaustive per-tool
// shape coverage lives in the Go backend's e2e MCP tests
// (backend/internal/api/mcp_test.go).

import { describe, expect, it, beforeAll, afterAll } from "vitest";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { existsSync } from "node:fs";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";

const here = dirname(fileURLToPath(import.meta.url));
const binPath = resolve(here, "..", "dist", "server.js");
const BASE_URL = (process.env.TELA_BASE_URL ?? "http://localhost:8780").replace(/\/+$/, "");
const ADMIN_USERNAME = process.env.TELA_ADMIN_USERNAME ?? "testadmin";
const ADMIN_PASSWORD = process.env.TELA_ADMIN_PASSWORD ?? "testpassword123";

let sessionCookie = "";
let spaceId = 0;
let pageId = 0;
let client: Client;

async function waitForHealth(timeoutMs = 120_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const r = await fetch(`${BASE_URL}/api/health`);
      if (r.ok) return;
    } catch {
      /* not up yet */
    }
    await new Promise((r) => setTimeout(r, 1000));
  }
  throw new Error(`backend never became healthy at ${BASE_URL}`);
}

async function login(): Promise<void> {
  const res = await fetch(`${BASE_URL}/api/auth/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username: ADMIN_USERNAME, password: ADMIN_PASSWORD }),
  });
  if (!res.ok) throw new Error(`login failed: ${res.status} ${await res.text()}`);
  const m = (res.headers.get("set-cookie") ?? "").match(/tela_session=([^;]+)/);
  if (!m) throw new Error("login returned no session cookie");
  sessionCookie = `tela_session=${m[1]}`;
}

async function sessionJSON<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`${BASE_URL}${path}`, {
    method,
    headers: { "Content-Type": "application/json", Cookie: sessionCookie },
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) throw new Error(`${method} ${path} failed: ${res.status} ${await res.text()}`);
  return (await res.json()) as T;
}

beforeAll(async () => {
  if (!existsSync(binPath)) throw new Error(`build first — ${binPath} missing`);
  await waitForHealth();
  await login();
  const ts = Date.now().toString(36);
  const apiKey = (
    await sessionJSON<{ api_key: { key: string } }>("POST", "/api/api_keys", {
      name: "proxy-integration-test",
      scope: "admin",
    })
  ).api_key.key;
  spaceId = (
    await sessionJSON<{ space: { id: number } }>("POST", "/api/spaces", {
      name: `Proxy ${ts}`,
      slug: `proxy-${ts}`,
    })
  ).space.id;
  pageId = (
    await sessionJSON<{ page: { id: number } }>("POST", "/api/pages", {
      space_id: spaceId,
      title: "Proxy Page",
      body: "hello through the proxy",
    })
  ).page.id;

  const transport = new StdioClientTransport({
    command: process.execPath,
    args: [binPath],
    env: { ...process.env, TELA_BASE_URL: BASE_URL, TELA_API_KEY: apiKey },
  });
  client = new Client({ name: "proxy-test", version: "0" });
  await client.connect(transport);
}, 180_000);

afterAll(async () => {
  await client?.close();
});

describe("tela-mcp stdio↔HTTP proxy", () => {
  it("forwards tools/list", async () => {
    const { tools } = await client.listTools();
    const names = tools.map((t) => t.name);
    expect(names).toContain("list_spaces");
    expect(names).toContain("get_page");
  });

  it("forwards a tool call with auth + session (list_spaces)", async () => {
    const res = await client.callTool({ name: "list_spaces", arguments: {} });
    expect(res.isError).not.toBe(true);
    const out = res.structuredContent as { spaces: Array<{ id: number }> };
    expect(out.spaces.some((s) => s.id === spaceId)).toBe(true);
  });

  it("forwards get_page (structured content round-trips)", async () => {
    const res = await client.callTool({ name: "get_page", arguments: { id: pageId } });
    const out = res.structuredContent as { page: { body: string } };
    expect(out.page.body).toBe("hello through the proxy");
  });

  it("forwards a resource read (tela://page/{id})", async () => {
    const res = await client.readResource({ uri: `tela://page/${pageId}` });
    const text = (res.contents[0] as { text?: string }).text ?? "";
    expect(text).toContain("hello through the proxy");
  });
});
