// M18.B.1 — "From mira" sub-section of the Settings → Import tab. Lets a PO
// import a single mira (mira.cagdas.io) page from a URL or a JSON file. One
// import per submission; navigates to the created page on success.
//
// Q8: only owned UI primitives (Input/Button/Select/ToggleGroup) + tokens. No
// new primitive. Picker pattern inlined from ImportSection per task guidance —
// don't yak-shave a shared component for two callers.
//
// Yjs scope (Hard Rule #6): zero Yjs imports in this file.

import { useMemo, useRef, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { Check, FileJson, FileText, Globe, Link as LinkIcon } from 'lucide-react'
import { ApiError } from '../../lib/api'
import { useMe } from '../../lib/queries/auth'
import { useImportMira } from '../../lib/queries/imports'
import { useSpaceMembers } from '../../lib/queries/members'
import { usePages } from '../../lib/queries/pages'
import { useSpaces } from '../../lib/queries/spaces'
import type { PageTreeNode } from '../../lib/types'
import { cn } from '../../lib/utils'
import { Button } from '../ui/button'
import { CommandInlinePicker, type CommandItem } from '../ui/command'
import { Field } from '../ui/field'
import { Input } from '../ui/input'
import { Select } from '../ui/select'
import { ToggleGroup, ToggleGroupItem } from '../ui/toggle'

type SourceMode = 'url' | 'file'

interface FlatPage {
  id: number
  title: string
  breadcrumb: string
}

// Mirrors ImportSection.flattenPages — pre-order walk that flattens the tree
// into a picker-friendly list with breadcrumb strings.
function flattenPages(roots: PageTreeNode[]): FlatPage[] {
  const out: FlatPage[] = []
  function walk(nodes: PageTreeNode[], trail: string[]) {
    for (const n of nodes) {
      const title = n.title || 'Untitled'
      out.push({ id: n.id, title, breadcrumb: trail.join(' / ') })
      walk(n.children, [...trail, title])
    }
  }
  walk(roots, [])
  return out
}

const ROOT_PARENT_VALUE = '__root__'

export function MiraImportSection() {
  const me = useMe()
  const spaces = useSpaces()
  const importMira = useImportMira()
  const navigate = useNavigate()

  const [spaceId, setSpaceId] = useState<number | null>(null)
  const [parentId, setParentId] = useState<number | null>(null)
  const [pickerValue, setPickerValue] = useState<string>(ROOT_PARENT_VALUE)
  const [sourceMode, setSourceMode] = useState<SourceMode>('url')
  const [url, setUrl] = useState('')
  const [urlError, setUrlError] = useState<string | null>(null)
  const [file, setFile] = useState<File | null>(null)
  const [parsedPayload, setParsedPayload] = useState<unknown>(null)
  const [fileError, setFileError] = useState<string | null>(null)
  const [submitError, setSubmitError] = useState<string | null>(null)
  const fileInputRef = useRef<HTMLInputElement | null>(null)

  // Editor-or-owner gate on the selected space — mirrors ImportSection.
  const members = useSpaceMembers(spaceId)
  const myRole = useMemo(() => {
    if (me.data == null || members.data == null) return null
    return members.data.find((m) => m.user_id === me.data!.id)?.role ?? null
  }, [me.data, members.data])
  const canImportToSelected = myRole === 'owner' || myRole === 'editor'

  const tree = usePages({ spaceId, tree: true })
  const flatPages = useMemo<FlatPage[]>(() => {
    const nodes = (tree.data as PageTreeNode[] | undefined) ?? []
    return flattenPages(nodes)
  }, [tree.data])

  function handleSpaceChange(next: number | null) {
    setSpaceId(next)
    setParentId(null)
    setPickerValue(ROOT_PARENT_VALUE)
    setSubmitError(null)
  }

  const pickerItems = useMemo<CommandItem[]>(() => {
    const items: CommandItem[] = []
    const rootChosen = parentId == null
    items.push({
      id: ROOT_PARENT_VALUE,
      title: '(space root)',
      subtitle: 'Imported page sits at the top of the space',
      icon: rootChosen ? (
        <Check width={14} height={14} />
      ) : (
        <Globe width={14} height={14} />
      ),
      keywords: ['root', 'top', 'space', 'none'],
      onSelect: () => {
        setParentId(null)
        setPickerValue(ROOT_PARENT_VALUE)
      },
    })
    for (const p of flatPages) {
      const chosen = parentId === p.id
      items.push({
        id: String(p.id),
        title: p.title,
        breadcrumb: p.breadcrumb || undefined,
        icon: chosen ? (
          <Check width={14} height={14} />
        ) : (
          <FileText width={14} height={14} />
        ),
        onSelect: () => {
          setParentId(p.id)
          setPickerValue(String(p.id))
        },
      })
    }
    return items
  }, [flatPages, parentId])

  function handleUrlChange(next: string) {
    setUrl(next)
    setSubmitError(null)
    if (next === '' || next.startsWith('https://')) {
      setUrlError(null)
    } else {
      setUrlError('URL must start with https://')
    }
  }

  async function handleFileChange(list: FileList | null) {
    setSubmitError(null)
    if (!list || list.length === 0) {
      setFile(null)
      setParsedPayload(null)
      setFileError(null)
      return
    }
    const f = list[0]
    setFile(f)
    try {
      const text = await f.text()
      const parsed: unknown = JSON.parse(text)
      setParsedPayload(parsed)
      setFileError(null)
    } catch (err) {
      setParsedPayload(null)
      setFileError(
        err instanceof Error ? `Could not parse JSON: ${err.message}` : 'Could not parse JSON.',
      )
    }
  }

  function handleModeChange(next: SourceMode | string) {
    if (next !== 'url' && next !== 'file') return
    setSourceMode(next)
    setSubmitError(null)
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (spaceId == null) return
    setSubmitError(null)
    try {
      const input =
        sourceMode === 'url'
          ? { spaceId, parentId, sourceUrl: url.trim() }
          : { spaceId, parentId, payload: parsedPayload }
      const res = await importMira.mutateAsync(input)
      void navigate({
        to: '/spaces/$spaceId/pages/$pageId/{-$slug}',
        params: { spaceId: res.page.space_id, pageId: res.page.id, slug: undefined },
      })
    } catch (err) {
      if (err instanceof ApiError) {
        setSubmitError(err.message)
      } else {
        setSubmitError('Import failed. Try again.')
      }
    }
  }

  const urlReady = sourceMode === 'url' && url.trim() !== '' && urlError == null
  const fileReady = sourceMode === 'file' && file != null && parsedPayload != null && fileError == null
  const submitDisabled =
    importMira.isPending ||
    spaceId == null ||
    !canImportToSelected ||
    !(urlReady || fileReady)

  return (
    <section
      aria-labelledby="settings-import-mira"
      className="flex flex-col gap-[var(--space-5)]"
    >
      <header
        className="flex flex-col gap-[var(--space-1)]"
        id="settings-import-mira"
      >
        <h2 className="m-0 font-[family-name:var(--font-sans)] text-[length:var(--text-lg)] leading-[var(--leading-tight)] text-[var(--text-primary)]">
          From mira
        </h2>
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)] leading-[var(--leading-relaxed)]">
          Import a single mira (<code>mira.cagdas.io</code>) page from a URL or
          a JSON file. Tier-1 blocks render faithfully; Tier-2 visual blocks
          (kanban / chart / timeline) become best-effort markdown placeholders.
        </p>
      </header>

      <form onSubmit={handleSubmit} className="flex flex-col gap-[var(--space-4)]">
        <Field label="Target space" htmlFor="mira-import-space">
          {spaces.isLoading ? (
            <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
              Loading spaces…
            </p>
          ) : spaces.isError || !spaces.data ? (
            <p
              role="alert"
              className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]"
            >
              Couldn't load spaces.
            </p>
          ) : (
            <Select
              id="mira-import-space"
              value={spaceId == null ? '' : String(spaceId)}
              onChange={(e) => {
                const v = e.target.value
                handleSpaceChange(v === '' ? null : Number(v))
              }}
            >
              <option value="">Choose a space…</option>
              {spaces.data.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </Select>
          )}
          {spaceId != null && members.data != null && !canImportToSelected ? (
            <p
              role="alert"
              className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]"
            >
              You need editor or owner role to import into this space.
            </p>
          ) : null}
        </Field>

        <Field label="Target parent" htmlFor="mira-import-parent">
          {spaceId == null ? (
            <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
              Choose a space first.
            </p>
          ) : (
            <div
              className={cn(
                'rounded-[var(--radius-md)]',
                'border border-[var(--border-subtle)] bg-[var(--surface-1)]',
              )}
            >
              <CommandInlinePicker
                items={pickerItems}
                placeholder="Search pages in this space…"
                emptyMessage="No pages."
                label="Mira import parent"
                value={pickerValue}
                onValueChange={setPickerValue}
                autoFocus={false}
              />
            </div>
          )}
        </Field>

        <Field label="Source">
          <ToggleGroup
            type="single"
            value={sourceMode}
            onValueChange={handleModeChange}
            aria-label="Mira import source mode"
          >
            <ToggleGroupItem value="url" aria-label="URL">
              <LinkIcon width={14} height={14} />
              <span>URL</span>
            </ToggleGroupItem>
            <ToggleGroupItem value="file" aria-label="JSON file">
              <FileJson width={14} height={14} />
              <span>JSON file</span>
            </ToggleGroupItem>
          </ToggleGroup>
        </Field>

        {sourceMode === 'url' ? (
          <Field label="Mira URL" htmlFor="mira-import-url">
            <Input
              id="mira-import-url"
              type="url"
              inputMode="url"
              autoComplete="off"
              spellCheck={false}
              placeholder="https://mira.cagdas.io/p/..."
              value={url}
              onChange={(e) => handleUrlChange(e.currentTarget.value)}
            />
            {urlError ? (
              <p
                role="alert"
                className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]"
              >
                {urlError}
              </p>
            ) : null}
          </Field>
        ) : (
          <Field label="Mira JSON file" htmlFor="mira-import-file">
            <input
              ref={fileInputRef}
              id="mira-import-file"
              type="file"
              accept="application/json,.json"
              onChange={(e) => {
                void handleFileChange(e.currentTarget.files)
              }}
              className={cn(
                'block w-full text-[length:var(--text-sm)]',
                'font-[family-name:var(--font-sans)] text-[var(--text-primary)]',
                'file:mr-[var(--space-3)] file:rounded-[var(--radius-sm)]',
                'file:border file:border-[var(--border)] file:bg-[var(--surface-2)]',
                'file:px-[var(--space-3)] file:py-[var(--space-2)]',
                'file:text-[length:var(--text-sm)] file:font-medium',
                'file:text-[var(--text-primary)] file:cursor-pointer',
                'hover:file:bg-[var(--surface-3)]',
              )}
            />
            {file != null && parsedPayload != null ? (
              <p className="m-0 text-[length:var(--text-xs)] text-[var(--text-muted)]">
                {file.name} parsed.
              </p>
            ) : null}
            {fileError ? (
              <p
                role="alert"
                className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]"
              >
                {fileError}
              </p>
            ) : null}
          </Field>
        )}

        {submitError ? (
          <p
            role="alert"
            className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]"
          >
            {submitError}
          </p>
        ) : null}

        {importMira.isPending ? (
          <p
            role="status"
            className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]"
          >
            Importing…
          </p>
        ) : null}

        <div className="flex">
          <Button type="submit" variant="primary" disabled={submitDisabled}>
            {importMira.isPending ? 'Importing…' : 'Import'}
          </Button>
        </div>
      </form>
    </section>
  )
}

