import { z } from "zod";
import type { TelaClient } from "../client.js";

export const searchBodiesInputSchema = {
  query: z.string().min(1).describe("Search phrase, matched across page titles and bodies."),
  space_id: z
    .number()
    .int()
    .positive()
    .describe("Required. The backend endpoint is per-space."),
  limit: z.number().int().min(1).max(100).optional().describe("Max results to return (1-100). Backend default 20."),
};

const searchBodiesArgs = z.object(searchBodiesInputSchema);
export type SearchBodiesArgs = z.infer<typeof searchBodiesArgs>;

interface SearchBodyHitRow {
  id: number;
  title: string;
  score: number;
}

interface SearchBodiesResponse {
  results: SearchBodyHitRow[];
}

export async function searchBodies(
  client: TelaClient,
  args: SearchBodiesArgs,
): Promise<{ results: SearchBodyHitRow[] }> {
  const params: Record<string, string | number> = { q: args.query, space_id: args.space_id };
  if (args.limit !== undefined) params.limit = args.limit;
  const res = await client.getJSON<SearchBodiesResponse>("/api/search/bodies", params);
  return { results: res.results ?? [] };
}
