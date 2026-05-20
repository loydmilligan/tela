// Smoke test: spawn the built stdio binary, send initialize + tools/list,
// assert tool shape. Skipped unless dist/server.js exists. The test only
// runs after `npm run build` (CI: `npm run build && npm run test:smoke`).

import { describe, expect, it } from "vitest";
import { spawn } from "node:child_process";
import { existsSync, mkdtempSync, rmSync, symlinkSync } from "node:fs";
import { tmpdir } from "node:os";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const binPath = resolve(here, "..", "dist", "server.js");
const hasBuild = existsSync(binPath);

// Regression for 0.1.0 symlink bug: npx spawns the binary via
// `node_modules/.bin/tela-mcp` which is a symlink to dist/server.js. The
// entrypoint guard used path equality, which doesn't follow symlinks, so
// main() never ran and the process exited 0 silently. We mirror that
// invocation here by creating a real symlink and invoking via it. Should be
// indistinguishable from a direct spawn from the test's perspective.
function setupSymlinkBin(): { path: string; cleanup: () => void } {
  const dir = mkdtempSync(join(tmpdir(), "tela-mcp-symlink-"));
  const linkPath = join(dir, "tela-mcp");
  symlinkSync(binPath, linkPath);
  return { path: linkPath, cleanup: () => rmSync(dir, { recursive: true, force: true }) };
}

interface JsonRpcResponse {
  jsonrpc: "2.0";
  id: number | string | null;
  result?: unknown;
  error?: { code: number; message: string };
}

describe.skipIf(!hasBuild)("stdio smoke", () => {
  // Both invocation paths must behave identically: a direct spawn of
  // dist/server.js, and a spawn via a symlink (npx's bin-dir layout).
  const invocations: Array<{ name: string; mkArgv: () => { argv0: string; cleanup: () => void } }> = [
    {
      name: "direct (node dist/server.js)",
      mkArgv: () => ({ argv0: binPath, cleanup: () => {} }),
    },
    {
      name: "via bin symlink (npx invocation shape)",
      mkArgv: () => {
        const { path, cleanup } = setupSymlinkBin();
        return { argv0: path, cleanup };
      },
    },
  ];

  it.each(invocations)("$name responds to initialize + tools/list with full surface", async ({ mkArgv }) => {
    const { argv0, cleanup } = mkArgv();
    const child = spawn(process.execPath, [argv0], {
      env: {
        ...process.env,
        TELA_BASE_URL: "http://127.0.0.1:1", // unreachable, irrelevant for handshake
        TELA_API_KEY: "tela_pat_smoke",
      },
      stdio: ["pipe", "pipe", "pipe"],
    });

    const responses = new Map<number, JsonRpcResponse>();
    let buf = "";
    child.stdout.on("data", (chunk: Buffer) => {
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
          // ignore — server may emit non-JSON to stderr only; stdout should be JSON-RPC
        }
      }
    });

    const send = (msg: object): void => {
      child.stdin.write(JSON.stringify(msg) + "\n");
    };

    const wait = async (id: number, timeoutMs = 5000): Promise<JsonRpcResponse> => {
      const deadline = Date.now() + timeoutMs;
      while (Date.now() < deadline) {
        const r = responses.get(id);
        if (r) return r;
        await new Promise((resolve) => setTimeout(resolve, 10));
      }
      throw new Error(`timeout waiting for response id=${id}`);
    };

    try {
      send({
        jsonrpc: "2.0",
        id: 1,
        method: "initialize",
        params: {
          protocolVersion: "2025-06-18",
          capabilities: {},
          clientInfo: { name: "smoke", version: "0.0.0" },
        },
      });
      const initRes = await wait(1);
      expect(initRes.error).toBeUndefined();
      expect(initRes.result).toMatchObject({ serverInfo: { name: "tela" } });

      send({ jsonrpc: "2.0", method: "notifications/initialized", params: {} });

      send({ jsonrpc: "2.0", id: 2, method: "tools/list", params: {} });
      const listRes = await wait(2);
      expect(listRes.error).toBeUndefined();
      const tools = (listRes.result as { tools: Array<{ name: string }> }).tools;
      const names = tools.map((t) => t.name).sort();
      expect(names).toEqual(
        [
          "add_comment",
          "create_page",
          "create_space",
          "delete_page",
          "delete_space",
          "get_page",
          "import_markdown",
          "list_backlinks",
          "list_pages",
          "list_spaces",
          "search",
          "search_bodies",
          "submit_feedback",
          "update_page",
          "update_space",
        ].sort(),
      );
    } finally {
      child.kill("SIGTERM");
      cleanup();
    }
  });
});
