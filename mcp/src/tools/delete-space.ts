// delete_space hard-deletes a space and (via ON DELETE CASCADE) every
// page, comment, share link, and revision under it. Irreversible. Backend
// requires the caller to hold the owner role within the space. The MCP tool
// is tagged `admin` scope so only admin-tier API keys can call it — that is
// defense-in-depth ABOVE the backend gate, not in place of it.

import { z } from "zod";
import type { TelaClient } from "../client.js";

export const deleteSpaceInputSchema = {
  id: z
    .number()
    .int()
    .positive()
    .describe("Numeric Tela space id. ALL pages in the space are cascade-deleted — operation is irreversible."),
};

const deleteSpaceArgs = z.object(deleteSpaceInputSchema);
export type DeleteSpaceArgs = z.infer<typeof deleteSpaceArgs>;

export async function deleteSpace(
  client: TelaClient,
  args: DeleteSpaceArgs,
): Promise<{ ok: true }> {
  await client.deleteVoid(`/api/spaces/${args.id}`);
  return { ok: true };
}
