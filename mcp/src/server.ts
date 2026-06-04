#!/usr/bin/env node
// Tela MCP server. Read + write + import tool surface (M16.B.1 + M16.C.1).
//
// Transport: stdio. Process is spawned by the MCP host (Claude Code, etc.)
// per .mcp.json. All tool calls become bearer-authed HTTP requests against
// the Tela backend specified by TELA_BASE_URL.

import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { readFileSync, realpathSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";

import { TelaApiError, TelaClient } from "./client.js";
import { runVersionCheck } from "./version-check.js";

import { listSpaces, listSpacesInputSchema } from "./tools/list-spaces.js";
import { listPages, listPagesInputSchema } from "./tools/list-pages.js";
import { getPage, getPageInputSchema } from "./tools/get-page.js";
import { search, searchInputSchema } from "./tools/search.js";
import { searchBodies, searchBodiesInputSchema } from "./tools/search-bodies.js";
import { semanticSearch, semanticSearchInputSchema } from "./tools/semantic-search.js";
import { listBacklinks, listBacklinksInputSchema } from "./tools/list-backlinks.js";
import { createPage, createPageInputSchema } from "./tools/create-page.js";
import { updatePage, updatePageInputSchema } from "./tools/update-page.js";
import { deletePage, deletePageInputSchema } from "./tools/delete-page.js";
import { addComment, addCommentInputSchema } from "./tools/add-comment.js";
import { importMarkdown, importMarkdownInputSchema } from "./tools/import-markdown.js";
import { importMira, importMiraInputSchema } from "./tools/import-mira.js";
import { submitFeedback, submitFeedbackInputSchema } from "./tools/submit-feedback.js";
import { createSpace, createSpaceInputSchema } from "./tools/create-space.js";
import { updateSpace, updateSpaceInputSchema } from "./tools/update-space.js";
import { deleteSpace, deleteSpaceInputSchema } from "./tools/delete-space.js";
import { registerPageResource } from "./resources/page.js";

function readPackageVersion(): string {
  try {
    const here = dirname(fileURLToPath(import.meta.url));
    const pkgPath = resolve(here, "..", "package.json");
    const raw = readFileSync(pkgPath, "utf8");
    const pkg = JSON.parse(raw) as { version?: string };
    return typeof pkg.version === "string" ? pkg.version : "0.0.0";
  } catch {
    return "0.0.0";
  }
}

// SDK's CallToolResult requires an index signature alongside the typed
// fields; the index signature must permit `unknown` so the object literal
// stays portable across SDK 1.x patch releases. The cast confines that
// looseness to the two helpers below.
type ToolCallResult = {
  content: Array<{ type: "text"; text: string }>;
  isError?: boolean;
  [k: string]: unknown;
};

function ok(value: unknown): ToolCallResult {
  return { content: [{ type: "text", text: JSON.stringify(value) }] };
}

function fail(err: unknown): ToolCallResult {
  if (err instanceof TelaApiError) {
    return {
      content: [
        {
          type: "text",
          text: JSON.stringify({ error: err.message, code: err.code, status: err.status }),
        },
      ],
      isError: true,
    };
  }
  const msg = err instanceof Error ? err.message : String(err);
  return {
    content: [{ type: "text", text: JSON.stringify({ error: msg, code: "client_error" }) }],
    isError: true,
  };
}

export function buildServer(client: TelaClient, version: string): McpServer {
  const server = new McpServer(
    { name: "tela", version },
    { capabilities: { tools: {}, resources: {} } },
  );

  server.registerTool(
    "list_spaces",
    {
      description:
        "List all Tela spaces this API key can access. Returns id, name, slug only — timestamps (created_at, updated_at) are dropped; this is the navigation projection.",
      inputSchema: listSpacesInputSchema.shape,
    },
    async () => {
      try {
        return ok(await listSpaces(client));
      } catch (err) {
        return fail(err);
      }
    },
  );

  server.registerTool(
    "list_pages",
    {
      description:
        "List pages in a space. Flat list. Pass parent_id to scope to direct children; omit for root pages. Returns id, title, parent_id, position, space_id only — timestamps (created_at, updated_at) are dropped; re-fetch via get_page for the full row.",
      inputSchema: listPagesInputSchema,
    },
    async (args) => {
      try {
        return ok(await listPages(client, args));
      } catch (err) {
        return fail(err);
      }
    },
  );

  server.registerTool(
    "get_page",
    {
      description:
        "Fetch a page (full markdown body + metadata) by numeric id. Includes `url`, the human-shareable in-app link.",
      inputSchema: getPageInputSchema,
    },
    async (args) => {
      try {
        return ok(await getPage(client, args));
      } catch (err) {
        return fail(err);
      }
    },
  );

  server.registerTool(
    "search",
    {
      description:
        "Full-text search across page titles and bodies (FTS5 BM25). Returns snippet-highlighted hits.",
      inputSchema: searchInputSchema,
    },
    async (args) => {
      try {
        return ok(await search(client, args));
      } catch (err) {
        return fail(err);
      }
    },
  );

  server.registerTool(
    "search_bodies",
    {
      description:
        "Fuzzy body search within a single space. No snippets — re-fetch via get_page for context. Higher score = better match.",
      inputSchema: searchBodiesInputSchema,
    },
    async (args) => {
      try {
        return ok(await searchBodies(client, args));
      } catch (err) {
        return fail(err);
      }
    },
  );

  server.registerTool(
    "semantic_search",
    {
      description:
        "Meaning-aware chunk search (hybrid: embeddings + BM25, RRF-fused). Returns ranked page sections with heading_path for citation and page_id to re-fetch via get_page. Best for conceptual/natural-language questions where keywords won't match. Omit space_id to search all accessible spaces.",
      inputSchema: semanticSearchInputSchema,
    },
    async (args) => {
      try {
        return ok(await semanticSearch(client, args));
      } catch (err) {
        return fail(err);
      }
    },
  );

  server.registerTool(
    "list_backlinks",
    {
      description:
        "List pages that link to the given page via [[wikilink]] or tela://page/{id} reference.",
      inputSchema: listBacklinksInputSchema,
    },
    async (args) => {
      try {
        return ok(await listBacklinks(client, args));
      } catch (err) {
        return fail(err);
      }
    },
  );

  server.registerTool(
    "create_page",
    {
      description:
        "Create a new page. Requires write scope on the API key (or admin); editor/owner on the target space. Returns the created page row, including `url` (the human-shareable in-app link).",
      inputSchema: createPageInputSchema,
    },
    async (args) => {
      try {
        return ok(await createPage(client, args));
      } catch (err) {
        return fail(err);
      }
    },
  );

  server.registerTool(
    "update_page",
    {
      description:
        "Update title and/or body of an existing page. At least one of title, body must be provided. Snapshots a revision when content changes. Returns the updated page row, including `url` (the human-shareable in-app link).",
      inputSchema: updatePageInputSchema,
    },
    async (args) => {
      try {
        return ok(await updatePage(client, args));
      } catch (err) {
        return fail(err);
      }
    },
  );

  server.registerTool(
    "delete_page",
    {
      description: "Delete a page. Backlinks from other pages are preserved with the page's last-known title.",
      inputSchema: deletePageInputSchema,
    },
    async (args) => {
      try {
        return ok(await deletePage(client, args));
      } catch (err) {
        return fail(err);
      }
    },
  );

  server.registerTool(
    "add_comment",
    {
      description:
        "Attach a root comment to a page, anchored by a (prefix, exact, suffix) text triplet. Pass ~32 chars of context on each side of the selected text.",
      inputSchema: addCommentInputSchema,
    },
    async (args) => {
      try {
        return ok(await addComment(client, args));
      } catch (err) {
        return fail(err);
      }
    },
  );

  server.registerTool(
    "import_markdown",
    {
      description:
        "Bulk-import a directory of .md files into a space. Walks `local_path` recursively, preserves the directory structure as nested pages. 8 MiB total cap — split larger imports. Pass dry_run=true to preview without writing.",
      inputSchema: importMarkdownInputSchema,
    },
    async (args) => {
      try {
        return ok(await importMarkdown(client, args));
      } catch (err) {
        return fail(err);
      }
    },
  );

  server.registerTool(
    "import_mira",
    {
      description:
        "Import a single mira (mira.cagdas.io) page into a tela space. Provide either `source_url` (a `https://mira.cagdas.io/p/<slug>` link, fetched server-side) OR `payload` (the raw mira block JSON). The endpoint enforces an https-only host allowlist (default `mira.cagdas.io`), a 5s fetch timeout, and a 1 MiB cap on both request body and fetched response. Returns the created tela page wrapped as `{ page: ... }`. Use `parent_id` to nest under an existing page; omit for top-level.",
      inputSchema: importMiraInputSchema,
    },
    async (args) => {
      try {
        return ok(await importMira(client, args));
      } catch (err) {
        return fail(err);
      }
    },
  );

  server.registerTool(
    "create_space",
    {
      description:
        "Create a new Tela space. Creator auto-becomes owner. Requires write scope on the API key.",
      inputSchema: createSpaceInputSchema,
    },
    async (args) => {
      try {
        return ok(await createSpace(client, args));
      } catch (err) {
        return fail(err);
      }
    },
  );

  server.registerTool(
    "update_space",
    {
      description:
        "Patch a space's name and/or slug. Requires write scope on the API key AND owner role within the target space (backend rejects editors). At least one of name, slug must be provided.",
      inputSchema: updateSpaceInputSchema,
    },
    async (args) => {
      try {
        return ok(await updateSpace(client, args));
      } catch (err) {
        return fail(err);
      }
    },
  );

  server.registerTool(
    "delete_space",
    {
      description:
        "Delete a space AND all its pages, comments, revisions, share links. Irreversible cascade. Requires admin scope on the API key AND owner role within the target space.",
      inputSchema: deleteSpaceInputSchema,
    },
    async (args) => {
      try {
        return ok(await deleteSpace(client, args));
      } catch (err) {
        return fail(err);
      }
    },
  );

  server.registerTool(
    "submit_feedback",
    {
      description:
        "Submit feedback about Tela or the tela-mcp server itself: friction, bugs, missing capabilities, ergonomic issues. NOT for page content notes — use add_comment for those. Free-text subject + body, no fixed categories.",
      inputSchema: submitFeedbackInputSchema,
    },
    async (args) => {
      try {
        return ok(await submitFeedback(client, args));
      } catch (err) {
        return fail(err);
      }
    },
  );

  registerPageResource(server, client);

  return server;
}

async function main(): Promise<void> {
  const baseUrl = process.env.TELA_BASE_URL;
  const apiKey = process.env.TELA_API_KEY;
  if (!baseUrl || !apiKey) {
    const missing = [!baseUrl && "TELA_BASE_URL", !apiKey && "TELA_API_KEY"].filter(Boolean).join(", ");
    console.error(
      `[tela-mcp] missing required env: ${missing}. Set both before launching the server.`,
    );
    process.exit(1);
  }

  const version = readPackageVersion();
  // TELA_PUBLIC_URL is the origin users browse; falls back to TELA_BASE_URL
  // (correct when the backend is reached at its public URL). Used to emit
  // human-shareable page `url` fields.
  const client = new TelaClient({
    baseUrl,
    apiKey,
    publicBaseUrl: process.env.TELA_PUBLIC_URL,
  });

  // Fire-and-forget probe. Advisory only — never blocks tool calls.
  void runVersionCheck({ baseUrl, builtAgainst: version });

  const server = buildServer(client, version);
  const transport = new StdioServerTransport();
  await server.connect(transport);
}

// Determine whether this module is the process entrypoint.
//
// Direct invocation (`node dist/server.js`) and bin-symlink invocation
// (`npx -y tela-mcp@latest`, which spawns `node_modules/.bin/tela-mcp` → the
// real server.js) must both run main(). `path.resolve` does NOT follow
// symlinks, so a naive equality check between `process.argv[1]` and
// `fileURLToPath(import.meta.url)` returns false when invoked via the bin
// symlink — main() never runs and the process exits 0 silently with no MCP
// handshake response. Resolve symlinks on both sides via realpathSync to
// compare canonical filesystem paths.
function isProcessEntrypoint(): boolean {
  if (!process.argv[1]) return false;
  try {
    const argvReal = realpathSync(resolve(process.argv[1]));
    const moduleReal = realpathSync(fileURLToPath(import.meta.url));
    return argvReal === moduleReal;
  } catch {
    return false;
  }
}

if (isProcessEntrypoint()) {
  main().catch((err) => {
    console.error("[tela-mcp] fatal:", err);
    process.exit(1);
  });
}
