// update_space patches the display name and/or slug of an existing space.
// Backend requires editor-or-owner role within the space — bearer keys with
// write scope still 403 if the underlying user is only a viewer of the target
// space. Mirrors the at-least-one client-side guard that update_page uses to
// short-circuit obviously-malformed calls before the round-trip.

import { z } from "zod";
import type { TelaClient } from "../client.js";

export const updateSpaceInputSchema = {
  id: z.number().int().positive().describe("Numeric Tela space id."),
  name: z.string().min(1).max(200).optional().describe("New display name. Omit to leave unchanged."),
  slug: z
    .string()
    .min(1)
    .max(100)
    .regex(/^[a-z0-9-]+$/)
    .optional()
    .describe("New URL slug, lowercase alphanumeric + dashes. Omit to leave unchanged."),
};

const updateSpaceArgs = z.object(updateSpaceInputSchema);
export type UpdateSpaceArgs = z.infer<typeof updateSpaceArgs>;

interface SpaceRow {
  id: number;
  name: string;
  slug: string;
  created_at: string;
  updated_at: string;
}

interface UpdateSpaceResponse {
  space: SpaceRow;
}

export async function updateSpace(
  client: TelaClient,
  args: UpdateSpaceArgs,
): Promise<{ space: SpaceRow }> {
  if (args.name === undefined && args.slug === undefined) {
    throw new Error("at least one of name, slug must be provided");
  }
  const body: Record<string, unknown> = {};
  if (args.name !== undefined) body.name = args.name;
  if (args.slug !== undefined) body.slug = args.slug;
  const res = await client.patchJSON<UpdateSpaceResponse>(`/api/spaces/${args.id}`, body);
  return { space: res.space };
}
