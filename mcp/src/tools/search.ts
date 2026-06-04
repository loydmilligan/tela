import { z } from "zod";
import type { TelaClient } from "../client.js";

export const searchInputSchema = {
  query: z.string().min(1).describe("Search phrase, matched across page titles and bodies."),
  space_id: z
    .number()
    .int()
    .positive()
    .optional()
    .describe("Optional — restrict to a single space. Omit to search every space the key can access."),
  limit: z.number().int().min(1).max(50).optional().describe("Max results to return (1-50). Backend default 25."),
};

const searchArgs = z.object(searchInputSchema);
export type SearchArgs = z.infer<typeof searchArgs>;

interface SearchHitRow {
  page_id: number;
  space_id: number;
  title: string;
  snippet: string;
  breadcrumb?: string[];
}

interface SearchResponse {
  results: SearchHitRow[];
}

export interface SearchResult {
  id: number;
  title: string;
  snippet: string;
}

export async function search(client: TelaClient, args: SearchArgs): Promise<{ results: SearchResult[] }> {
  const params: Record<string, string | number> = { q: args.query };
  if (args.space_id !== undefined) params.space_id = args.space_id;
  if (args.limit !== undefined) params.limit = args.limit;
  const res = await client.getJSON<SearchResponse>("/api/search", params);
  const results = (res.results ?? []).map((h) => ({
    id: h.page_id,
    title: h.title,
    snippet: h.snippet,
  }));
  return { results };
}
