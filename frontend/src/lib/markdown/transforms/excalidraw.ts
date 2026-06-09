// Pure, Milkdown-free parsing of ```excalidraw fences. SINGLE SOURCE shared by
// the Milkdown editor (milkdown-excalidraw.ts wraps this in `$remark` + builds
// the atom schema) and the view renderer's parser (lib/markdown/remark-stack.ts).
// The view renders the node as the server-rendered PNG, so it never loads the
// excalidraw lib. See docs/view-edit-split.md.

export interface MdastNode {
  type: string
  value?: string
  lang?: string
  children?: MdastNode[]
  sceneHash?: string
  altText?: string
  sceneJSON?: string
  [k: string]: unknown
}

interface ExcalidrawSceneJSON {
  scene_hash?: unknown
  alt_text?: unknown
  [k: string]: unknown
}

const SCENE_HASH_RE = /^[a-f0-9]{8,64}$/

// Walk the mdast tree and rewrite qualifying ```excalidraw fences into
// `excalidraw` mdast nodes in place. Recurses into block children so excalidraw
// fences inside blockquotes / details work too.
export function transformExcalidrawInMdast(node: MdastNode): void {
  if (
    node.type === 'code' &&
    typeof node.lang === 'string' &&
    node.lang === 'excalidraw' &&
    typeof node.value === 'string'
  ) {
    const raw = node.value
    let parsed: ExcalidrawSceneJSON | null
    try {
      parsed = JSON.parse(raw) as ExcalidrawSceneJSON
    } catch {
      parsed = null
    }
    if (parsed && typeof parsed === 'object') {
      const sceneHash = typeof parsed.scene_hash === 'string' ? parsed.scene_hash : ''
      const altText = typeof parsed.alt_text === 'string' ? parsed.alt_text : ''
      if (SCENE_HASH_RE.test(sceneHash)) {
        node.type = 'excalidraw'
        node.sceneHash = sceneHash
        node.altText = altText
        node.sceneJSON = raw
        delete node.lang
        delete node.value
        return
      }
    }
    // Parse failure or invalid scene_hash: leave as plain code block.
  }
  if (Array.isArray(node.children)) {
    for (const child of node.children) {
      transformExcalidrawInMdast(child)
    }
  }
}

export function excalidrawRemark() {
  return (tree: unknown) => {
    transformExcalidrawInMdast(tree as unknown as MdastNode)
  }
}
