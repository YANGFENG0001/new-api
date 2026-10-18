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
export interface RateLimits {
  rpm?: number
  rpd?: number
  concurrent?: number
  burst?: number
}

export interface RequestLimits {
  daily?: number
  monthly?: number
}

export interface UsageLimits {
  daily_quota?: number
  monthly_quota?: number
  input_tokens?: number
  output_tokens?: number
}

export interface ModelLimits {
  allow?: string[]
  block?: string[]
}

export interface ChannelLimits {
  allow_ids?: number[]
  block_ids?: number[]
  allow_types?: number[]
  block_types?: number[]
  allow_groups?: string[]
}

export interface APIKeyLimits {
  max_active?: number
  max_total?: number
}

export interface Limits {
  scoped_rules?: ScopedRule[]
  blocked?: boolean
  allow_relay?: boolean
  allow_playground?: boolean
  rate?: RateLimits
  requests?: RequestLimits
  usage?: UsageLimits
  models?: ModelLimits
  channels?: ChannelLimits
  api_keys?: APIKeyLimits
}

export interface ScopedRule {
  id: string
  priority?: number
  name?: string
  enabled?: boolean
  models?: string[]
  channel_ids?: number[]
  limits: Limits
  actions?: ControlActions
}

export interface ControlActions {
  request_mode?: 'merge' | 'replace'
  request_body?: string
  prompt_prefix?: string
  prompt_suffix?: string
  route_model?: string
  reasoning_effort?: string
  response?: {
    mode: 'text' | 'json' | 'error'
    body: string
    status?: number
    error_code?: string
  }
}

export interface ScheduleRule {
  id?: string
  name?: string
  enabled?: boolean
  priority?: number
  days?: number[]
  dates?: string[]
  start_date?: string
  end_date?: string
  start?: string
  end?: string
  limits: Limits
}

export interface Schedule {
  timezone?: string
  rules?: ScheduleRule[]
  default?: Limits
}

export interface UserControlPolicy {
  id: number
  name: string
  description: string
  enabled: boolean
  priority: number
  limits: string
  schedule: string
  version: number
  created_by: number
  updated_by: number
  created_at: number
  updated_at: number
}

export interface PolicyView {
  policy: UserControlPolicy
  limits: Limits
  schedule?: Schedule | null
}

export interface PolicyPayload {
  name: string
  description?: string
  enabled?: boolean
  priority?: number
  limits: Limits
  schedule?: Schedule | null
}

export interface ControlList {
  id: number
  name: string
  description: string
  policy_id: number
  enabled: boolean
  members: number
  created_by: number
  updated_by: number
  created_at: number
  updated_at: number
}

export interface ListMember {
  id: number
  list_id: number
  user_id: number
  username: string
  note: string
  added_by: number
  created_at: number
}

export interface UserOverride {
  id: number
  user_id: number
  policy_id: number
  limits: string
  schedule: string
  mode: 'merge' | 'replace'
  expires_at: number
  reason: string
  username: string
  limits_view: Limits
  schedule_view?: Schedule | null
  created_by: number
  updated_by: number
  created_at: number
  updated_at: number
}

export interface OverridePayload {
  policy_id?: number
  limits: Limits
  schedule?: Schedule | null
  mode?: 'merge' | 'replace'
  expires_at?: number
  reason?: string
}

export interface PlanBinding {
  id: number
  plan_id: number
  policy_id: number
  plan_title: string
  created_by: number
  created_at: number
  updated_at: number
}

export interface EffectiveView {
  user_id: number
  role: number
  resolved_at: string
  timezone: string
  has_policy: boolean
  sources: string[]
  policy_ids: number[]
  schedule_hits: string[]
  limits: Limits
  details: {
    source: string
    source_id: number
    policy_id: number
    policy_name: string
    priority: number
    active: boolean
    schedule_hits?: string[]
    limits: Limits
  }[]
}

export interface Capabilities {
  read: boolean
  list_write: boolean
  policy_write: boolean
  override_write: boolean
  override_relax: boolean
  apply: boolean
  audit_read: boolean
  default_timezone: string
  root: boolean
}

export interface AuditLogItem {
  id?: number
  event_id?: string
  user_id?: number
  username?: string
  action?: string
  category?: string
  created_at?: number
  success?: boolean
  other?: Record<string, unknown>
}
