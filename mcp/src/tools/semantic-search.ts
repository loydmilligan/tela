import { z } from "zod";
import type { TelaClient } from "../client.js";

export const semanticSearchInputSchema = {
  query: z
    .string()
    .min(1)
    .describe("Natural-language query. Matches on meaning (embeddings) and keywords (BM25), fused."),
  space_id: z
    .number()
    .int()
    .positive()
    .optional()
    .describe("Optional — restrict to a single space. Omit to search every space the key can access."),
  limit: z.number().int().min(1).max(100).optional().describe("Max chunks to return (1-100). Default 20."),
  mode: z
    .enum(["hybrid", "semantic", "lexical"])
    .optional()
    .describe("Retrieval mode. hybrid (default) fuses vector + BM25; semantic = vectors only; lexical = BM25 only."),
};

const semanticSearchArgs = z.object(semanticSearchInputSchema);
export type SemanticSearchArgs = z.infer<typeof semanticSearchArgs>;

interface ChunkHit {
  chunk_id: number;
  page_id: number;
  space_id: number;
  title: string;
  heading_path: string;
  snippet: string;
  score: number;
}

interface SemanticSearchResponse {
  results: ChunkHit[];
}

export async function semanticSearch(
  client: TelaClient,
  args: SemanticSearchArgs,
): Promise<{ results: ChunkHit[] }> {
  const params: Record<string, string | number> = { q: args.query };
  if (args.space_id !== undefined) params.space_id = args.space_id;
  if (args.limit !== undefined) params.limit = args.limit;
  if (args.mode !== undefined) params.mode = args.mode;
  const res = await client.getJSON<SemanticSearchResponse>("/api/rag/search", params);
  return { results: res.results ?? [] };
}
