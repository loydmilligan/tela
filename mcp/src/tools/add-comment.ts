// add_comment posts a root comment anchored by the surrounding text triplet
// (prefix / exact / suffix), matching the M8 text-fingerprint anchoring
// scheme. The backend stores anchor_prefix / anchor_exact / anchor_suffix as
// three flat columns; the tool input nests them under `anchor` so the agent
// has a clearer mental model. Reply-mode (parent_id) is intentionally
// out-of-scope for v0 — agents create root threads only.

import { z } from "zod";
import type { TelaClient } from "../client.js";

export const addCommentInputSchema = {
  page_id: z.number().int().positive().describe("Page to attach the comment to."),
  anchor: z
    .object({
      prefix: z
        .string()
        .describe("~32 chars of text immediately preceding the exact selection."),
      exact: z.string().min(1).describe("The selected text (non-empty)."),
      suffix: z
        .string()
        .describe("~32 chars of text immediately following the exact selection."),
    })
    .describe("Text-fingerprint anchor — re-located by matching the triplet against the live body."),
  body: z.string().min(1).max(10_000).describe("Comment body (1-10000 chars)."),
};

const addCommentArgs = z.object(addCommentInputSchema);
export type AddCommentArgs = z.infer<typeof addCommentArgs>;

interface CommentRow {
  id: number;
  page_id: number;
  parent_id: number | null;
  author_id: number;
  author_username: string;
  body: string;
  anchor_prefix: string | null;
  anchor_exact: string | null;
  anchor_suffix: string | null;
  resolved: boolean;
  created_at: string;
  updated_at: string;
}

interface CreateCommentResponse {
  comment: CommentRow;
}

export async function addComment(
  client: TelaClient,
  args: AddCommentArgs,
): Promise<{ comment: CommentRow }> {
  const payload = {
    body: args.body,
    anchor_prefix: args.anchor.prefix,
    anchor_exact: args.anchor.exact,
    anchor_suffix: args.anchor.suffix,
  };
  const res = await client.postJSON<CreateCommentResponse>(
    `/api/pages/${args.page_id}/comments`,
    payload,
  );
  return { comment: res.comment };
}
