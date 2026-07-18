import { useMemo, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { FileText } from 'lucide-react'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '../ui/dialog'
import { Button } from '../ui/button'
import { Input } from '../ui/input'
import { useTemplates, type TemplatePage } from '../../lib/queries/templates'
import { usePage, useCreatePage } from '../../lib/queries/pages'
import { extractVars, substituteVars } from '../../lib/templates'
import { useUiPrefs } from '../../lib/ui-prefs'

// Props on a template that describe the TEMPLATE, not the note it seeds — so
// they're not inherited by the created note (its own summary is regenerated).
const NON_INHERITED_PROPS = new Set(['template', 'summary', 'summary_lock'])

export interface TemplatePickerDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Space to create the new note in (the space the menu was invoked from). */
  spaceId: number
  /** Parent page to nest under, or null to create at the space's top level. */
  parentId: number | null
}

// Create-from-template picker (#12). Lists every readable template (any page with
// props.template:true), grouped by space; on pick, fetches its body, prompts for
// any {{placeholders}}, substitutes, and creates a note in the CURRENT space —
// the template is a content source, the note lands where you invoked the menu.
export function TemplatePickerDialog({
  open,
  onOpenChange,
  spaceId,
  parentId,
}: TemplatePickerDialogProps) {
  const navigate = useNavigate()
  const uiPrefs = useUiPrefs()
  const templates = useTemplates(open)
  const createPage = useCreatePage()

  const [picked, setPicked] = useState<TemplatePage | null>(null)
  const [values, setValues] = useState<Record<string, string>>({})

  // Fetch the picked template's body (the list query returns props/title only).
  const pickedPage = usePage(picked?.id)
  const body = pickedPage.data?.body ?? ''
  const vars = useMemo(() => (picked ? extractVars(body) : []), [picked, body])

  function reset() {
    setPicked(null)
    setValues({})
  }

  function close(next: boolean) {
    if (!next) reset()
    onOpenChange(next)
  }

  // Group templates by space so "Daily Journal (Personal)" reads distinctly from
  // "Roadmap (Templates)" — the reason "everywhere" stays navigable.
  const grouped = useMemo(() => {
    const m = new Map<string, TemplatePage[]>()
    for (const t of templates.data ?? []) {
      const arr = m.get(t.space_name) ?? []
      arr.push(t)
      m.set(t.space_name, arr)
    }
    return [...m.entries()]
  }, [templates.data])

  async function create() {
    if (!picked || pickedPage.isLoading) return
    const filledBody = substituteVars(body, values)
    const props: Record<string, unknown> = {}
    for (const [k, v] of Object.entries(picked.props)) {
      if (!NON_INHERITED_PROPS.has(k)) props[k] = v
    }
    try {
      const created = await createPage.mutateAsync({
        space_id: spaceId,
        parent_id: parentId,
        title: picked.title,
        body: filledBody,
        props,
      })
      close(false)
      void navigate({
        to: '/spaces/$spaceId/pages/$pageId/{-$slug}',
        params: { spaceId, pageId: created.id, slug: undefined },
        // #27 — respect the "open new pages in edit mode" preference.
        search: uiPrefs.newChildEditMode ? { edit: true } : undefined,
      })
    } catch {
      // Optimistic tree rollback + refetch surface the failure; keep the dialog
      // open so the user can retry.
    }
  }

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="max-w-[32rem]">
        <DialogHeader>
          <DialogTitle>New from template</DialogTitle>
          <DialogDescription>
            {picked
              ? vars.length > 0
                ? `Fill in ${picked.title}`
                : `Create a note from ${picked.title}`
              : 'Pick a template to create a new note from.'}
          </DialogDescription>
        </DialogHeader>

        {/* Phase 1 — pick a template. */}
        {!picked ? (
          <div className="max-h-[22rem] overflow-y-auto flex flex-col gap-[var(--space-3)]">
            {templates.isLoading ? (
              <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
                Loading templates…
              </p>
            ) : (templates.data?.length ?? 0) === 0 ? (
              <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)] leading-[var(--leading-relaxed)]">
                No templates yet. Mark any page as a template by setting its{' '}
                <code className="font-[family-name:var(--font-mono)]">template</code>{' '}
                property to <code className="font-[family-name:var(--font-mono)]">true</code>{' '}
                (the properties button in the page header).
              </p>
            ) : (
              grouped.map(([spaceName, items]) => (
                <div key={spaceName} className="flex flex-col gap-[1px]">
                  <span className="px-[var(--space-1)] text-[length:var(--text-xs)] uppercase tracking-wider text-[var(--text-muted)]">
                    {spaceName}
                  </span>
                  {items.map((t) => (
                    <button
                      key={t.id}
                      type="button"
                      onClick={() => setPicked(t)}
                      className="flex items-center gap-[var(--space-2)] text-left rounded-[var(--radius-sm)] px-[var(--space-2)] py-[var(--space-2)] bg-transparent border-0 cursor-pointer text-[var(--text-primary)] hover:bg-[var(--sidebar-item-hover)] outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)]"
                    >
                      <FileText
                        width={15}
                        height={15}
                        className="shrink-0 text-[var(--text-muted)]"
                      />
                      <span className="min-w-0 truncate text-[length:var(--text-sm)]">
                        {t.title || 'Untitled'}
                      </span>
                    </button>
                  ))}
                </div>
              ))
            )}
          </div>
        ) : (
          // Phase 2 — fill placeholders (if any), then create.
          <div className="flex flex-col gap-[var(--space-3)]">
            {pickedPage.isLoading ? (
              <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
                Loading template…
              </p>
            ) : vars.length === 0 ? (
              <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
                This template has no fields to fill.
              </p>
            ) : (
              vars.map((name) => (
                <label key={name} className="flex flex-col gap-[var(--space-1)]">
                  <span className="text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-mono)]">
                    {name}
                  </span>
                  <Input
                    value={values[name] ?? ''}
                    aria-label={`Value for ${name}`}
                    onChange={(e) =>
                      setValues((prev) => ({ ...prev, [name]: e.target.value }))
                    }
                  />
                </label>
              ))
            )}
          </div>
        )}

        <DialogFooter>
          {picked ? (
            <Button
              type="button"
              variant="ghost"
              onClick={reset}
              disabled={createPage.isPending}
            >
              Back
            </Button>
          ) : null}
          {picked ? (
            <Button
              type="button"
              variant="primary"
              onClick={() => void create()}
              disabled={pickedPage.isLoading || createPage.isPending}
            >
              Create
            </Button>
          ) : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
