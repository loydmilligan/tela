import { describe, expect, it } from "vitest";
import { z } from "zod";
import { TelaApiError } from "../src/client.js";
import { createSpace, createSpaceInputSchema } from "../src/tools/create-space.js";
import { updateSpace, updateSpaceInputSchema } from "../src/tools/update-space.js";
import { deleteSpace, deleteSpaceInputSchema } from "../src/tools/delete-space.js";
import { makeEmptyResponseClient, makeFlakyClient, makeMockClient } from "./fixtures.js";

describe("create_space", () => {
  it("POSTs JSON to /api/spaces and returns the unwrapped space row", async () => {
    const { client, requests } = makeMockClient({
      status: 201,
      body: {
        space: {
          id: 42,
          name: "Research",
          slug: "research",
          created_at: "2026-05-20 22:00:00",
          updated_at: "2026-05-20 22:00:00",
        },
      },
    });
    const out = await createSpace(client, { name: "Research", slug: "research" });
    expect(out.id).toBe(42);
    expect(out.slug).toBe("research");
    expect(requests).toHaveLength(1);
    expect(requests[0].method).toBe("POST");
    expect(requests[0].url).toBe("http://test.local/api/spaces");
    expect(requests[0].headers["Content-Type"]).toBe("application/json");
    expect(JSON.parse(requests[0].body as string)).toEqual({ name: "Research", slug: "research" });
  });

  it("omits slug from the JSON body when not provided", async () => {
    const { client, requests } = makeMockClient({
      status: 201,
      body: {
        space: {
          id: 1,
          name: "Auto Slug",
          slug: "auto-slug",
          created_at: "x",
          updated_at: "y",
        },
      },
    });
    await createSpace(client, { name: "Auto Slug" });
    expect(JSON.parse(requests[0].body as string)).toEqual({ name: "Auto Slug" });
  });

  it("surfaces 400 bad_request envelope as TelaApiError", async () => {
    const { client } = makeMockClient({
      status: 400,
      body: { error: "name is required", code: "invalid_name" },
    });
    await expect(createSpace(client, { name: "x" })).rejects.toMatchObject({
      status: 400,
      code: "invalid_name",
    });
  });

  it("retries once on 5xx then succeeds", async () => {
    const { client, requests } = makeFlakyClient([
      { status: 502, body: { error: "boom", code: "internal" } },
      {
        status: 201,
        body: {
          space: {
            id: 99,
            name: "After Retry",
            slug: "after-retry",
            created_at: "x",
            updated_at: "y",
          },
        },
      },
    ]);
    const out = await createSpace(client, { name: "After Retry" });
    expect(out.id).toBe(99);
    expect(requests).toHaveLength(2);
  });
});

describe("update_space", () => {
  it("PATCHes /api/spaces/{id} with only the provided fields and unwraps the envelope", async () => {
    const { client, requests } = makeMockClient({
      body: {
        space: {
          id: 4,
          name: "Renamed",
          slug: "renamed",
          created_at: "x",
          updated_at: "y",
        },
      },
    });
    const out = await updateSpace(client, { id: 4, name: "Renamed" });
    expect(out.id).toBe(4);
    expect(out.name).toBe("Renamed");
    expect(requests[0].method).toBe("PATCH");
    expect(requests[0].url).toBe("http://test.local/api/spaces/4");
    expect(JSON.parse(requests[0].body as string)).toEqual({ name: "Renamed" });
  });

  it("rejects client-side when neither name nor slug is provided", async () => {
    const { client } = makeMockClient({ body: { space: {} } });
    await expect(
      updateSpace(client, { id: 1 } as unknown as { id: number; name: string }),
    ).rejects.toThrow(/at least one of name, slug/);
  });

  it("surfaces 403 api_key_scope envelope as TelaApiError", async () => {
    const { client } = makeMockClient({
      status: 403,
      body: { error: "write scope required", code: "api_key_scope" },
    });
    await expect(updateSpace(client, { id: 4, slug: "x" })).rejects.toMatchObject({
      status: 403,
      code: "api_key_scope",
    });
  });

  it("surfaces 404 not_found envelope as TelaApiError", async () => {
    const { client } = makeMockClient({
      status: 404,
      body: { error: "space not found", code: "not_found" },
    });
    await expect(updateSpace(client, { id: 999, name: "X" })).rejects.toBeInstanceOf(TelaApiError);
  });
});

describe("delete_space", () => {
  it("DELETEs /api/spaces/{id} and returns ok:true on 204", async () => {
    const { client, requests } = makeEmptyResponseClient(204);
    const out = await deleteSpace(client, { id: 42 });
    expect(out).toEqual({ ok: true });
    expect(requests[0].method).toBe("DELETE");
    expect(requests[0].url).toBe("http://test.local/api/spaces/42");
  });

  it("surfaces 403 forbidden envelope as TelaApiError", async () => {
    const { client } = makeMockClient({
      status: 403,
      body: { error: "owner role required", code: "forbidden" },
    });
    await expect(deleteSpace(client, { id: 7 })).rejects.toMatchObject({
      status: 403,
      code: "forbidden",
    });
  });

  it("surfaces 404 not_found envelope as TelaApiError", async () => {
    const { client } = makeMockClient({
      status: 404,
      body: { error: "space not found", code: "not_found" },
    });
    await expect(deleteSpace(client, { id: 999 })).rejects.toBeInstanceOf(TelaApiError);
  });
});

describe("space tool input schemas", () => {
  const createParse = z.object(createSpaceInputSchema).safeParse.bind(
    z.object(createSpaceInputSchema),
  );
  const updateParse = z.object(updateSpaceInputSchema).safeParse.bind(
    z.object(updateSpaceInputSchema),
  );
  const deleteParse = z.object(deleteSpaceInputSchema).safeParse.bind(
    z.object(deleteSpaceInputSchema),
  );

  it("rejects an empty name", () => {
    expect(createParse({ name: "" }).success).toBe(false);
  });

  it("rejects a name over 200 chars", () => {
    expect(createParse({ name: "x".repeat(201) }).success).toBe(false);
  });

  it("rejects an uppercase slug", () => {
    expect(createParse({ name: "ok", slug: "Bad-Slug" }).success).toBe(false);
  });

  it("rejects a slug containing underscores", () => {
    expect(createParse({ name: "ok", slug: "bad_slug" }).success).toBe(false);
  });

  it("rejects a slug over 100 chars", () => {
    expect(createParse({ name: "ok", slug: "a".repeat(101) }).success).toBe(false);
  });

  it("accepts a valid lowercase-dash slug at 100 chars", () => {
    expect(createParse({ name: "ok", slug: "a".repeat(100) }).success).toBe(true);
  });

  it("update_space accepts a single field", () => {
    expect(updateParse({ id: 1, name: "Renamed" }).success).toBe(true);
    expect(updateParse({ id: 1, slug: "renamed" }).success).toBe(true);
  });

  it("update_space rejects an invalid id", () => {
    expect(updateParse({ id: 0, name: "x" }).success).toBe(false);
    expect(updateParse({ id: -1, name: "x" }).success).toBe(false);
  });

  it("delete_space requires a positive id", () => {
    expect(deleteParse({ id: 0 }).success).toBe(false);
    expect(deleteParse({ id: 1 }).success).toBe(true);
  });
});
