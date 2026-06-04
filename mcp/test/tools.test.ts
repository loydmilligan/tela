import { describe, expect, it } from "vitest";
import { TelaApiError } from "../src/client.js";
import { listSpaces } from "../src/tools/list-spaces.js";
import { listPages } from "../src/tools/list-pages.js";
import { getPage } from "../src/tools/get-page.js";
import { search } from "../src/tools/search.js";
import { searchBodies } from "../src/tools/search-bodies.js";
import { listBacklinks } from "../src/tools/list-backlinks.js";
import { makeFlakyClient, makeMockClient } from "./fixtures.js";

describe("list_spaces", () => {
  it("projects to {id,name,slug} and hits /api/spaces", async () => {
    const { client, requests } = makeMockClient({
      body: {
        spaces: [
          { id: 1, name: "Test Space", slug: "test-space", created_at: "x", updated_at: "y" },
          { id: 2, name: "Engineering", slug: "engineering", created_at: "x", updated_at: "y" },
        ],
      },
    });
    const out = await listSpaces(client);
    expect(out.spaces).toEqual([
      { id: 1, name: "Test Space", slug: "test-space" },
      { id: 2, name: "Engineering", slug: "engineering" },
    ]);
    expect(requests).toHaveLength(1);
    expect(requests[0].url).toBe("http://test.local/api/spaces");
    expect(requests[0].headers["Authorization"]).toBe("Bearer tela_pat_test");
  });
});

describe("list_pages", () => {
  it("passes space_id+parent_id query params", async () => {
    const { client, requests } = makeMockClient({
      body: {
        pages: [
          {
            id: 10,
            space_id: 1,
            parent_id: null,
            title: "Root",
            body: "ignored",
            position: 0,
            created_at: "x",
            updated_at: "y",
          },
        ],
      },
    });
    const out = await listPages(client, { space_id: 1, parent_id: 7 });
    expect(out.pages[0]).toEqual({ id: 10, title: "Root", parent_id: null, position: 0, space_id: 1 });
    expect(requests[0].url).toContain("space_id=1");
    expect(requests[0].url).toContain("parent_id=7");
  });

  it("omits parent_id when not provided", async () => {
    const { client, requests } = makeMockClient({ body: { pages: [] } });
    await listPages(client, { space_id: 4 });
    expect(requests[0].url).toContain("space_id=4");
    expect(requests[0].url).not.toContain("parent_id");
  });
});

describe("get_page", () => {
  it("unwraps the {page: ...} envelope", async () => {
    const { client, requests } = makeMockClient({
      body: {
        page: {
          id: 24,
          space_id: 4,
          parent_id: 1,
          title: "Mira",
          body: "# Hello",
          position: 0,
          created_at: "2026-05-20 10:00:00",
          updated_at: "2026-05-20 11:00:00",
        },
      },
    });
    const out = await getPage(client, { id: 24 });
    expect(out).toEqual({
      id: 24,
      title: "Mira",
      body: "# Hello",
      space_id: 4,
      parent_id: 1,
      created_at: "2026-05-20 10:00:00",
      updated_at: "2026-05-20 11:00:00",
      url: "http://test.local/spaces/4/pages/24/mira",
    });
    expect(requests[0].url).toBe("http://test.local/api/pages/24");
  });

  it("surfaces TelaApiError with {error,code} envelope on 403", async () => {
    const { client } = makeMockClient({
      status: 403,
      body: { error: "not a member", code: "forbidden" },
    });
    await expect(getPage(client, { id: 99 })).rejects.toMatchObject({
      code: "forbidden",
      status: 403,
    });
  });
});

describe("search", () => {
  it("maps page_id → id and projects {id,title,snippet}", async () => {
    const { client, requests } = makeMockClient({
      body: {
        results: [
          {
            page_id: 12,
            space_id: 1,
            title: "Hit",
            snippet: "before <mark>term</mark> after",
            breadcrumb: ["Folder"],
          },
        ],
      },
    });
    const out = await search(client, { query: "term", limit: 5 });
    expect(out.results).toEqual([{ id: 12, title: "Hit", snippet: "before <mark>term</mark> after" }]);
    expect(requests[0].url).toContain("q=term");
    expect(requests[0].url).toContain("limit=5");
  });
});

describe("search_bodies", () => {
  it("requires space_id and passes it through", async () => {
    const { client, requests } = makeMockClient({
      body: { results: [{ id: 7, title: "Match", score: 0.42 }] },
    });
    const out = await searchBodies(client, { query: "mermaid", space_id: 4 });
    expect(out.results).toEqual([{ id: 7, title: "Match", score: 0.42 }]);
    expect(requests[0].url).toContain("/api/search/bodies");
    expect(requests[0].url).toContain("space_id=4");
    expect(requests[0].url).toContain("q=mermaid");
  });

  it("surfaces 400 invalid_query envelope from server-side sanitiser", async () => {
    const { client } = makeMockClient({
      status: 400,
      body: { error: "q is required", code: "invalid_query" },
    });
    await expect(searchBodies(client, { query: " ", space_id: 4 })).rejects.toBeInstanceOf(TelaApiError);
  });
});

describe("list_backlinks", () => {
  it("maps page_id → id and forwards space_id + title", async () => {
    const { client, requests } = makeMockClient({
      body: {
        backlinks: [
          { page_id: 33, space_id: 1, space_name: "Test", title: "Source A", breadcrumb: [], snippet: "…" },
        ],
      },
    });
    const out = await listBacklinks(client, { page_id: 10 });
    expect(out.backlinks).toEqual([{ id: 33, title: "Source A", space_id: 1 }]);
    expect(requests[0].url).toBe("http://test.local/api/pages/10/backlinks");
  });
});

describe("client retry behaviour", () => {
  it("retries once on 5xx then succeeds", async () => {
    const { client, requests } = makeFlakyClient([
      { status: 503, body: { error: "down", code: "internal" } },
      { status: 200, body: { spaces: [] } },
    ]);
    const out = await listSpaces(client);
    expect(out.spaces).toEqual([]);
    expect(requests).toHaveLength(2);
  });
});
