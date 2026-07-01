import { $nodeSchema } from '@milkdown/kit/utils'
import { editorViewCtx } from '@milkdown/kit/core'
import type { Ctx } from '@milkdown/ctx'
import { insertBlock } from '../../lib/milkdown/insert-block'

// Poll: a `:::poll{id}` container directive — a question (heading) + an option
// list, where each voter is a nested `- @username` under their chosen option.
// Votes live in the body (see backend internal/pollmd); the rich vote/results UI
// (bars, avatars, "see votes", click-to-vote) lives in the READ view
// (PollWidget). In the EDITOR this is a deliberately minimal round-trip schema —
// its only jobs are (1) survive Milkdown's strict parser so a poll isn't
// unwrapped/stripped on save (poll is in KNOWN_DIRECTIVE_NAMES), and (2) let you
// edit the question + options as ordinary block content. No custom node view:
// authoring is kept light on purpose; voting is a read-view concern.

interface MdastNode {
  type: string
  name?: string
  attributes?: Record<string, string | null | undefined>
  children?: MdastNode[]
}

export const pollSchema = $nodeSchema('poll', () => ({
  group: 'block',
  content: 'block+',
  defining: true,
  attrs: {
    pollId: { default: '', validate: 'string' },
    closed: { default: false, validate: 'boolean' },
  },
  parseDOM: [
    {
      tag: 'div[data-poll]',
      getAttrs: (dom) => ({
        pollId: dom instanceof HTMLElement ? (dom.dataset.pollId ?? '') : '',
        closed: dom instanceof HTMLElement ? dom.dataset.closed === 'true' : false,
      }),
    },
  ],
  toDOM: (node) => [
    'div',
    {
      'data-poll': '',
      'data-poll-id': node.attrs.pollId,
      'data-closed': node.attrs.closed ? 'true' : 'false',
      class: 'tela-poll',
    },
    0,
  ],
  parseMarkdown: {
    match: (node) =>
      node.type === 'containerDirective' && (node as MdastNode).name === 'poll',
    runner: (state, node, type) => {
      const attrs = (node as MdastNode).attributes ?? {}
      const pollId = attrs.id ?? ''
      const closed = attrs.closed != null
      const children = (node as MdastNode).children ?? []
      state.openNode(type, { pollId, closed })
      if (children.length > 0) {
        state.next(children as never)
      } else {
        // block+ must be satisfied — an empty poll gets one empty paragraph.
        const paraType = type.schema.nodes.paragraph
        if (paraType) {
          state.openNode(paraType)
          state.closeNode()
        }
      }
      state.closeNode()
    },
  },
  toMarkdown: {
    match: (node) => node.type.name === 'poll',
    runner: (state, node) => {
      const attributes: Record<string, string> = {}
      const pollId = (node.attrs.pollId as string) || ''
      if (pollId) attributes.id = pollId
      if (node.attrs.closed) attributes.closed = 'true'
      state.openNode('containerDirective', undefined, {
        name: 'poll',
        attributes,
      })
      state.next(node.content)
      state.closeNode()
    },
  },
}))

// Slash inserter: a poll skeleton — a question + two blank options to fill in.
// The id is stamped here so authors never have to think about it.
export function insertPoll(ctx: Ctx) {
  const view = ctx.get(editorViewCtx)
  const { schema } = view.state
  const pollType = schema.nodes.poll
  const headingType = schema.nodes.heading
  const listType = schema.nodes.bullet_list
  const itemType = schema.nodes.list_item
  const paraType = schema.nodes.paragraph
  if (!pollType || !headingType || !listType || !itemType || !paraType) return
  const pollId = `poll-${Date.now().toString(36)}`
  const heading = headingType.create(
    { level: 3 },
    schema.text('Your question?'),
  )
  const option = (label: string) =>
    itemType.create(null, paraType.create(null, schema.text(label)))
  const list = listType.create(null, [option('Option 1'), option('Option 2')])
  const node = pollType.create({ pollId }, [heading, list])
  insertBlock(view, node, { caret: 'none' })
}
