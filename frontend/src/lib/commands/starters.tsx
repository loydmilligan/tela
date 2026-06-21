// Starter set of application commands. Imported once for its side effects;
// future tasks add commands by registering them in their own module and
// importing that module at app boot.

import { Folder, FolderPlus, HelpCircle, Palette } from 'lucide-react'
import { registerCommand } from '../commands'
import { emitOpenNewSpace } from '../newSpaceEvent'
import { THEMES, type ThemeName } from '../theme'

// Cycle through THEMES in declaration order: light → dark → warm → light.
function nextTheme(current: ThemeName): ThemeName {
  const i = THEMES.indexOf(current)
  if (i === -1) return THEMES[0]
  return THEMES[(i + 1) % THEMES.length]
}

registerCommand({
  id: 'tela.toggle-theme',
  title: 'Toggle theme',
  subtitle: 'Cycle light → dark → warm',
  icon: <Palette width={14} height={14} />,
  keywords: ['theme', 'appearance', 'dark', 'light', 'warm'],
  run: (ctx) => {
    ctx.setTheme(nextTheme(ctx.currentTheme))
  },
})

registerCommand({
  id: 'tela.new-space',
  title: 'New space',
  subtitle: 'Create a space',
  icon: <FolderPlus width={14} height={14} />,
  keywords: ['space', 'new', 'create', 'add'],
  run: () => emitOpenNewSpace(),
})

registerCommand({
  id: 'tela.go-to-space',
  title: 'Go to space…',
  subtitle: 'Jump to another space',
  icon: <Folder width={14} height={14} />,
  keywords: ['space', 'navigate', 'jump', 'switch'],
  keepPaletteOpen: true,
  run: (ctx) => {
    ctx.openSubPicker({
      label: 'Go to space',
      placeholder: 'Search spaces…',
      emptyMessage: ctx.spaces.length === 0 ? 'No spaces yet.' : 'No matches.',
      items: ctx.spaces.map((space) => ({
        id: `space:${space.id}`,
        title: space.name || 'Untitled space',
        subtitle: space.slug || undefined,
        icon: <Folder width={14} height={14} />,
        keywords: [space.name, space.slug].filter(Boolean) as string[],
        onSelect: () => ctx.navigateToSpace(space.id),
      })),
    })
  },
})

registerCommand({
  id: 'tela.show-shortcuts',
  title: 'Show keyboard shortcuts',
  subtitle: 'See every shortcut',
  icon: <HelpCircle width={14} height={14} />,
  keywords: ['help', 'shortcuts', 'keys', 'keyboard'],
  keepPaletteOpen: true,
  run: (ctx) => {
    ctx.openHelpMode()
  },
})
