// import_markdown wraps the backend's POST /api/spaces/{id}/import endpoint.
//
// Walks the local directory, picks every `.md` file (other extensions are
// dropped — the backend would skip them anyway, but ignoring early saves
// upload bytes), and POSTs a multipart body where each file part's
// Content-Disposition filename is the path relative to the import root with
// forward-slash separators. The backend reads this raw filename via
// mime.ParseMediaType and treats it as the relative path for nesting.
//
// The 8 MiB total-size cap is a courtesy guard — the backend doesn't enforce
// it explicitly at the HTTP layer, but very large bundles are slow over a
// single request and the user should split them.

import { promises as fs } from "node:fs";
import { resolve, join, sep } from "node:path";
import { z } from "zod";
import type { TelaClient } from "../client.js";

const MAX_TOTAL_BYTES = 8 * 1024 * 1024;

export const importMarkdownInputSchema = {
  space_id: z.number().int().positive().describe("Target space id."),
  parent_id: z
    .number()
    .int()
    .positive()
    .optional()
    .describe("Optional parent page. Omit to import at the space root."),
  local_path: z
    .string()
    .min(1)
    .describe(
      "Absolute or relative path on the MCP server host to a directory containing .md files. Walks recursively.",
    ),
  dry_run: z
    .boolean()
    .optional()
    .describe("If true, the backend plans the import and returns the proposed tree without writing."),
};

const importMarkdownArgs = z.object(importMarkdownInputSchema);
export type ImportMarkdownArgs = z.infer<typeof importMarkdownArgs>;

interface ImportFile {
  rel: string;
  content: Buffer;
}

interface ImportSummary {
  created: number;
  skipped: number;
  conflicts_renamed: number;
}

interface ImportResponse {
  summary: ImportSummary;
  pages: unknown[];
  skipped: unknown[];
  errors: unknown[];
}

export async function importMarkdown(
  client: TelaClient,
  args: ImportMarkdownArgs,
): Promise<ImportResponse> {
  const root = resolve(args.local_path);
  let rootStat;
  try {
    rootStat = await fs.stat(root);
  } catch (err) {
    throw new Error(`local_path does not exist or is not readable: ${root} (${(err as Error).message})`);
  }
  if (!rootStat.isDirectory()) {
    throw new Error(`local_path must be a directory: ${root}`);
  }

  const files = await collectMarkdownFiles(root);
  if (files.length === 0) {
    throw new Error(`no .md files found under ${root}`);
  }

  let total = 0;
  for (const f of files) total += f.content.byteLength;
  if (total > MAX_TOTAL_BYTES) {
    const mib = (total / (1024 * 1024)).toFixed(2);
    throw new Error(
      `import bundle is ${mib} MiB which exceeds the 8 MiB cap. Split the directory into smaller batches and call import_markdown for each.`,
    );
  }

  const form = new FormData();
  if (args.parent_id !== undefined) form.append("parent_id", String(args.parent_id));
  if (args.dry_run) form.append("dry_run", "true");
  for (const f of files) {
    // The third argument is the form-data "filename" parameter. Forward-slash
    // separators are part of the value, not path-normalized by the FormData
    // implementation — Node's undici writes the bytes verbatim into the
    // Content-Disposition header.
    const blob = new Blob([new Uint8Array(f.content)], { type: "text/markdown" });
    form.append("files", blob, f.rel);
  }

  return client.postMultipart<ImportResponse>(
    `/api/spaces/${args.space_id}/import`,
    form,
  );
}

// collectMarkdownFiles walks `root` and returns every regular file whose name
// ends in `.md` (case-insensitive). The returned `rel` field uses forward
// slashes regardless of host OS — the multipart filename header is shared
// across clients and the backend always interprets `/` as the separator.
async function collectMarkdownFiles(root: string): Promise<ImportFile[]> {
  const out: ImportFile[] = [];
  await walk(root, root, out);
  out.sort((a, b) => (a.rel < b.rel ? -1 : a.rel > b.rel ? 1 : 0));
  return out;
}

async function walk(root: string, dir: string, out: ImportFile[]): Promise<void> {
  let entries: import("node:fs").Dirent[];
  try {
    entries = await fs.readdir(dir, { withFileTypes: true });
  } catch (err) {
    throw new Error(`failed to read directory ${dir}: ${(err as Error).message}`);
  }
  for (const ent of entries) {
    const abs = join(dir, ent.name);
    if (ent.isDirectory()) {
      await walk(root, abs, out);
      continue;
    }
    if (!ent.isFile()) continue;
    if (!ent.name.toLowerCase().endsWith(".md")) continue;
    const rel = abs.slice(root.length + 1).split(sep).join("/");
    const content = await fs.readFile(abs);
    out.push({ rel, content });
  }
}
