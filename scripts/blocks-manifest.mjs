#!/usr/bin/env node
// blocks-manifest — keep the agent-facing copy of the block palette in sync with
// the frontend source of truth, and guard against a renderer plugin shipping
// without a manifest entry (invisible to agents + missing from the slash menu).
//
//   node scripts/blocks-manifest.mjs --write   # regenerate the backend copy
//   node scripts/blocks-manifest.mjs --check    # verify in sync + full coverage (CI)
//
// SOURCE  frontend/src/components/app/blocks-manifest.json
// GEN     backend/internal/api/blocks_gen.json   (go:embed can't reach into frontend/)
//
// Coverage: every frontend/src/components/app/milkdown-*.ts(x) is either an
// authorable block (mapped to >=1 manifest id in PLUGIN_BLOCKS) or declared
// non-authorable infra (INFRA). A new plugin in neither set fails --check, so
// adding a block forces a conscious "manifest entry or infra?" decision.

import { readFileSync, writeFileSync, readdirSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..')
const SRC = join(ROOT, 'frontend/src/components/app/blocks-manifest.json')
const GEN = join(ROOT, 'backend/internal/api/blocks_gen.json')
const PLUGIN_DIR = join(ROOT, 'frontend/src/components/app')

// milkdown-* plugins that are NOT authorable blocks (no manifest entry expected).
const INFRA = new Set([
  'milkdown-block-handle', // drag/insert handle in the gutter
  'milkdown-bubble-toolbar', // inline selection toolbar
  'milkdown-directives', // shared remark directive container plumbing
  'milkdown-editor', // the editor host component
  'milkdown-floating', // slash/bubble positioning helper
  'milkdown-image-upload', // upload transport for the `image` block (markdown is plain)
  'milkdown-mira-paste', // paste interop
  'milkdown-mira-paste-popover', // paste interop UI
  'milkdown-modifier-click', // cmd/ctrl-click navigation
  'milkdown-slash', // the slash menu itself
  'milkdown-templates', // composed snippets, not a block type
  'milkdown-url-unfurl', // link unfurl decoration
  'milkdown-wikilink', // wikilink autocomplete/resolve plumbing
  'milkdown-wikilink-decoration', // decoration layer for the `wikilink` block
])

// milkdown-* plugin basename -> manifest block id(s) it backs.
const PLUGIN_BLOCKS = {
  'milkdown-calendar': ['calendar'],
  'milkdown-callouts': ['callout'],
  'milkdown-chart': ['chart'],
  'milkdown-codeblock': ['code'],
  'milkdown-collapsibles': ['collapsible'],
  'milkdown-embed': ['embed'],
  'milkdown-excalidraw': ['excalidraw'],
  'milkdown-highlight': ['highlight'],
  'milkdown-kanban': ['kanban'],
  'milkdown-math': ['equation', 'inline-math'],
  'milkdown-mermaid': ['mermaid'],
  'milkdown-pullquote': ['pull-quote'],
  'milkdown-stat-grid': ['stat-grid'],
  'milkdown-table': ['table'],
  'milkdown-tabs': ['tabs'],
  'milkdown-task-list': ['task-list'],
  'milkdown-timeline': ['timeline'],
  'milkdown-wikilink-bracket': ['wikilink'],
}

function loadSource() {
  const raw = JSON.parse(readFileSync(SRC, 'utf8'))
  if (!Array.isArray(raw.blocks)) fail(`${SRC} has no "blocks" array`)
  return raw.blocks
}

// Deterministic serialization for a stable git diff (2-space indent, trailing nl).
function render(blocks) {
  return JSON.stringify({ blocks }, null, 2) + '\n'
}

function fail(msg) {
  console.error(`blocks-manifest: ${msg}`)
  process.exit(1)
}

function checkCoverage(blocks) {
  const ids = new Set(blocks.map((b) => b.id))
  const problems = []

  // Every mapped id must exist in the manifest.
  for (const [file, mapped] of Object.entries(PLUGIN_BLOCKS)) {
    for (const id of mapped) {
      if (!ids.has(id)) problems.push(`${file} maps to unknown block id "${id}"`)
    }
  }

  // Every milkdown-* plugin must be classified (block or infra).
  const plugins = readdirSync(PLUGIN_DIR)
    .filter((f) => /^milkdown-.*\.tsx?$/.test(f) && !f.endsWith('.stories.tsx'))
    .map((f) => f.replace(/\.tsx?$/, ''))
  for (const p of plugins) {
    if (!INFRA.has(p) && !(p in PLUGIN_BLOCKS)) {
      problems.push(
        `plugin "${p}" is unclassified — add it to PLUGIN_BLOCKS (with a blocks-manifest.json entry) or INFRA in scripts/blocks-manifest.mjs`,
      )
    }
  }

  // Required fields per block.
  for (const b of blocks) {
    for (const k of ['id', 'label', 'hint', 'category', 'syntax']) {
      if (!b[k]) problems.push(`block "${b.id ?? '?'}" missing field "${k}"`)
    }
    if (b.agent && !b.when) problems.push(`agent block "${b.id}" missing "when"`)
  }

  if (problems.length) fail('coverage failed:\n  - ' + problems.join('\n  - '))
}

const mode = process.argv[2]
const blocks = loadSource()
checkCoverage(blocks)
const out = render(blocks)

if (mode === '--write') {
  writeFileSync(GEN, out)
  console.log(`blocks-manifest: wrote ${GEN} (${blocks.length} blocks)`)
} else if (mode === '--check') {
  let current = ''
  try {
    current = readFileSync(GEN, 'utf8')
  } catch {
    fail(`${GEN} missing — run \`make blocks-gen\``)
  }
  if (current !== out) {
    fail(`${GEN} is stale — run \`make blocks-gen\` and commit the result`)
  }
  console.log(`blocks-manifest: in sync (${blocks.length} blocks)`)
} else {
  fail('usage: blocks-manifest.mjs --write | --check')
}
