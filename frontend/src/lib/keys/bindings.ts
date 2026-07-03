// The tela keymap, as data. Imported once for its side effects (KeymapHost
// imports it) so the registry is populated before the engine mounts. Add a
// binding here and it works in the dispatcher AND shows up in the `?`
// cheatsheet automatically.
//
// Groups (and their first-seen order) drive the cheatsheet sections:
//   Go to · Actions · Move · Reading · General

import { registerKeys } from './keymap'

// --- Go to (jumps) — authed app only -----------------------------------
const GO = 'Go to'

registerKeys({
  id: 'go.home',
  keys: 'g h',
  label: 'Home',
  group: GO,
  when: 'app',
  run: (c) => c.navigate('/'),
})
registerKeys({
  id: 'go.ask',
  keys: 'g a',
  label: 'Ask your docs',
  group: GO,
  when: 'app',
  run: (c) => c.navigate('/ask'),
})
registerKeys({
  id: 'go.graph',
  keys: 'g r',
  label: 'Graph',
  group: GO,
  when: 'app',
  run: (c) => c.navigate('/graph'),
})
registerKeys({
  id: 'go.shared',
  keys: 'g e',
  label: 'Shared with everyone',
  group: GO,
  when: 'app',
  run: (c) => c.navigate('/shared'),
})
registerKeys({
  id: 'go.inbox',
  keys: 'g i',
  label: 'Quick notes',
  group: GO,
  when: 'app',
  run: (c) => c.navigate('/n'),
})
registerKeys({
  id: 'go.search',
  keys: 'g s',
  label: 'Full search',
  group: GO,
  when: 'app',
  run: (c) => c.navigate('/search'),
})

// --- Actions — authed app only -----------------------------------------
const ACT = 'Actions'

registerKeys({
  id: 'act.palette',
  keys: '/',
  label: 'Find a page',
  group: ACT,
  when: 'app',
  run: (c) => c.openPalette(),
})
registerKeys({
  id: 'act.new',
  keys: 'c',
  label: 'Create page',
  group: ACT,
  when: 'app',
  run: (c) => c.openNewPage(),
})
registerKeys({
  id: 'act.theme',
  keys: 't',
  label: 'Toggle theme',
  group: ACT,
  when: 'app',
  run: (c) => c.toggleTheme(),
})
registerKeys({
  id: 'act.sidebar',
  keys: '\\',
  label: 'Toggle sidebar',
  group: ACT,
  when: 'app',
  run: (c) => c.toggleSidebar(),
})

// --- Move (roving / scroll) — every surface ----------------------------
const MOVE = 'Move'

registerKeys({
  id: 'move.down',
  keys: 'j',
  label: 'Down (next item / scroll)',
  group: MOVE,
  run: (c) => c.down(),
})
registerKeys({
  id: 'move.up',
  keys: 'k',
  label: 'Up (previous item / scroll)',
  group: MOVE,
  run: (c) => c.up(),
})
registerKeys({
  id: 'move.top',
  keys: 'g g',
  label: 'Jump to top',
  group: MOVE,
  run: (c) => c.top(),
})
registerKeys({
  id: 'move.bottom',
  keys: 'G',
  label: 'Jump to bottom',
  group: MOVE,
  run: (c) => c.bottom(),
})
registerKeys({
  id: 'move.activate',
  keys: ['Enter', 'o'],
  label: 'Open focused item',
  group: MOVE,
  run: (c) => c.activate(),
})

// --- Reading — every surface (no-ops outside a reader) ------------------
const READ = 'Reading'

registerKeys({
  id: 'read.prev-section',
  keys: '[',
  label: 'Previous heading',
  group: READ,
  run: (c) => c.prevSection(),
})
registerKeys({
  id: 'read.next-section',
  keys: ']',
  label: 'Next heading',
  group: READ,
  run: (c) => c.nextSection(),
})

// --- General — every surface -------------------------------------------
registerKeys({
  id: 'gen.help',
  keys: '?',
  label: 'Keyboard shortcuts',
  group: 'General',
  run: (c) => c.openCheatsheet(),
})
