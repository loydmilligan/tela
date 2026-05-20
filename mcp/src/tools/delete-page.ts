import { z } from "zod";
import type { TelaClient } from "../client.js";

export const deletePageInputSchema = {
  id: z.number().int().positive().describe("Numeric Tela page id."),
};

const deletePageArgs = z.object(deletePageInputSchema);
export type DeletePageArgs = z.infer<typeof deletePageArgs>;

export async function deletePage(
  client: TelaClient,
  args: DeletePageArgs,
): Promise<{ ok: true }> {
  await client.deleteVoid(`/api/pages/${args.id}`);
  return { ok: true };
}
