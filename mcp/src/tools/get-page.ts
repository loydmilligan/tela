import { z } from "zod";
import type { TelaClient } from "../client.js";
import { pageUrl } from "../slug.js";

export const getPageInputSchema = {
  id: z.number().int().positive().describe("Numeric Tela page id."),
};

const getPageArgs = z.object(getPageInputSchema);
export type GetPageArgs = z.infer<typeof getPageArgs>;

interface PageFull {
  id: number;
  title: string;
  body: string;
  space_id: number;
  parent_id: number | null;
  created_at: string;
  updated_at: string;
}

interface GetPageResponse {
  page: PageFull;
}

export async function getPage(
  client: TelaClient,
  args: GetPageArgs,
): Promise<PageFull & { url: string }> {
  const res = await client.getJSON<GetPageResponse>(`/api/pages/${args.id}`);
  const p = res.page;
  return {
    id: p.id,
    title: p.title,
    body: p.body,
    space_id: p.space_id,
    parent_id: p.parent_id,
    created_at: p.created_at,
    updated_at: p.updated_at,
    url: pageUrl(client.publicBaseUrl, p.space_id, p.id, p.title),
  };
}
