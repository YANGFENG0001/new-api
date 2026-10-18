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
export type BatchAction =
  | 'enable'
  | 'disable'
  | 'group'
  | 'add_quota'
  | 'subtract_quota'
  | 'override_quota'
  | 'delete'
  | 'promote'
  | 'demote'
  | 'reset_2fa'
  | 'reset_passkey'
  | 'clear_bindings'
  | 'invalidate_subscriptions'
  | 'delete_subscriptions'
  | 'block'
  | 'unblock'

export interface BatchActionInfo {
  action: BatchAction
  permission: string
  allowed: boolean
  destructive: boolean
  quota: boolean
}

export interface BatchRequest {
  action: BatchAction
  user_ids: number[]
  group?: string
  quota?: number
  reason?: string
  note?: string
  idempotency_key?: string
}

export interface BatchItemPreview {
  user_id: number
  username: string
  role: number
  status: number
  group: string
  quota: number
  eligible: boolean
  skip_reason?: string
  warnings?: string[]
  before: Record<string, unknown>
  after: Record<string, unknown>
}

export interface BatchPreview {
  action: BatchAction
  batch_hash: string
  total: number
  eligible: number
  skipped: number
  warnings?: string[]
  items: BatchItemPreview[]
}

export interface BatchJob {
  id: string
  action: BatchAction
  batch_hash: string
  total: number
  succeeded: number
  failed: number
  skipped: number
  status: string
  created_by: number
  created_at: number
  started_at: number
  finished_at: number
}

export interface BatchJobItem {
  id: number
  job_id: string
  user_id: number
  username: string
  status: string
  error_code?: string
  error_message?: string
  before?: string
  after?: string
}

export interface BatchExecuteResult {
  job: BatchJob
  items: BatchJobItem[]
}
