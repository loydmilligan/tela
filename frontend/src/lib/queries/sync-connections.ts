import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'
import type { ApiKeyRow } from '../types'

// "Connect your vault" — user self-service WebDAV sync tokens (sync §16). Unlike
// the instance-admin /api/api_keys CRUD, any member can mint a token for THEIR
// OWN access (two-way or read-only, optionally pinned to one space) and gets a
// ready-to-paste rclone setup. The raw key is returned ONCE (inside the
// config-create command); the list scrubs it. Revoke reuses the owner-gated
// DELETE /api/api_keys/{id}.

export const syncConnectionKeys = {
  all: ['sync-connections'] as const,
  list: () => [...syncConnectionKeys.all, 'list'] as const,
}

// RcloneSetup is the server-authoritative copy-paste configuration (the WebDAV
// URL and the required --ignore-size flag come from the backend, not hardcoded).
export interface RcloneSetup {
  webdav_url: string
  remote_name: string
  remote_path: string
  config_create_command: string
  // bisync (two-way) or a one-way pull, depending on read_only.
  sync_command: string
  read_only: boolean
  excludes: string
}

export interface SyncConnectionCreated {
  connection: ApiKeyRow
  rclone: RcloneSetup
}

export interface CreateSyncConnectionInput {
  name: string
  // Omitted → whole workspace (all spaces you can access). Numeric id → one space.
  space_id?: number | null
  // false → two-way (write); true → sync-down only (read).
  read_only?: boolean
}

export function useSyncConnections() {
  return useQuery({
    queryKey: syncConnectionKeys.list(),
    queryFn: async () => {
      const { connections } = await api<{ connections: ApiKeyRow[] }>(
        '/api/sync/connections',
      )
      return connections
    },
    staleTime: 30_000,
  })
}

export function useCreateSyncConnection() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (input: CreateSyncConnectionInput) => {
      const body: Record<string, unknown> = {
        name: input.name,
        read_only: input.read_only ?? false,
      }
      if (input.space_id != null) body.space_id = input.space_id
      return await api<SyncConnectionCreated>('/api/sync/connections', {
        method: 'POST',
        body: JSON.stringify(body),
      })
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: syncConnectionKeys.list() })
    },
  })
}

export function useRevokeSyncConnection() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (id: number) => {
      await api<void>(`/api/api_keys/${id}`, { method: 'DELETE' })
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: syncConnectionKeys.list() })
    },
  })
}
