// Template variable parsing/substitution for create-from-template (#12).
//
// A template is an ordinary page marked `props.template: true`. Its body may
// carry `{{placeholder}}` tokens that a fill-in modal replaces at creation time.
// This module is the pure core (no DOM, no React) so it's unit-testable — the
// one piece of the feature that genuinely earns a test.
//
// Tag syntax is `{{ name }}`: two braces, any text that isn't a brace, optional
// surrounding whitespace. `{{` is NOT a tela `:::` directive or a markdown
// construct, so a template body carrying tokens round-trips through the editor
// and save path untouched (verified separately). Field/query blocks in a
// template body are copied verbatim and render live in the new note — that's the
// "field-seeded / query-populated" facet, for free.

const VAR_RE = /\{\{\s*([^{}]+?)\s*\}\}/g

// extractVars returns the distinct placeholder names in the body, in first-seen
// order (so the fill modal lists them the way they appear). Deduped: `{{date}}`
// used twice yields one field.
export function extractVars(body: string): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const m of body.matchAll(VAR_RE)) {
    const name = m[1].trim()
    if (name && !seen.has(name)) {
      seen.add(name)
      out.push(name)
    }
  }
  return out
}

// substituteVars replaces every `{{name}}` with values[name]. A name present in
// the map (even as an empty string) is substituted; a name NOT in the map is
// left intact rather than blanked, so a stray token is never silently eaten.
// Callers pass a value for every extractVars() name, so a real fill leaves no
// `{{` residue.
export function substituteVars(body: string, values: Record<string, string>): string {
  return body.replace(VAR_RE, (whole, rawName: string) => {
    const name = rawName.trim()
    return Object.prototype.hasOwnProperty.call(values, name) ? values[name] : whole
  })
}
