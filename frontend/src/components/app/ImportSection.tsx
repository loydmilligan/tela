import { useMemo, useRef, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { Check, FileText, FolderUp, Globe } from 'lucide-react'
import { ApiError } from '../../lib/api'
import {
  useImportPages,
  type ImportResult as ImportResultPayload,
} from '../../lib/queries/imports'
import { usePages } from '../../lib/queries/pages'
import { useSpaceRole, useSpaces } from '../../lib/queries/spaces'
import type { PageTreeNode } from '../../lib/types'
import { Button } from '../ui/button'
import { Checkbox } from '../ui/checkbox'
import {
  CommandInlinePicker,
  type CommandItem,
} from '../ui/command'
import { Field } from '../ui/field'
import { Select } from '../ui/select'
import { ToggleGroup, ToggleGroupItem } from '../ui/toggle'
import { cn } from '../../lib/utils'
import { ImportResult } from './ImportResult'

type SourceMode = 'folder' | 'file'

interface FlatPage {
  id: number
  title: string
  breadcrumb: string
}

// Pre-order walk that flattens the tree into a picker-friendly list with
// breadcrumb strings. Mirrors move-page-dialog's flattenValidTargets but
// without the cycle filter — every page is a valid import target.
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

export function ImportSection() {
  const spaces = useSpaces()
  const importPages = useImportPages()
  const navigate = useNavigate()

  const [spaceId, setSpaceId] = useState<number | null>(null)
  const [parentId, setParentId] = useState<number | null>(null)
  const [pickerValue, setPickerValue] = useState<string>(ROOT_PARENT_VALUE)
  const [sourceMode, setSourceMode] = useState<SourceMode>('folder')
  const [files, setFiles] = useState<File[]>([])
  const [dryRun, setDryRun] = useState(true)
  const [error, setError] = useState<string | null>(null)
  // Successful run we're showing in the result card. Tracks dryRun separately
  // so a dry-run preview → confirmed real run can clear the preview state.
  const [result, setResult] = useState<ImportResultPayload | null>(null)
  const [resultIsDryRun, setResultIsDryRun] = useState(false)
  const folderInputRef = useRef<HTMLInputElement | null>(null)
  const fileInputRef = useRef<HTMLInputElement | null>(null)

  // Effective role on the selected space drives the editor/owner gate —
  // my_role on the space detail, fetched per-space on demand. Mirrors
  // PageView's role-resolution pattern.
  const { resolved: roleResolved, isViewer } = useSpaceRole(spaceId)
  const canImportToSelected = roleResolved && !isViewer

  // Page tree for the parent picker. Disabled until a space is chosen.
  const tree = usePages({ spaceId, tree: true })
  const flatPages = useMemo<FlatPage[]>(() => {
    const nodes = (tree.data as PageTreeNode[] | undefined) ?? []
    return flattenPages(nodes)
  }, [tree.data])

  // Reset parent + result when the space changes — pointing the parent picker
  // at a page that no longer exists in the new space would silently fail
  // server-side. Done in the Select onChange handler below rather than an
  // effect (avoids set-state-in-effect lint regression).
  function handleSpaceChange(next: number | null) {
    setSpaceId(next)
    setParentId(null)
    setPickerValue(ROOT_PARENT_VALUE)
    setResult(null)
    setError(null)
  }

  const pickerItems = useMemo<CommandItem[]>(() => {
    const items: CommandItem[] = []
    const rootChosen = parentId == null
    items.push({
      id: ROOT_PARENT_VALUE,
      title: '(space root)',
      subtitle: 'Imported pages sit at the top of the space',
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

  function handleFiles(list: FileList | null) {
    if (!list || list.length === 0) {
      setFiles([])
      return
    }
    setFiles(Array.from(list))
  }

  function clearFileInputs() {
    if (folderInputRef.current) folderInputRef.current.value = ''
    if (fileInputRef.current) fileInputRef.current.value = ''
    setFiles([])
  }

  function handleModeChange(next: SourceMode | string) {
    if (next !== 'folder' && next !== 'file') return
    setSourceMode(next)
    clearFileInputs()
  }

  async function runImport(asDryRun: boolean) {
    if (spaceId == null || files.length === 0) return
    setError(null)
    try {
      const r = await importPages.mutateAsync({
        spaceId,
        parentId,
        files,
        dryRun: asDryRun,
      })
      setResult(r)
      setResultIsDryRun(asDryRun)
    } catch (err) {
      if (err instanceof ApiError) {
        setError(err.message)
      } else {
        setError('Import failed. Try again.')
      }
    }
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    await runImport(dryRun)
  }

  function handleConfirm() {
    void runImport(false)
  }

  function handleCancel() {
    setResult(null)
    setError(null)
  }

  function handleOpenFirstPage() {
    if (spaceId == null || !result || result.pages.length === 0) return
    void navigate({
      to: '/spaces/$spaceId/pages/$pageId/{-$slug}',
      params: { spaceId, pageId: result.pages[0].id, slug: undefined },
    })
  }

  const submitDisabled =
    importPages.isPending ||
    spaceId == null ||
    files.length === 0 ||
    !canImportToSelected

  const showInFlight = importPages.isPending
  const showResult = result != null && !showInFlight

  return (
    <section
      aria-labelledby="settings-import"
      className="flex flex-col gap-[var(--space-5)]"
    >
      <header className="flex flex-col gap-[var(--space-1)]" id="settings-import">
        <h2 className="m-0 font-[family-name:var(--font-sans)] text-[length:var(--text-lg)] leading-[var(--leading-tight)] text-[var(--text-primary)]">
          Markdown import
        </h2>
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)] leading-[var(--leading-relaxed)]">
          Bulk-create pages from a folder of <code>.md</code> files. A{' '}
          <code>README.md</code> inside a folder becomes that folder's index
          page; sibling-title conflicts are auto-renamed with a numeric suffix.
        </p>
      </header>

      <form
        onSubmit={handleSubmit}
        className="flex flex-col gap-[var(--space-4)]"
      >
        <Field label="Target space" htmlFor="import-space">
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
              id="import-space"
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
          {spaceId != null && roleResolved && !canImportToSelected ? (
            <p
              role="alert"
              className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]"
            >
              You need editor or owner role to import into this space.
            </p>
          ) : null}
        </Field>

        <Field label="Target parent" htmlFor="import-parent">
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
                label="Import parent"
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
            aria-label="Import source mode"
          >
            <ToggleGroupItem value="folder" aria-label="Folder">
              <FolderUp width={14} height={14} />
              <span>Folder</span>
            </ToggleGroupItem>
            <ToggleGroupItem value="file" aria-label="Single file">
              <FileText width={14} height={14} />
              <span>Single file</span>
            </ToggleGroupItem>
          </ToggleGroup>
        </Field>

        <Field
          label={sourceMode === 'folder' ? 'Folder' : 'File'}
          htmlFor="import-files"
        >
          {sourceMode === 'folder' ? (
            <FolderFileInput
              ref={folderInputRef}
              onFiles={handleFiles}
              id="import-files"
            />
          ) : (
            <input
              ref={fileInputRef}
              id="import-files"
              type="file"
              accept=".md,.markdown"
              onChange={(e) => handleFiles(e.currentTarget.files)}
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
          )}
          {files.length > 0 ? (
            <p className="m-0 text-[length:var(--text-xs)] text-[var(--text-muted)]">
              {files.length} file{files.length === 1 ? '' : 's'} selected.
            </p>
          ) : null}
        </Field>

        <div className="flex items-center gap-[var(--space-2)]">
          <Checkbox
            id="import-dry-run"
            checked={dryRun}
            onCheckedChange={(v) => setDryRun(v === true)}
          />
          <label
            htmlFor="import-dry-run"
            className="text-[length:var(--text-sm)] text-[var(--text-primary)] cursor-pointer font-[family-name:var(--font-sans)]"
          >
            Dry run — preview before importing
          </label>
        </div>

        {error ? (
          <p
            role="alert"
            className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]"
          >
            {error}
          </p>
        ) : null}

        <div className="flex">
          <Button type="submit" variant="primary" disabled={submitDisabled}>
            {importPages.isPending
              ? 'Importing…'
              : dryRun
                ? 'Preview import'
                : 'Import'}
          </Button>
        </div>
      </form>

      {showInFlight ? (
        <div
          role="status"
          className={cn(
            'rounded-[var(--radius-md)]',
            'border border-[var(--border-subtle)] bg-[var(--surface-2)]',
            'px-[var(--space-4)] py-[var(--space-3)]',
            'text-[length:var(--text-sm)] text-[var(--text-muted)]',
            'font-[family-name:var(--font-sans)]',
            'animate-pulse',
          )}
        >
          Importing…
        </div>
      ) : null}

      {showResult && result ? (
        <ImportResult
          result={result}
          dryRun={resultIsDryRun}
          onConfirm={resultIsDryRun ? handleConfirm : undefined}
          onCancel={resultIsDryRun ? handleCancel : undefined}
          confirmPending={importPages.isPending}
          onOpenFirstPage={!resultIsDryRun ? handleOpenFirstPage : undefined}
        />
      ) : null}
    </section>
  )
}

// Folder input with the non-standard `webkitdirectory` attribute that the
// React types don't know about. The cast is local + scoped so we don't
// pollute the global type augmentation surface.
type FolderInputProps = React.InputHTMLAttributes<HTMLInputElement> & {
  webkitdirectory?: string
  directory?: string
}
const FolderFileInput = (() => {
  return function FolderFileInputComponent({
    onFiles,
    id,
    ref,
  }: {
    onFiles: (list: FileList | null) => void
    id?: string
    ref?: React.Ref<HTMLInputElement>
  }) {
    const props: FolderInputProps = {
      id,
      type: 'file',
      accept: '.md,.markdown',
      multiple: true,
      webkitdirectory: '',
      directory: '',
      onChange: (e) => onFiles(e.currentTarget.files),
    }
    return (
      <input
        {...props}
        ref={ref}
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
    )
  }
})()
