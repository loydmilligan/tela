// Live MCP <-> backend integration test (M16.C.2). Boots the full stack via
// docker compose externally; this file:
//
//   1. waits for the backend to answer GET /api/health
//   2. logs in as the bootstrap admin (TELA_ADMIN_USERNAME / TELA_ADMIN_PASSWORD)
//   3. creates an admin-scope API key, a fresh space, and five sample pages
//   4. spawns the built MCP stdio binary (dist/server.js)
//   5. JSON-RPC handshake → tools/list → calls every tool with realistic args
//   6. asserts the response shape returned by each tool matches the typed
//      contract declared in mcp/src/tools/*.ts.
//
// Why this test exists: when the REST API changes shape (e.g., a renamed
// field), each tool in mcp/src/tools/* still compiles — TypeScript only sees
// the local interface, never the live JSON. This test is the canonical guard
// against silent drift; failures show up at PR time, not in production.

import { describe, expect, it, beforeAll, afterAll } from "vitest";
import { spawn, type ChildProcess } from "node:child_process";
import { existsSync, mkdtempSync, rmSync, writeFileSync, mkdirSync } from "node:fs";
import { resolve, dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { tmpdir } from "node:os";

const here = dirname(fileURLToPath(import.meta.url));
const binPath = resolve(here, "..", "dist", "server.js");

const BASE_URL = (process.env.TELA_BASE_URL ?? "http://localhost:8780").replace(/\/+$/, "");
const ADMIN_USERNAME = process.env.TELA_ADMIN_USERNAME ?? "testadmin";
const ADMIN_PASSWORD = process.env.TELA_ADMIN_PASSWORD ?? "testpassword123";

interface SeedState {
  apiKey: string;
  spaceId: number;
  pageIds: number[];
  /** Page whose body contains a wikilink to pageIds[0] — exercise list_backlinks. */
  pageWithBacklinkSource: number;
  /** Substring guaranteed unique to one page, used to drive search + search_bodies. */
  uniqueToken: string;
  /** Body of pageIds[0], known shape so add_comment anchor matches. */
  pageZeroBody: string;
  importTmpDir: string;
}

const seed: SeedState = {
  apiKey: "",
  spaceId: 0,
  pageIds: [],
  pageWithBacklinkSource: 0,
  uniqueToken: `telax${Date.now().toString(36)}q`,
  pageZeroBody: "",
  importTmpDir: "",
};

interface JsonRpcResponse {
  jsonrpc: "2.0";
  id: number | string | null;
  result?: unknown;
  error?: { code: number; message: string };
}

let child: ChildProcess | null = null;
let nextId = 1;
let sessionCookie = "";
const responses = new Map<number, JsonRpcResponse>();

async function waitForBackend(timeoutMs = 120_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  let lastErr: string = "";
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`${BASE_URL}/api/health`);
      if (res.ok) return;
      lastErr = `HTTP ${res.status}`;
    } catch (err) {
      lastErr = (err as Error).message;
    }
    await new Promise((r) => setTimeout(r, 1000));
  }
  throw new Error(`backend never became healthy at ${BASE_URL}/api/health: ${lastErr}`);
}

async function login(): Promise<void> {
  const res = await fetch(`${BASE_URL}/api/auth/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username: ADMIN_USERNAME, password: ADMIN_PASSWORD }),
  });
  if (!res.ok) {
    throw new Error(`login failed: HTTP ${res.status} ${await res.text()}`);
  }
  const setCookie = res.headers.get("set-cookie");
  if (!setCookie) throw new Error("login returned no Set-Cookie");
  const m = setCookie.match(/tela_session=([^;]+)/);
  if (!m) throw new Error(`login returned unexpected Set-Cookie: ${setCookie}`);
  sessionCookie = `tela_session=${m[1]}`;
}

async function sessionJSON<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    Cookie: sessionCookie,
  };
  const res = await fetch(`${BASE_URL}${path}`, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) {
    throw new Error(`${method} ${path} failed: HTTP ${res.status} ${await res.text()}`);
  }
  return (await res.json()) as T;
}

async function createApiKey(): Promise<string> {
  interface Resp {
    api_key: { id: number; key: string; scope: string };
  }
  const r = await sessionJSON<Resp>("POST", "/api/api_keys", {
    name: "mcp-integration-test",
    scope: "admin",
  });
  if (!r.api_key?.key) throw new Error("create_api_key did not return the raw key");
  return r.api_key.key;
}

async function createSpace(): Promise<number> {
  interface Resp {
    space: { id: number; name: string; slug: string };
  }
  const slug = `mcp-itest-${Date.now().toString(36)}`;
  const r = await sessionJSON<Resp>("POST", "/api/spaces", {
    name: `MCP Integration ${slug}`,
    slug,
  });
  if (!r.space?.id) throw new Error("create_space did not return a space row");
  return r.space.id;
}

async function createPage(opts: {
  space_id: number;
  title: string;
  body: string;
  parent_id?: number;
}): Promise<{ id: number; title: string; body: string }> {
  interface Resp {
    page: { id: number; title: string; body: string };
  }
  const r = await sessionJSON<Resp>("POST", "/api/pages", opts);
  if (!r.page?.id) throw new Error("create_page did not return a page row");
  return r.page;
}

async function seedStack(): Promise<void> {
  await waitForBackend();
  await login();
  seed.apiKey = await createApiKey();
  seed.spaceId = await createSpace();

  // Page 0: target of backlinks. Body shape is known so add_comment can
  // anchor exactly inside it.
  seed.pageZeroBody = [
    `# Welcome to the test space`,
    ``,
    `This is the canonical landing page for the MCP integration suite.`,
    `It contains the ${seed.uniqueToken} token to make search trivially deterministic.`,
    ``,
    `End of intro.`,
  ].join("\n");
  const p0 = await createPage({
    space_id: seed.spaceId,
    title: "Welcome",
    body: seed.pageZeroBody,
  });
  seed.pageIds.push(p0.id);

  // Page 1: nested child + wikilink back to page 0 — drives list_backlinks.
  const p1 = await createPage({
    space_id: seed.spaceId,
    title: "Architecture",
    parent_id: p0.id,
    body: [
      `Refer to [Welcome](tela://page/${p0.id}) for an overview.`,
      ``,
      `This page covers architecture topics.`,
    ].join("\n"),
  });
  seed.pageIds.push(p1.id);
  seed.pageWithBacklinkSource = p1.id;

  // Page 2-4: pad the corpus so list_pages has >1 row.
  for (const t of ["Runbook", "Glossary", "FAQ"]) {
    const p = await createPage({
      space_id: seed.spaceId,
      title: t,
      body: `# ${t}\n\nLorem ipsum dolor sit amet. Placeholder body for ${t}.`,
    });
    seed.pageIds.push(p.id);
  }

  // Stage a tiny markdown bundle on disk so import_markdown has something
  // real to upload. Use OS temp dir; cleaned up in afterAll.
  seed.importTmpDir = mkdtempSync(join(tmpdir(), "tela-mcp-itest-"));
  mkdirSync(join(seed.importTmpDir, "sub"), { recursive: true });
  writeFileSync(
    join(seed.importTmpDir, "alpha.md"),
    "# Alpha\n\nFirst imported page.\n",
  );
  writeFileSync(
    join(seed.importTmpDir, "sub", "beta.md"),
    "# Beta\n\nNested imported page.\n",
  );
}

function spawnMCP(): void {
  child = spawn(process.execPath, [binPath], {
    env: {
      ...process.env,
      TELA_BASE_URL: BASE_URL,
      TELA_API_KEY: seed.apiKey,
    },
    stdio: ["pipe", "pipe", "pipe"],
  });

  let buf = "";
  child.stdout!.on("data", (chunk: Buffer) => {
    buf += chunk.toString("utf8");
    let nl: number;
    while ((nl = buf.indexOf("\n")) >= 0) {
      const line = buf.slice(0, nl).trim();
      buf = buf.slice(nl + 1);
      if (!line) continue;
      try {
        const msg = JSON.parse(line) as JsonRpcResponse;
        if (typeof msg.id === "number") responses.set(msg.id, msg);
      } catch {
        // stdout should be JSON-RPC clean; ignore stray lines defensively.
      }
    }
  });
  child.stderr!.on("data", (chunk: Buffer) => {
    // Mirror server stderr so CI logs surface any startup issue.
    process.stderr.write(`[mcp] ${chunk.toString("utf8")}`);
  });
}

function send(msg: object): void {
  if (!child) throw new Error("MCP child not spawned");
  child.stdin!.write(JSON.stringify(msg) + "\n");
}

async function call(method: string, params: object): Promise<JsonRpcResponse> {
  const id = nextId++;
  send({ jsonrpc: "2.0", id, method, params });
  const deadline = Date.now() + 20_000;
  while (Date.now() < deadline) {
    const r = responses.get(id);
    if (r) return r;
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
  throw new Error(`timeout waiting for response id=${id} method=${method}`);
}

/**
 * Calls a tool via tools/call and parses the JSON payload the server wraps
 * inside content[0].text. Throws if the tool returned isError or if the
 * payload is not JSON.
 */
async function callTool<T = unknown>(name: string, args: object): Promise<T> {
  const res = await call("tools/call", { name, arguments: args });
  expect(res.error, `transport error for ${name}: ${JSON.stringify(res.error)}`).toBeUndefined();
  const result = res.result as {
    content?: Array<{ type: string; text: string }>;
    isError?: boolean;
  };
  expect(result, `no result envelope for ${name}`).toBeDefined();
  expect(Array.isArray(result.content), `content not array for ${name}`).toBe(true);
  expect(result.content!.length, `empty content for ${name}`).toBeGreaterThan(0);
  expect(result.content![0].type, `content[0].type for ${name}`).toBe("text");
  const text = result.content![0].text;
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch (err) {
    throw new Error(`tool ${name} returned non-JSON text: ${text} (${(err as Error).message})`);
  }
  if (result.isError) {
    throw new Error(`tool ${name} reported isError: ${JSON.stringify(parsed)}`);
  }
  return parsed as T;
}

// NOTE: this test does NOT skip when the build artifact is missing. A silent
// skip would let CI go green without actually running the suite — exactly the
// failure mode this test exists to catch. If dist/server.js is absent we
// fail hard so the operator notices.
describe("MCP <-> backend live integration", () => {
  beforeAll(async () => {
    if (!existsSync(binPath)) {
      throw new Error(`MCP build artifact missing: ${binPath}. Run \`npm --prefix mcp run build\` first.`);
    }
    await seedStack();
    spawnMCP();

    const initRes = await call("initialize", {
      protocolVersion: "2025-06-18",
      capabilities: {},
      clientInfo: { name: "live-integration", version: "0.0.0" },
    });
    expect(initRes.error).toBeUndefined();
    expect(initRes.result).toMatchObject({ serverInfo: { name: "tela" } });
    send({ jsonrpc: "2.0", method: "notifications/initialized", params: {} });
  }, 180_000);

  afterAll(() => {
    if (child) child.kill("SIGTERM");
    if (seed.importTmpDir) {
      try {
        rmSync(seed.importTmpDir, { recursive: true, force: true });
      } catch {
        // best-effort cleanup
      }
    }
  });

  it("tools/list exposes all 11 tools", async () => {
    const res = await call("tools/list", {});
    expect(res.error).toBeUndefined();
    const tools = (res.result as { tools: Array<{ name: string }> }).tools;
    const names = tools.map((t) => t.name).sort();
    expect(names).toEqual(
      [
        "add_comment",
        "create_page",
        "delete_page",
        "get_page",
        "import_markdown",
        "list_backlinks",
        "list_pages",
        "list_spaces",
        "search",
        "search_bodies",
        "update_page",
      ].sort(),
    );
  });

  it("list_spaces returns our seeded space with the expected shape", async () => {
    const r = await callTool<{ spaces: Array<{ id: number; name: string; slug: string }> }>(
      "list_spaces",
      {},
    );
    expect(Array.isArray(r.spaces)).toBe(true);
    expect(r.spaces.length).toBeGreaterThan(0);
    for (const s of r.spaces) {
      expect(typeof s.id).toBe("number");
      expect(typeof s.name).toBe("string");
      expect(typeof s.slug).toBe("string");
      // The DTO is intentionally narrow — fields outside our typed contract
      // would be a sign of accidental over-projection. Spot-check the three
      // we expect are the only keys present.
      expect(new Set(Object.keys(s))).toEqual(new Set(["id", "name", "slug"]));
    }
    expect(r.spaces.some((s) => s.id === seed.spaceId)).toBe(true);
  });

  it("list_pages (no parent_id) returns the 4 root pages with the expected shape", async () => {
    // Backend's GET /api/pages?space_id=X (no parent_id param) returns rows
    // where parent_id IS NULL — i.e., root pages only. The seed has 4 such
    // pages: Welcome, Runbook, Glossary, FAQ. Architecture is nested under
    // Welcome and surfaces only via the {parent_id} call below.
    const r = await callTool<{
      pages: Array<{
        id: number;
        title: string;
        parent_id: number | null;
        position: number;
        space_id: number;
      }>;
    }>("list_pages", { space_id: seed.spaceId });
    expect(Array.isArray(r.pages)).toBe(true);
    expect(r.pages.length).toBe(4);
    for (const p of r.pages) {
      expect(typeof p.id).toBe("number");
      expect(typeof p.title).toBe("string");
      expect(typeof p.position).toBe("number");
      expect(p.space_id).toBe(seed.spaceId);
      expect(p.parent_id).toBeNull();
      expect(new Set(Object.keys(p))).toEqual(
        new Set(["id", "title", "parent_id", "position", "space_id"]),
      );
    }
  });

  it("list_pages (with parent_id) returns direct children only", async () => {
    const r = await callTool<{
      pages: Array<{ id: number; parent_id: number | null }>;
    }>("list_pages", { space_id: seed.spaceId, parent_id: seed.pageIds[0] });
    expect(Array.isArray(r.pages)).toBe(true);
    expect(r.pages.length).toBe(1);
    expect(r.pages[0].id).toBe(seed.pageWithBacklinkSource);
    expect(r.pages[0].parent_id).toBe(seed.pageIds[0]);
  });

  it("get_page returns the full body unwrapped (NOT wrapped in {page})", async () => {
    const r = await callTool<{
      id: number;
      title: string;
      body: string;
      space_id: number;
      parent_id: number | null;
      created_at: string;
      updated_at: string;
    }>("get_page", { id: seed.pageIds[0] });
    expect(r.id).toBe(seed.pageIds[0]);
    expect(typeof r.body).toBe("string");
    expect(r.body).toContain(seed.uniqueToken);
    expect(r.space_id).toBe(seed.spaceId);
    expect(typeof r.created_at).toBe("string");
    expect(typeof r.updated_at).toBe("string");
    expect(new Set(Object.keys(r))).toEqual(
      new Set(["id", "title", "body", "space_id", "parent_id", "created_at", "updated_at"]),
    );
  });

  it("search returns hits projected to {id, title, snippet}", async () => {
    const r = await callTool<{
      results: Array<{ id: number; title: string; snippet: string }>;
    }>("search", { query: seed.uniqueToken, space_id: seed.spaceId });
    expect(Array.isArray(r.results)).toBe(true);
    expect(r.results.length).toBeGreaterThan(0);
    for (const h of r.results) {
      expect(typeof h.id).toBe("number");
      expect(typeof h.title).toBe("string");
      expect(typeof h.snippet).toBe("string");
      expect(new Set(Object.keys(h))).toEqual(new Set(["id", "title", "snippet"]));
    }
    expect(r.results.some((h) => h.id === seed.pageIds[0])).toBe(true);
  });

  it("search_bodies returns hits with {id, title, score}", async () => {
    const r = await callTool<{
      results: Array<{ id: number; title: string; score: number }>;
    }>("search_bodies", { query: seed.uniqueToken, space_id: seed.spaceId });
    expect(Array.isArray(r.results)).toBe(true);
    expect(r.results.length).toBeGreaterThan(0);
    for (const h of r.results) {
      expect(typeof h.id).toBe("number");
      expect(typeof h.title).toBe("string");
      expect(typeof h.score).toBe("number");
      // M16.A.5 score is in (0, 1) — sigmoid of -bm25. Anything outside this
      // range means the backend formula drifted from the documented contract.
      expect(h.score).toBeGreaterThan(0);
      expect(h.score).toBeLessThan(1);
      expect(new Set(Object.keys(h))).toEqual(new Set(["id", "title", "score"]));
    }
  });

  it("list_backlinks returns pages that wikilink to the target", async () => {
    const r = await callTool<{
      backlinks: Array<{ id: number; title: string; space_id: number }>;
    }>("list_backlinks", { page_id: seed.pageIds[0] });
    expect(Array.isArray(r.backlinks)).toBe(true);
    // Page 1's body wikilinks to page 0 — must appear here.
    expect(r.backlinks.some((b) => b.id === seed.pageWithBacklinkSource)).toBe(true);
    for (const b of r.backlinks) {
      expect(typeof b.id).toBe("number");
      expect(typeof b.title).toBe("string");
      expect(typeof b.space_id).toBe("number");
      expect(new Set(Object.keys(b))).toEqual(new Set(["id", "title", "space_id"]));
    }
  });

  // Write tools below mutate state but the volume is torn down on cleanup, so
  // there is no need to be conservative.
  let createdPageId = 0;

  it("create_page returns the new page wrapped in {page}", async () => {
    const r = await callTool<{
      page: { id: number; title: string; body: string; space_id: number; position: number };
    }>("create_page", {
      space_id: seed.spaceId,
      title: "Created via MCP",
      body: "Body written by integration test.",
    });
    expect(r.page).toBeDefined();
    expect(typeof r.page.id).toBe("number");
    expect(r.page.title).toBe("Created via MCP");
    expect(r.page.space_id).toBe(seed.spaceId);
    createdPageId = r.page.id;
  });

  it("update_page returns the updated page wrapped in {page}", async () => {
    const r = await callTool<{ page: { id: number; title: string; body: string } }>(
      "update_page",
      { id: createdPageId, body: "Updated body." },
    );
    expect(r.page.id).toBe(createdPageId);
    expect(r.page.body).toBe("Updated body.");
  });

  it("add_comment returns {comment} anchored to the given text", async () => {
    // Anchor exactly on a substring of pageZeroBody. The backend stores the
    // three columns verbatim; the tool flattens the nested anchor object.
    const exact = seed.uniqueToken;
    const idx = seed.pageZeroBody.indexOf(exact);
    expect(idx).toBeGreaterThan(-1);
    const prefix = seed.pageZeroBody.slice(Math.max(0, idx - 32), idx);
    const suffix = seed.pageZeroBody.slice(idx + exact.length, idx + exact.length + 32);

    const r = await callTool<{
      comment: {
        id: number;
        page_id: number;
        anchor_prefix: string | null;
        anchor_exact: string | null;
        anchor_suffix: string | null;
        body: string;
      };
    }>("add_comment", {
      page_id: seed.pageIds[0],
      anchor: { prefix, exact, suffix },
      body: "Anchored by integration test.",
    });
    expect(typeof r.comment.id).toBe("number");
    expect(r.comment.page_id).toBe(seed.pageIds[0]);
    expect(r.comment.anchor_exact).toBe(exact);
    expect(r.comment.anchor_prefix).toBe(prefix);
    expect(r.comment.anchor_suffix).toBe(suffix);
    expect(r.comment.body).toBe("Anchored by integration test.");
  });

  it("import_markdown returns the {summary, pages, skipped, errors} envelope", async () => {
    const r = await callTool<{
      summary: { created: number; skipped: number; conflicts_renamed: number };
      pages: unknown[];
      skipped: unknown[];
      errors: unknown[];
    }>("import_markdown", {
      space_id: seed.spaceId,
      local_path: seed.importTmpDir,
    });
    expect(r.summary).toBeDefined();
    expect(typeof r.summary.created).toBe("number");
    expect(typeof r.summary.skipped).toBe("number");
    expect(typeof r.summary.conflicts_renamed).toBe("number");
    expect(Array.isArray(r.pages)).toBe(true);
    expect(Array.isArray(r.skipped)).toBe(true);
    expect(Array.isArray(r.errors)).toBe(true);
    // The fixture has alpha.md + sub/beta.md → backend creates 2 pages plus
    // the directory wrapper "sub" → 3 created rows. Assert the lower bound so
    // a future README-as-index tweak does not break the test.
    expect(r.summary.created).toBeGreaterThanOrEqual(2);
  });

  it("delete_page returns {ok: true} and the page becomes inaccessible", async () => {
    const r = await callTool<{ ok: boolean }>("delete_page", { id: createdPageId });
    expect(r).toEqual({ ok: true });

    // Follow-up get_page must come back as a tool error envelope. The
    // backend deliberately collapses missing-page to 403 forbidden (see
    // GetPage handler) so callers cannot enumerate page ids across spaces
    // they're not in. The contract test we want here is "tool surfaces the
    // error envelope verbatim", regardless of which envelope it is.
    const followUp = await call("tools/call", {
      name: "get_page",
      arguments: { id: createdPageId },
    });
    const env = followUp.result as {
      content: Array<{ text: string }>;
      isError?: boolean;
    };
    expect(env.isError).toBe(true);
    const parsed = JSON.parse(env.content[0].text) as { code: string; status: number };
    expect(parsed.code).toBe("forbidden");
    expect(parsed.status).toBe(403);
  });
});
