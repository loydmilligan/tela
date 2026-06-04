import { z } from "zod";
import type { TelaClient } from "../client.js";
import { pageUrl } from "../slug.js";

export const updatePageInputSchema = {
  id: z.number().int().positive().describe("Numeric Tela page id."),
  title: z.string().min(1).max(500).optional().describe("New title. Omit to leave unchanged."),
  body: z.string().optional().describe("New markdown body. Omit to leave unchanged."),
};

const updatePageArgs = z.object(updatePageInputSchema);
export type UpdatePageArgs = z.infer<typeof updatePageArgs>;

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

interface UpdatePageResponse {
  page: PageRow;
}

export async function updatePage(
  client: TelaClient,
  args: UpdatePageArgs,
): Promise<{ page: PageRow & { url: string } }> {
  if (args.title === undefined && args.body === undefined) {
    throw new Error("at least one of title, body must be provided");
  }
  const body: Record<string, unknown> = {};
  if (args.title !== undefined) body.title = args.title;
  if (args.body !== undefined) body.body = args.body;
  const res = await client.patchJSON<UpdatePageResponse>(`/api/pages/${args.id}`, body);
  const p = res.page;
  const url = pageUrl(client.publicBaseUrl, p.space_id, p.id, p.title);
  return { page: { ...p, url } };
}
