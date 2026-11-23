import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

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
import type { SyncObservation, SyncBinding, SyncState } from './sync-api'

export type CycleRule = {
  key: string
  name: string
  seconds: number
  next_reset: number
  limit: number
  unlimited: boolean
  shared: boolean
  enabled: boolean
  upstream_limit: number
  enforce_upstream: boolean
}
export type Pool = {
  id: number
  name: string
  note: string
  enabled: boolean
  end_time: number
  version: number
  external_source: string
  external_id: string
  updated_at: number
  cycles: CycleRule[]
  channels: number[]
}
export type PoolSyncCreation = {
  source_id: number
  source_version: number
  account_id: string
  sync_expiry: boolean
}
export type AllocationConfig = {
  plan_id: number
  mode: string
  pool_id: number
  fixed_end_time: number
  version: number
  cycles: CycleRule[]
}
export type CycleView = {
  limit_unknown?: boolean
  key: string
  name: string
  seconds: number
  shared: boolean
  unlimited: boolean
  limit: number
  used: number
  reserved: number
  adjustment: number
  start_time: number
  next_reset: number
  pending: boolean
}
export type Usage = {
  subscription_id: number
  plan_id: number
  status: string
  pending: boolean
  managed: boolean
  end_time: number
  cycles: CycleView[]
}
export type Observation = {
  key: string
  source: string
  unit: string
  used: number
  limit: number
  sampled_at: number
  window_start: number
  window_end: number
}
export type Overview = {
  subscription_ids?: number[]
  id: number
  name: string
  end_time: number
  updated_at: number
  cycles: CycleView[]
  observations: Observation[]
  sync_observations?: SyncObservation[]
}
export type Allocation = {
  subscription_id: number
  user_id: number
  pool_id: number
  pending: boolean
  version: number
}
export type Assignment = {
  usage_digest?: string
  subscription_id: number
  pool_id: number
  version: number
  end_time: number
  confirm: boolean
  pool_version: number
}
export type AssignmentPreview = {
  legacy_carry?: {
    amount: number
    excluded_reserved: number
    window_start: number
    window_end: number
    keys: string[]
    digest: string
  }
  subscription_id: number
  current: Allocation
  target_pool_id: number
  pool_version: number
  end_time: number
  target_cycles: CycleView[]
  usage_transfer: boolean
}
export type AssignmentResult = {
  subscription_id: number
  success: boolean
  message: string
  preview?: AssignmentPreview | null
}
export type PoolDetail = {
  sync_events?: SyncState['events']
  cycles: CycleView[]
  members: {
    subscription_id: number
    user_id: number
    username: string
    end_time: number
    status: string
    version: number
  }[]
  events: {
    id: number
    actor_id: number
    action: string
    subscription_id: number
    detail: string
    created_at: number
  }[]
}
export type Capabilities = Partial<
  Record<
    | 'sync_read'
    | 'sync_run'
    | 'sync_credentials'
    | 'sync_binding'
    | 'sync_apply',
    boolean
  >
> &
  Record<
    | 'read'
    | 'manage'
    | 'assign'
    | 'calibrate'
    | 'shared_reset'
    | 'personal_reset'
    | 'batch_migrate'
    | 'audit_read',
    boolean
  >

const base = '/api/subscription/admin'
export async function poolRequest<T>(
  method: 'get' | 'post' | 'put' | 'delete',
  path: string,
  data?: unknown,
  headers?: Record<string, string>
): Promise<T> {
  const response = await api.request<{
    success: boolean
    message?: string
    data: T
  }>({
    method,
    url: path,
    data,
    ...(headers
      ? { headers, singleUseAuthorization: !!headers['X-Security-Proof'] }
      : {}),
  })
  return requireServerSuccess(response.data).data
}
export const poolAPI = {
  remove: (pool: Pool) =>
    poolRequest<null>('delete', `${base}/pools/${pool.id}`, {
      version: pool.version,
      confirm: true,
    }),
  capabilities: () =>
    poolRequest<Capabilities>('get', `${base}/pool-capabilities`),
  list: () => poolRequest<Pool[]>('get', `${base}/pools`),
  catalog: () =>
    poolRequest<{ id: number; name: string }[]>('get', `${base}/pool-catalog`),
  detail: (id: number) => poolRequest<PoolDetail>('get', `${base}/pools/${id}`),
  save: async (pool: Pool & { sync_binding?: SyncBinding }) => {
    const saved = await poolRequest<Pool>(
      'put',
      `${base}/pools/${pool.id}`,
      pool
    )
    return {
      ...pool,
      ...saved,
      cycles: saved.cycles ?? pool.cycles,
      channels: saved.channels ?? pool.channels,
    }
  },
  create: async (pool: Pool & { sync?: PoolSyncCreation }) => {
    const saved = await poolRequest<Pool>('post', `${base}/pools`, pool)
    return {
      ...pool,
      ...saved,
      cycles: saved.cycles ?? pool.cycles,
      channels: saved.channels ?? pool.channels,
    }
  },
  config: async (id: number): Promise<AllocationConfig> => {
    const config = await poolRequest<
      Omit<AllocationConfig, 'cycles'> & { cycles?: CycleRule[] | null }
    >('get', `${base}/plans/${id}/allocation`)
    // Also tolerate older servers during rolling deployments.
    return { ...config, cycles: config.cycles ?? [] }
  },
  saveConfig: (id: number, data: AllocationConfig) =>
    poolRequest<null>('put', `${base}/plans/${id}/allocation`, data),
  allocations: (id: number) =>
    poolRequest<{ allocations: Allocation[]; subscriptions: Usage[] }>(
      'get',
      `${base}/users/${id}/allocations`
    ),
  assign: (data: Assignment) =>
    poolRequest<AssignmentPreview | null>(
      'post',
      `${base}/pool-assignment`,
      data
    ),
  batchAssign: (items: Assignment[]) =>
    poolRequest<AssignmentResult[]>('post', `${base}/pool-assignment/batch`, {
      items,
    }),
  reset: (id: number, key: string) =>
    poolRequest<null>('post', `${base}/pools/${id}/reset`, {
      key,
      confirm: true,
    }),
  resetPersonal: (id: number, key: string) =>
    poolRequest<null>('post', `${base}/user_subscriptions/${id}/cycle-reset`, {
      key,
      confirm: true,
    }),
  snapshot: (id: number, data: unknown) =>
    poolRequest<null>('post', `${base}/pools/${id}/snapshots`, data),
  usage: () =>
    poolRequest<{ server_time: number; subscriptions: Usage[] }>(
      'get',
      '/api/subscription/self/usage'
    ),
  overview: () =>
    poolRequest<Overview[]>('get', '/api/subscription/self/pools'),
}
