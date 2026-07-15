// Milkdown-free field render core. SINGLE SOURCE shared by the editor's field
// decoration (milkdown-field.ts) and the read-only view renderer's interactive
// widget (FieldWidget.tsx via MarkdownView). A ` ```field ` fenced block is a
// *pointer*: it names a widget type + a target prop key; the live value is read
// from — and written back to — the page's props[prop] (see
// docs/page-properties.md). The block body never stores the value.
//
// The spec is a tiny YAML-ish mapping, parsed synchronously here (no js-yaml
// dependency — unlike chart, whose spec can be deeply nested) so the read-view
// widget stays a plain React render with no async parse dance.
//
// Supported `type`: text, toggle, select, button. `select` needs `options`;
// `button` needs a fixed `value` to set on click.

export type FieldType = 'text' | 'toggle' | 'select' | 'button'

export interface FieldSpec {
  /** The props key this widget is bound to. */
  prop: string
  type: FieldType
  /** Human label; falls back to the prop name in the UI. */
  label?: string
  /** Choices for a `select` (one is written verbatim to props[prop]). */
  options: string[]
  /** Fixed value a `button` writes to props[prop] on click. */
  value?: string
}

export interface FieldSpecError {
  error: string
}

const FIELD_TYPES: readonly FieldType[] = ['text', 'toggle', 'select', 'button']

export function isFieldError(
  spec: FieldSpec | FieldSpecError,
): spec is FieldSpecError {
  return 'error' in spec
}

// Strip one layer of matching surrounding quotes.
function unquote(s: string): string {
  const t = s.trim()
  if (t.length >= 2) {
    const q = t[0]
    if ((q === '"' || q === "'") && t[t.length - 1] === q) {
      return t.slice(1, -1)
    }
  }
  return t
}

// Parse an inline flow list — `[pass, fail, pending]` — into trimmed, unquoted
// items. A bare comma-separated scalar (no brackets) is accepted too.
function parseInlineList(s: string): string[] {
  let t = s.trim()
  if (t.startsWith('[') && t.endsWith(']')) t = t.slice(1, -1)
  if (t.trim() === '') return []
  return t
    .split(',')
    .map((x) => unquote(x))
    .filter((x) => x.length > 0)
}

// parseFieldSpec turns the fenced-block source into a typed spec, or an error
// with a human message (rendered in place, never thrown — a malformed field
// shouldn't blank the page).
export function parseFieldSpec(code: string): FieldSpec | FieldSpecError {
  const raw: Record<string, string> = {}
  for (const line of code.split('\n')) {
    const t = line.trim()
    if (t === '' || t.startsWith('#')) continue
    const idx = t.indexOf(':')
    if (idx < 0) continue
    const key = t.slice(0, idx).trim().toLowerCase()
    if (key === '') continue
    raw[key] = t.slice(idx + 1).trim()
  }

  const prop = unquote(raw.prop ?? '')
  if (prop === '') return { error: 'field needs a `prop:` key' }

  const type = (raw.type ?? 'text').toLowerCase() as FieldType
  if (!FIELD_TYPES.includes(type)) {
    return { error: `unknown field type "${type}"` }
  }

  const options = parseInlineList(raw.options ?? '')
  if (type === 'select' && options.length === 0) {
    return { error: 'select field needs `options: [...]`' }
  }

  const value = raw.value != null ? unquote(raw.value) : undefined
  if (type === 'button' && (value == null || value === '')) {
    return { error: 'button field needs a `value:` to set' }
  }

  const label = raw.label != null ? unquote(raw.label) : undefined
  return { prop, type, label, options, value }
}

// A short, non-interactive description of the control for the editor preview
// (the interactive surface lives in the read view — same stance as the poll).
function previewControl(spec: FieldSpec): string {
  switch (spec.type) {
    case 'select':
      return spec.options.join('  ·  ')
    case 'toggle':
      return 'off / on'
    case 'button':
      return `→ ${spec.value ?? ''}`
    case 'text':
    default:
      return 'text…'
  }
}

// buildFieldPreview renders the editor-side decoration: a static, non-editable
// chip showing the bound prop + a hint of the control. Interaction is a
// read-view concern (FieldWidget). Token-styled via .tela-field-* classes.
export function buildFieldPreview(code: string): HTMLElement {
  const dom = document.createElement('div')
  dom.className = 'tela-field-preview'
  dom.setAttribute('contenteditable', 'false')

  const spec = parseFieldSpec(code)
  if (isFieldError(spec)) {
    dom.classList.add('tela-field-error')
    dom.textContent = spec.error
    return dom
  }

  const label = document.createElement('span')
  label.className = 'tela-field-preview-label'
  label.textContent = spec.label || spec.prop

  const control = document.createElement('span')
  control.className = 'tela-field-preview-control'
  control.textContent = previewControl(spec)

  const key = document.createElement('span')
  key.className = 'tela-field-preview-key'
  key.textContent = spec.prop

  dom.append(label, control, key)
  return dom
}
