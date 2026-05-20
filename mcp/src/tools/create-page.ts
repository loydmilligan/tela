import { z } from "zod";
import type { TelaClient } from "../client.js";

export const createPageInputSchema = {
  space_id: z.number().int().positive().describe("Numeric Tela space id."),
  parent_id: z
    .number()
    .int()
    .positive()
    .optional()
    .describe("Optional parent page id. Omit to create at the space root."),
  title: z.string().min(1).max(500).describe("Page title (1-500 chars after trim)."),
  body: z.string().describe("Markdown body. May be empty."),
};

const createPageArgs = z.object(createPageInputSchema);
export type CreatePageArgs = z.infer<typeof createPageArgs>;

interface PageRow {
  id: number;
  title: string;
  body: string;
  space_id: number;
  parent_id: number | null;
  position: number;
  created_at: string;
  updated_at: string;
}

interface CreatePageResponse {
  page: PageRow;
}

export async function createPage(
  client: TelaClient,
  args: CreatePageArgs,
): Promise<{ page: PageRow }> {
  const body: Record<string, unknown> = {
    space_id: args.space_id,
    title: args.title,
    body: args.body,
  };
  if (args.parent_id !== undefined) body.parent_id = args.parent_id;
  const res = await client.postJSON<CreatePageResponse>("/api/pages", body);
  return { page: res.page };
}
