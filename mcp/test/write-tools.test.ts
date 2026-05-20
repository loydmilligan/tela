import { describe, expect, it } from "vitest";
import { mkdtemp, writeFile, mkdir } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { TelaApiError } from "../src/client.js";
import { createPage } from "../src/tools/create-page.js";
import { updatePage } from "../src/tools/update-page.js";
import { deletePage } from "../src/tools/delete-page.js";
import { addComment } from "../src/tools/add-comment.js";
import { importMarkdown } from "../src/tools/import-markdown.js";
import { makeEmptyResponseClient, makeMockClient } from "./fixtures.js";

describe("create_page", () => {
  it("POSTs JSON body to /api/pages and returns the unwrapped page", async () => {
    const { client, requests } = makeMockClient({
      status: 201,
      body: {
        page: {
          id: 99,
          space_id: 4,
          parent_id: 1,
          title: "Created",
          body: "hello",
          position: 0,
          created_at: "2026-05-20 12:00:00",
          updated_at: "2026-05-20 12:00:00",
        },
      },
    });
    const out = await createPage(client, {
      space_id: 4,
      parent_id: 1,
      title: "Created",
      body: "hello",
    });
    expect(out.page.id).toBe(99);
    expect(requests).toHaveLength(1);
    expect(requests[0].method).toBe("POST");
    expect(requests[0].url).toBe("http://test.local/api/pages");
    expect(requests[0].headers["Content-Type"]).toBe("application/json");
    expect(JSON.parse(requests[0].body as string)).toEqual({
      space_id: 4,
      parent_id: 1,
      title: "Created",
      body: "hello",
    });
  });

  it("omits parent_id from the JSON body when not provided", async () => {
    const { client, requests } = makeMockClient({
      status: 201,
      body: { page: { id: 1, space_id: 1, parent_id: null, title: "t", body: "b", position: 0, created_at: "x", updated_at: "y" } },
    });
    await createPage(client, { space_id: 1, title: "t", body: "b" });
    expect(JSON.parse(requests[0].body as string)).toEqual({ space_id: 1, title: "t", body: "b" });
  });

  it("surfaces a 403 forbidden envelope as TelaApiError", async () => {
    const { client } = makeMockClient({
      status: 403,
      body: { error: "editor or owner role required", code: "forbidden" },
    });
    await expect(
      createPage(client, { space_id: 4, title: "x", body: "" }),
    ).rejects.toMatchObject({ status: 403, code: "forbidden" });
  });
});

describe("update_page", () => {
  it("PATCHes /api/pages/{id} with only the provided fields", async () => {
    const { client, requests } = makeMockClient({
      body: {
        page: {
          id: 24,
          space_id: 4,
          parent_id: null,
          title: "New",
          body: "old body",
          position: 0,
          created_at: "x",
          updated_at: "y",
        },
      },
    });
    await updatePage(client, { id: 24, title: "New" });
    expect(requests[0].method).toBe("PATCH");
    expect(requests[0].url).toBe("http://test.local/api/pages/24");
    expect(JSON.parse(requests[0].body as string)).toEqual({ title: "New" });
  });

  it("rejects when neither title nor body is provided", async () => {
    const { client } = makeMockClient({ body: { page: {} } });
    await expect(updatePage(client, { id: 1 } as unknown as { id: number; title: string })).rejects.toThrow(
      /at least one of title, body/,
    );
  });

  it("surfaces 404 not_found envelope as TelaApiError", async () => {
    const { client } = makeMockClient({
      status: 404,
      body: { error: "page not found", code: "not_found" },
    });
    await expect(updatePage(client, { id: 999, body: "x" })).rejects.toBeInstanceOf(TelaApiError);
  });
});

describe("delete_page", () => {
  it("DELETEs /api/pages/{id} and returns ok:true on 204", async () => {
    const { client, requests } = makeEmptyResponseClient(204);
    const out = await deletePage(client, { id: 42 });
    expect(out).toEqual({ ok: true });
    expect(requests[0].method).toBe("DELETE");
    expect(requests[0].url).toBe("http://test.local/api/pages/42");
  });

  it("surfaces 403 forbidden envelope as TelaApiError", async () => {
    const { client } = makeMockClient({
      status: 403,
      body: { error: "editor or owner role required", code: "forbidden" },
    });
    await expect(deletePage(client, { id: 7 })).rejects.toMatchObject({
      status: 403,
      code: "forbidden",
    });
  });
});

describe("add_comment", () => {
  it("flattens the nested anchor into anchor_prefix/exact/suffix on the wire", async () => {
    const { client, requests } = makeMockClient({
      status: 201,
      body: {
        comment: {
          id: 5,
          page_id: 24,
          parent_id: null,
          author_id: 2,
          author_username: "admin",
          body: "nit",
          anchor_prefix: "before",
          anchor_exact: "X",
          anchor_suffix: "after",
          resolved: false,
          created_at: "x",
          updated_at: "y",
        },
      },
    });
    await addComment(client, {
      page_id: 24,
      body: "nit",
      anchor: { prefix: "before", exact: "X", suffix: "after" },
    });
    expect(requests[0].method).toBe("POST");
    expect(requests[0].url).toBe("http://test.local/api/pages/24/comments");
    expect(JSON.parse(requests[0].body as string)).toEqual({
      body: "nit",
      anchor_prefix: "before",
      anchor_exact: "X",
      anchor_suffix: "after",
    });
  });

  it("surfaces comment_no_anchor envelope on empty anchor", async () => {
    const { client } = makeMockClient({
      status: 400,
      body: {
        error: "root comments require anchor_prefix, anchor_exact, anchor_suffix",
        code: "comment_no_anchor",
      },
    });
    await expect(
      addComment(client, { page_id: 1, body: "x", anchor: { prefix: "", exact: "Y", suffix: "" } }),
    ).rejects.toMatchObject({ code: "comment_no_anchor" });
  });
});

describe("import_markdown", () => {
  it("walks a directory, builds a multipart body with relative paths, and POSTs to /api/spaces/{id}/import", async () => {
    const dir = await mkdtemp(join(tmpdir(), "telamcp-imp-"));
    await writeFile(join(dir, "root.md"), "# Root");
    await mkdir(join(dir, "nested"), { recursive: true });
    await writeFile(join(dir, "nested", "a.md"), "# A");
    await writeFile(join(dir, "nested", "b.md"), "# B");
    await writeFile(join(dir, "nested", "skip.txt"), "ignored"); // non-md

    const { client, requests } = makeMockClient({
      body: {
        summary: { created: 3, skipped: 0, conflicts_renamed: 0 },
        pages: [],
        skipped: [],
        errors: [],
      },
    });

    const out = await importMarkdown(client, {
      space_id: 4,
      parent_id: 1,
      local_path: dir,
    });
    expect(out.summary).toEqual({ created: 3, skipped: 0, conflicts_renamed: 0 });

    expect(requests).toHaveLength(1);
    expect(requests[0].method).toBe("POST");
    expect(requests[0].url).toBe("http://test.local/api/spaces/4/import");

    const body = requests[0].body as FormData;
    expect(body).toBeInstanceOf(FormData);

    expect(body.get("parent_id")).toBe("1");
    expect(body.get("dry_run")).toBeNull(); // not requested

    const fileParts = body.getAll("files");
    expect(fileParts).toHaveLength(3);

    // FormData entries preserve insertion order; collectMarkdownFiles sorts
    // alphabetically, so we expect nested/a.md, nested/b.md, root.md.
    const names = fileParts.map((p) => (p instanceof File ? p.name : String(p)));
    expect(names).toEqual(["nested/a.md", "nested/b.md", "root.md"]);
  });

  it("forwards dry_run=true on the multipart body", async () => {
    const dir = await mkdtemp(join(tmpdir(), "telamcp-imp-dry-"));
    await writeFile(join(dir, "one.md"), "# One");
    const { client, requests } = makeMockClient({
      body: { summary: { created: 0, skipped: 0, conflicts_renamed: 0 }, pages: [], skipped: [], errors: [] },
    });
    await importMarkdown(client, { space_id: 1, local_path: dir, dry_run: true });
    const body = requests[0].body as FormData;
    expect(body.get("dry_run")).toBe("true");
    expect(body.get("parent_id")).toBeNull();
  });

  it("rejects when the directory contains zero .md files", async () => {
    const dir = await mkdtemp(join(tmpdir(), "telamcp-imp-empty-"));
    await writeFile(join(dir, "readme.txt"), "no md here");
    const { client } = makeMockClient({ body: {} });
    await expect(importMarkdown(client, { space_id: 1, local_path: dir })).rejects.toThrow(
      /no \.md files/,
    );
  });

  it("rejects when local_path is not a directory", async () => {
    const { client } = makeMockClient({ body: {} });
    await expect(
      importMarkdown(client, { space_id: 1, local_path: "/does/not/exist-telamcp" }),
    ).rejects.toThrow(/does not exist|not a directory/);
  });

  it("enforces the 8 MiB cap with a hint to split", async () => {
    const dir = await mkdtemp(join(tmpdir(), "telamcp-imp-big-"));
    // 9 MiB single .md — over the 8 MiB cap. Write a single large file so we
    // don't blow out the test runtime walking many entries.
    const huge = Buffer.alloc(9 * 1024 * 1024, "x");
    await writeFile(join(dir, "huge.md"), huge);
    const { client } = makeMockClient({ body: {} });
    await expect(importMarkdown(client, { space_id: 1, local_path: dir })).rejects.toThrow(
      /8 MiB/,
    );
  });
});
