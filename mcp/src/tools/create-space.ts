// create_space posts a new Tela space. Creator auto-becomes owner per backend
// CreateSpace handler. Scope label on the MCP surface is `write`; backend
// enforces nothing beyond authentication on this endpoint, so a write-scope
// API key suffices.

import { z } from "zod";
import type { TelaClient } from "../client.js";

export const createSpaceInputSchema = {
  name: z.string().min(1).max(200).describe("Display name (1-200 chars). Required."),
  slug: z
    .string()
    .min(1)
    .max(100)
    .regex(/^[a-z0-9-]+$/)
    .optional()
    .describe("URL slug, lowercase alphanumeric + dashes. Optional — backend auto-slugs from name if omitted."),
};

const createSpaceArgs = z.object(createSpaceInputSchema);
export type CreateSpaceArgs = z.infer<typeof createSpaceArgs>;

interface SpaceRow {
  id: number;
  name: string;
  slug: string;
  created_at: string;
  updated_at: string;
}

interface CreateSpaceResponse {
  space: SpaceRow;
}

export async function createSpace(
  client: TelaClient,
  args: CreateSpaceArgs,
): Promise<SpaceRow> {
  const body: Record<string, unknown> = { name: args.name };
  if (args.slug !== undefined) body.slug = args.slug;
  const res = await client.postJSON<CreateSpaceResponse>("/api/spaces", body);
  return res.space;
}
