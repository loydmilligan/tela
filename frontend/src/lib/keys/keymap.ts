// App-wide keyboard layer — a vim-style keymap that drives navigation and
// actions from bare keys + leader sequences (e.g. `g h`, `j`/`k`, `?`), NOT
// editor text input. Bindings are declared as data here so a single source
// feeds both the dispatcher (useKeymap) and the `?` cheatsheet.
//
// Two axes the engine resolves at keypress time:
//  - SURFACE — which app context we're in: 'app' (authenticated shell, incl.
//    the ?view=read reader overlay), or 'public' (logged-out reader/share).
//    Gates bindings that need the authed app (jumps, new page, palette).
//  - REGION — which DOM list/scroller `j`/`k` should drive right now (the
//    sidebar tree, a results list, or the reading-mode scroller). Resolved
//    from `[data-keynav-region]` in the live DOM, not React state, so any
//    surface opts in by marking its container + rows.

export type KeySurface = 'app' | 'public'

// Everything a binding's `run` can reach. Action verbs come from the host
// (KeymapHost wires them to palette events / router / theme); motion verbs are
// implemented by the engine against the active region. `surface` is the
// resolved surface at press time.
export interface KeyContext {
  surface: KeySurface
  // Actions (host-provided) — no-op on surfaces where they don't apply.
  navigate: (to: string) => void
  openPalette: () => void
  openNewPage: () => void
  toggleTheme: () => void
  toggleSidebar: () => void
  openCheatsheet: () => void
  // Motion (engine-provided) — operate on the active keynav region. `down`/`up`
  // scroll a reader region or move the cursor in a list region; `activate`
  // opens the focused list row; `prevSection`/`nextSection` jump headings.
  down: () => void
  up: () => void
  top: () => void
  bottom: () => void
  activate: () => void
  prevSection: () => void
  nextSection: () => void
}

export interface KeyBinding {
  id: string
  // One combo or a leader sequence. Single keys are bare (`j`, `?`, `G`);
  // sequences are space-separated (`g h`). Keys match `KeyboardEvent.key`
  // verbatim, so case is significant (`g` ≠ `G`). An array registers aliases.
  keys: string | string[]
  label: string
  // Cheatsheet section. Order of first appearance sets section order.
  group: string
  // Surface gate. Omitted → available everywhere (motion + help).
  when?: KeySurface
  run: (ctx: KeyContext) => void
}

// Insertion-ordered registry, keyed by id so HMR / double-import overwrite
// cleanly rather than duplicating (mirrors lib/commands.ts).
const registry = new Map<string, KeyBinding>()

export function registerKeys(binding: KeyBinding): void {
  registry.set(binding.id, binding)
}

export function getKeyBindings(): KeyBinding[] {
  return Array.from(registry.values())
}

export function keysOf(binding: KeyBinding): string[] {
  return Array.isArray(binding.keys) ? binding.keys : [binding.keys]
}
