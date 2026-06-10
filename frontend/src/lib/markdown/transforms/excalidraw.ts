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
  diagramId?: string
  [k: string]: unknown
}

interface ExcalidrawSceneJSON {
  scene_hash?: unknown
  alt_text?: unknown
  // Stable, save-invariant id (unlike scene_hash, which changes on every
  // edit). Used as the live-collab room key so peers and late joiners share a
  // session across saves/checkpoints. Absent on legacy diagrams → '' → callers
  // fall back to scene_hash and stamp a fresh id on next save.
  diagram_id?: unknown
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
      const diagramId = typeof parsed.diagram_id === 'string' ? parsed.diagram_id : ''
      const validHash = SCENE_HASH_RE.test(sceneHash)
      // Recognize an excalidraw fence by EITHER a content-addressed scene_hash
      // (a drawn + saved diagram, served as a PNG sidecar) OR the excalidraw
      // scene shape itself (`elements` array + `appState` object). The shape
      // check is what catches an empty, never-drawn diagram (`scene_hash: ""`,
      // inserted via the slash menu but never saved): it must still render as
      // the empty-diagram placeholder, NOT dump its raw JSON into the page.
      const isScene =
        Array.isArray(parsed.elements) &&
        typeof parsed.appState === 'object' &&
        parsed.appState !== null
      if (validHash || isScene) {
        node.type = 'excalidraw'
        node.sceneHash = validHash ? sceneHash : ''
        node.altText = altText
        node.diagramId = diagramId
        node.sceneJSON = raw
        delete node.lang
        delete node.value
        return
      }
    }
    // Parse failure, or a ```excalidraw fence that isn't a scene: leave it as a
    // plain code block.
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
