import { z } from "zod";
import type { TelaClient } from "../client.js";

export const readChunkInputSchema = {
  chunk_id: z
    .number()
    .int()
    .positive()
    .describe("The chunk_id from a semantic_search result. Returns that section's full text."),
};

const readChunkArgs = z.object(readChunkInputSchema);
export type ReadChunkArgs = z.infer<typeof readChunkArgs>;

interface ChunkRow {
  chunk_id: number;
  page_id: number;
  space_id: number;
  title: string;
  heading_path: string;
  content: string;
  updated_at: string;
}

interface ReadChunkResponse {
  chunk: ChunkRow;
}

// Wraps the row in a named { chunk } envelope, mirroring the other row-returning
// MCP tools.
export async function readChunk(client: TelaClient, args: ReadChunkArgs): Promise<{ chunk: ChunkRow }> {
  const res = await client.getJSON<ReadChunkResponse>("/api/rag/chunk", { chunk_id: args.chunk_id });
  return { chunk: res.chunk };
}
