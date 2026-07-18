import { useState } from 'react'
import { Braces, Check, Plus, Trash2 } from 'lucide-react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Button } from '../ui/button'
import { Input } from '../ui/input'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from '../ui/dropdown-menu'
import { api } from '../../lib/api'
import { pageKeys } from '../../lib/queries/pages'
import { coerceScalar, isEditableScalar } from '../../lib/props-coerce'

// formatScalar renders a single non-container value for display.
function formatScalar(v: unknown): string {
  if (typeof v === 'boolean') return v ? 'true' : 'false'
  return String(v)
}

// formatPropValue flattens any frontmatter value to a readable string. Scalars
// pass through; arrays join with commas; objects fall back to compact JSON.
function formatPropValue(v: unknown): string {
  if (v === null || v === undefined) return '—'
  if (Array.isArray(v)) return v.map(formatScalar).join(', ')
  if (typeof v === 'object') return JSON.stringify(v)
  return formatScalar(v)
}

// The text shown in an editable value input for a scalar prop.
function valueToInput(v: unknown): string {
  if (v === null) return 'null'
  return formatScalar(v)
}

export interface PagePropertiesProps {
  /** The page's free-form props bag (frontmatter). */
  props?: Record<string, unknown> | null
  /** Page id — required for editing; omit for a pure read-only view. */
  pageId?: number
  /** When true (and pageId set), the panel edits/adds/removes props. */
  canEdit?: boolean
}

/**
 * PageProperties — the page's frontmatter properties, surfaced as a quiet header
 * icon that opens the key/value list in a popover. Read-only for viewers (and
 * anywhere pageId is omitted); for editors it becomes the general "edit page
 * properties" UI (#15) — inline value edits, add a key, remove a key — the
 * missing parity leg that lets humans set props in the UI, not just agents.
 *
 * Renders nothing for a page with no properties UNLESS the caller can edit (an
 * editor needs the affordance to add the first property).
 */
export function PageProperties({ props, pageId, canEdit }: PagePropertiesProps) {
  const qc = useQueryClient()
  const bag = props ?? {}
  const entries = Object.entries(bag)
  const editable = !!canEdit && pageId != null

  const [newKey, setNewKey] = useState('')
  const [newValue, setNewValue] = useState('')

  // Set/merge one key (server-side shallow-merge — safe against a concurrent
  // prop write), then refetch so the popover re-reads the stored value.
  const setProp = useMutation({
    mutationFn: ({ key, value }: { key: string; value: unknown }) =>
      api(`/api/pages/${pageId}/props`, {
        method: 'PATCH',
        body: JSON.stringify({ key, value }),
      }),
    onSuccess: () => {
      if (pageId != null) void qc.invalidateQueries({ queryKey: pageKeys.detail(pageId) })
    },
  })

  // Delete a key: props merge cannot remove, so replace the whole bag minus the
  // key via PATCH /pages/{id}. (Whole-bag replace, like update_page — acceptable
  // for a human editing their own page.)
  const removeProp = useMutation({
    mutationFn: (key: string) => {
      const next: Record<string, unknown> = { ...bag }
      delete next[key]
      return api(`/api/pages/${pageId}`, {
        method: 'PATCH',
        body: JSON.stringify({ props: next }),
      })
    },
    onSuccess: () => {
      if (pageId != null) void qc.invalidateQueries({ queryKey: pageKeys.detail(pageId) })
    },
  })

  function commitValue(key: string, raw: string) {
    const value = coerceScalar(raw)
    if (value === bag[key]) return // unchanged — skip the write
    setProp.mutate({ key, value })
  }

  function addProp() {
    const key = newKey.trim()
    if (key === '') return
    setProp.mutate({ key, value: coerceScalar(newValue) })
    setNewKey('')
    setNewValue('')
  }

  // Nothing to show and nothing to add → no chrome.
  if (entries.length === 0 && !editable) return null

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          aria-label="Page properties"
          title="Properties"
          className="h-[var(--space-8)] w-[var(--space-8)] p-0"
        >
          <Braces width={16} height={16} />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="end"
        className="min-w-[18rem] max-w-[24rem] p-[var(--space-3)]"
      >
        {!editable ? (
          <dl className="m-0 grid grid-cols-[minmax(0,8rem)_1fr] gap-x-[var(--space-3)] gap-y-[var(--space-1)]">
            {entries.map(([key, value]) => (
              <div key={key} className="contents">
                <dt className="truncate text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-mono)]">
                  {key}
                </dt>
                <dd className="m-0 min-w-0 break-words text-[length:var(--text-xs)] text-[var(--text-primary)] font-[family-name:var(--font-sans)]">
                  {formatPropValue(value)}
                </dd>
              </div>
            ))}
          </dl>
        ) : (
          <div className="flex flex-col gap-[var(--space-2)]">
            {entries.map(([key, value]) => (
              <div key={key} className="flex items-center gap-[var(--space-2)]">
                <span className="w-[8rem] shrink-0 truncate text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-mono)]">
                  {key}
                </span>
                {isEditableScalar(value) ? (
                  <Input
                    defaultValue={valueToInput(value)}
                    aria-label={`Value for ${key}`}
                    className="h-[var(--space-7)] flex-1 min-w-0 text-[length:var(--text-xs)]"
                    onBlur={(e) => commitValue(key, e.currentTarget.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') {
                        e.preventDefault()
                        e.currentTarget.blur()
                      }
                    }}
                  />
                ) : (
                  <span
                    title="Lists and objects are edited in the page body"
                    className="flex-1 min-w-0 break-words text-[length:var(--text-xs)] text-[var(--text-primary)]"
                  >
                    {formatPropValue(value)}
                  </span>
                )}
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  aria-label={`Remove ${key}`}
                  className="h-[var(--space-7)] w-[var(--space-7)] shrink-0 p-0 text-[var(--text-muted)] hover:text-[var(--danger)]"
                  onClick={() => removeProp.mutate(key)}
                >
                  <Trash2 width={13} height={13} />
                </Button>
              </div>
            ))}

            {/* Add a new property. */}
            <div className="flex items-center gap-[var(--space-2)] border-t border-[var(--border-subtle)] pt-[var(--space-2)]">
              <Input
                value={newKey}
                onChange={(e) => setNewKey(e.target.value)}
                placeholder="key"
                aria-label="New property key"
                className="h-[var(--space-7)] w-[8rem] shrink-0 text-[length:var(--text-xs)] font-[family-name:var(--font-mono)]"
              />
              <Input
                value={newValue}
                onChange={(e) => setNewValue(e.target.value)}
                placeholder="value"
                aria-label="New property value"
                className="h-[var(--space-7)] flex-1 min-w-0 text-[length:var(--text-xs)]"
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault()
                    addProp()
                  }
                }}
              />
              <Button
                type="button"
                variant="ghost"
                size="sm"
                aria-label="Add property"
                disabled={newKey.trim() === ''}
                className="h-[var(--space-7)] w-[var(--space-7)] shrink-0 p-0"
                onClick={addProp}
              >
                {newKey.trim() === '' ? (
                  <Plus width={13} height={13} />
                ) : (
                  <Check width={13} height={13} />
                )}
              </Button>
            </div>
          </div>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
