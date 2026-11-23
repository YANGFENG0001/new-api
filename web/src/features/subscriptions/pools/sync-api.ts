/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { z } from 'zod'

import { poolRequest } from './api'

export const syncCycleMappingSchema = z.object({
  key: z.string().min(1).max(64),
  name: z.string().max(128),
  seconds: z.number().int().min(1).max(315360000),
  utilization_path: z.string().min(1),
  scale: z.enum(['ratio', 'percent']),
  reset_path: z.string().min(1),
  sampled_at_path: z.string(),
})
export const syncMappingSchema = z.object({
  success_path: z.string(),
  accounts_path: z.string().min(1),
  id_path: z.string().min(1),
  name_path: z.string(),
  sampled_at_path: z.string().min(1),
  source_path: z.string(),
  expiry_path: z.string(),
  time_format: z.enum(['rfc3339', 'unix_s', 'unix_ms']),
  cycles: z.array(syncCycleMappingSchema).min(1).max(32),
})
export const syncSourceSchema = z.object({
  id: z.number().int().min(0),
  name: z.string().trim().min(1).max(128),
  url: z.url().max(4096),
  method: z.enum(['GET', 'POST']),
  enabled: z.boolean(),
  interval_seconds: z.number().int().min(10).max(86400),
  stale_seconds: z.number().int().min(10).max(604800),
  timeout_seconds: z.number().int().min(1).max(60),
  allow_http: z.boolean(),
  private_ips: z.string(),
  version: z.number(),
  mapping: syncMappingSchema,
  refresh_mode: z.enum(['manual', 'login', 'oauth']).default('manual'),
  refresh_url: z.string().max(4096).default(''),
  auto_refresh: z.boolean().default(false),
  refresh_before: z.number().int().min(1).max(604800).default(86400),
  credential_revision: z.number().int().min(0).default(0),
})
export type SyncMapping = z.infer<typeof syncMappingSchema>
export type SyncCycleMapping = z.infer<typeof syncCycleMappingSchema>
export type SyncSource = z.infer<typeof syncSourceSchema> & {
  fresh_cycles?: number
  reported_cycles?: number
  oldest_observed_at?: number
  stale_accounts?: string[]
  effective_stale_seconds?: number
  fetch_status?: string
  observation_status?: string
  observed_at?: number
  has_credentials?: boolean
  last_attempt?: number
  last_fetch?: number
  last_applied?: number
  last_error?: string
  next_run?: number
  token_expires_at?: number
  last_auth_success?: number
  last_token_refresh?: number
  auth_error?: string
  auth_next_attempt?: number
  refresh_blocked?: boolean
}
export const syncRequestSchema = z.object({
  auth_type: z.enum(['none', 'bearer', 'header', 'basic']),
  auth_header: z.string().optional(),
  username: z.string().optional(),
  password: z.string().optional(),
  token: z.string().optional(),
  headers: z.record(z.string(), z.string()).optional(),
  query: z.record(z.string(), z.string()).optional(),
  body: z.string().optional(),
  refresh_token: z.string().optional(),
  client_id: z.string().optional(),
  client_secret: z.string().optional(),
})
export type SyncRequest = z.infer<typeof syncRequestSchema>
export type SyncCycle = {
  key: string
  name: string
  seconds: number
  utilization: number | null
  window_start: number
  window_end: number
  sampled_at: number
  source: string
  error: string
}
export type SyncAccount = {
  id: string
  name: string
  expiry: number
  sampled_at: number
  cycles: SyncCycle[]
}
export type SyncObservation = {
  seconds?: number
  estimated_utilization?: number | null
  local_delta?: number
  key: string
  name: string
  utilization: number | null
  sampled_at: number
  fetched_at: number
  window_end: number
  stale: boolean
  pending: string
  digest?: string
}
export const syncBindingSchema = z.object({
  last_error: z.string().optional(),
  pool_id: z.number().int().positive(),
  source_id: z.number().int().positive(),
  account_id: z.string().min(1).max(128),
  enabled: z.boolean(),
  sync_expiry: z.boolean(),
  stale_policy: z.enum(['stop', 'local']),
  grace_seconds: z.number().int().min(0).max(86400),
  version: z.number().int(),
  cycles: z
    .array(
      z.object({
        local_key: z.string().min(1),
        upstream_key: z.string().min(1),
        align_reset: z.boolean(),
        enforce: z.boolean(),
        threshold: z.number().positive().max(1),
        max_shift_seconds: z.number().int().min(0).max(315360000),
        quota_per_full_window: z
          .number()
          .int()
          .min(0)
          .max(Number.MAX_SAFE_INTEGER)
          .optional(),
      })
    )
    .max(32),
})
export type SyncBinding = z.infer<typeof syncBindingSchema>
export type SyncChange = {
  previous_utilization?: number | null
  utilization?: number | null
  pool_id: number
  key: string
  before: number
  after: number
  message: string
}
export type SyncResult = {
  preview: boolean
  accounts: SyncAccount[]
  changes: SyncChange[]
}
export type SyncSourceList = {
  sources: SyncSource[]
  template: SyncMapping
  privileged: boolean
}
export type SyncState = {
  accounts: SyncAccount[]
  events: {
    id: number
    pool_id: number
    actor_id: number
    action: string
    changes?: SyncChange[]
    message: string
    created_at: number
  }[]
}
const base = '/api/subscription/admin'
export const syncAPI = {
  remove: (source: SyncSource, proof: string) =>
    poolRequest<null>(
      'delete',
      `${base}/sync-sources/${source.id}`,
      {
        version: source.version,
        credential_revision: source.credential_revision,
        confirm: true,
      },
      { 'X-Security-Proof': proof }
    ),
  sources: () => poolRequest<SyncSourceList>('get', `${base}/sync-sources`),
  save: (source: SyncSource, request?: SyncRequest, proofToken?: string) =>
    poolRequest<SyncSource>(
      source.id ? 'put' : 'post',
      `${base}/sync-sources${source.id ? `/${source.id}` : ''}`,
      { ...source, request },
      { 'X-Security-Proof': proofToken ?? '' }
    ),
  run: (id: number, preview: boolean) =>
    poolRequest<SyncResult>('post', `${base}/sync-sources/${id}/run`, {
      preview,
    }),
  authenticate: (
    source: SyncSource,
    action: 'check' | 'refresh' | 'replace',
    token = '',
    proofToken = ''
  ) =>
    poolRequest<SyncSource>(
      'post',
      `${base}/sync-sources/${source.id}/auth`,
      {
        action,
        token,
        version: source.version,
        credential_revision: source.credential_revision,
        confirm: true,
      },
      { 'X-Security-Proof': proofToken }
    ),
  state: (id: number) =>
    poolRequest<SyncState>('get', `${base}/sync-sources/${id}/state`),
  binding: (id: number) =>
    poolRequest<{ binding: SyncBinding; observations: SyncObservation[] }>(
      'get',
      `${base}/pools/${id}/sync`
    ),
  saveBinding: (binding: SyncBinding) =>
    poolRequest<null>('put', `${base}/pools/${binding.pool_id}/sync`, binding),
  unbind: (id: number, version: number) =>
    poolRequest<null>('post', `${base}/pools/${id}/sync/unbind`, {
      version,
      confirm: true,
    }),
  confirmReset: (id: number, key: string, digest: string, version: number) =>
    poolRequest<null>('post', `${base}/pools/${id}/sync/confirm-reset`, {
      key,
      digest,
      version,
      confirm: true,
    }),
}
export function emptySyncSource(mapping: SyncMapping): SyncSource {
  return {
    id: 0,
    name: '',
    url: '',
    method: 'GET',
    enabled: false,
    interval_seconds: 60,
    stale_seconds: 300,
    timeout_seconds: 10,
    allow_http: false,
    private_ips: '',
    version: 0,
    mapping: structuredClone(mapping),
    refresh_mode: 'manual',
    refresh_url: '',
    auto_refresh: false,
    refresh_before: 86400,
    credential_revision: 0,
  }
}

export function syncStaleSeconds(
  source?: Pick<
    SyncSource,
    | 'stale_seconds'
    | 'interval_seconds'
    | 'timeout_seconds'
    | 'effective_stale_seconds'
  >
): number {
  if (!source) return 300
  if (source.effective_stale_seconds && source.effective_stale_seconds > 0) {
    return source.effective_stale_seconds
  }
  return Math.max(
    source.stale_seconds,
    2 * source.interval_seconds + source.timeout_seconds + 10
  )
}

// Zero is reported; absent/null/invalid percentages do not create quota cards.
export function reportedSyncAccounts(accounts: SyncAccount[]): SyncAccount[] {
  return accounts
    .map((account) => ({
      ...account,
      cycles: account.cycles.filter(
        (c) =>
          c.utilization != null &&
          Number.isFinite(c.utilization) &&
          c.utilization >= 0
      ),
    }))
    .filter((account) => account.cycles.length > 0)
}

export function syncCycleName(name: string, seconds?: number): string {
  if (!seconds || !/^[0-9]+(?:\.[0-9]+)?[smhd]$/.test(name)) return name
  for (const [unit, suffix] of [
    [86400, 'd'],
    [3600, 'h'],
    [60, 'm'],
    [1, 's'],
  ] as const) {
    if (seconds % unit === 0) return `${seconds / unit}${suffix}`
  }
  return name
}
