import { z } from "zod";
import type { TelaClient } from "../client.js";

export const semanticSearchInputSchema = {
  query: z
    .string()
    .min(1)
    .describe("Natural-language question or phrase. Matched by meaning (vector) and keywords (lexical)."),
  space_id: z
    .number()
    .int()
    .positive()
    .optional()
    .describe("Optional — restrict to a single space. Omit to search every space the key can access."),
  limit: z.number().int().min(1).max(50).optional().describe("Max chunks to return (1-50). Backend default 20."),
  mode: z
    .enum(["hybrid", "semantic", "lexical"])
    .optional()
    .describe("Retrieval mode: 'hybrid' (default, RRF-fused vector+keyword), 'semantic' (vector only), 'lexical' (keyword only)."),
};

const semanticSearchArgs = z.object(semanticSearchInputSchema);
export type SemanticSearchArgs = z.infer<typeof semanticSearchArgs>;

interface ChunkHitRow {
  chunk_id: number;
  page_id: number;
  space_id: number;
  title: string;
  heading_path: string;
  snippet: string;
  score: number;
  updated_at: string;
}

interface SemanticSearchResponse {
  results: ChunkHitRow[];
}

// Each hit is a citeable chunk: chunk_id identifies it for read_chunk, page_id +
// heading_path locate the source so a downstream answer is verifiable. Read the
// full section with read_chunk(chunk_id), or the whole page with get_page.
export interface SemanticSearchHit {
  chunk_id: number;
  page_id: number;
  title: string;
  heading_path: string;
  snippet: string;
  score: number;
  updated_at: string;
}

export async function semanticSearch(
  client: TelaClient,
  args: SemanticSearchArgs,
): Promise<{ results: SemanticSearchHit[] }> {
  const params: Record<string, string | number> = { q: args.query };
  if (args.space_id !== undefined) params.space_id = args.space_id;
  if (args.limit !== undefined) params.limit = args.limit;
  if (args.mode !== undefined) params.mode = args.mode;
  const res = await client.getJSON<SemanticSearchResponse>("/api/rag/search", params);
  const results = (res.results ?? []).map((h) => ({
    chunk_id: h.chunk_id,
    page_id: h.page_id,
    title: h.title,
    heading_path: h.heading_path,
    snippet: h.snippet,
    score: h.score,
    updated_at: h.updated_at,
  }));
  return { results };
}
