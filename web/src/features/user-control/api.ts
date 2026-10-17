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
import { api } from '@/lib/api'

import type {
  AuditLogItem,
  Capabilities,
  ControlList,
  EffectiveView,
  Limits,
  ListMember,
  OverridePayload,
  PlanBinding,
  PolicyPayload,
  PolicyView,
  Schedule,
  UserOverride,
} from './types'

export type Envelope<T> = { success: boolean; message?: string; data?: T }

const base = '/api/user-control'

export async function getCapabilities() {
  const res = await api.get(`${base}/capabilities`)
  return res.data as Envelope<Capabilities>
}

export async function listPolicies() {
  const res = await api.get(`${base}/policies`)
  return res.data as Envelope<{ items: PolicyView[] }>
}

export async function createPolicy(payload: PolicyPayload) {
  const res = await api.post(`${base}/policies`, payload)
  return res.data as Envelope<PolicyView>
}

export async function updatePolicy(id: number, payload: PolicyPayload) {
  const res = await api.put(`${base}/policies/${id}`, payload)
  return res.data as Envelope<PolicyView>
}

export async function deletePolicy(id: number) {
  const res = await api.delete(`${base}/policies/${id}`)
  return res.data as Envelope<{ id: number }>
}

export async function listControlLists() {
  const res = await api.get(`${base}/lists`)
  return res.data as Envelope<{ items: ControlList[] }>
}

export async function createControlList(payload: {
  name: string
  description?: string
  policy_id?: number
  enabled?: boolean
}) {
  const res = await api.post(`${base}/lists`, payload)
  return res.data as Envelope<ControlList>
}

export async function updateControlList(
  id: number,
  payload: {
    name: string
    description?: string
    policy_id?: number
    enabled?: boolean
  }
) {
  const res = await api.put(`${base}/lists/${id}`, payload)
  return res.data as Envelope<ControlList>
}

export async function deleteControlList(id: number) {
  const res = await api.delete(`${base}/lists/${id}`)
  return res.data as Envelope<{ id: number }>
}

export async function listMembers(listId: number) {
  const res = await api.get(`${base}/lists/${listId}/members`)
  return res.data as Envelope<{ items: ListMember[] }>
}

export async function addMembers(
  listId: number,
  userIds: number[],
  note?: string
) {
  const res = await api.post(`${base}/lists/${listId}/members`, {
    user_ids: userIds,
    note,
  })
  return res.data as Envelope<{
    list_id: number
    inserted: number
    requested: number
  }>
}

export async function removeMembers(listId: number, userIds: number[]) {
  const res = await api.delete(`${base}/lists/${listId}/members`, {
    data: { user_ids: userIds },
  })
  return res.data as Envelope<{ list_id: number; removed: number }>
}

export async function listOverrides() {
  const res = await api.get(`${base}/overrides`)
  return res.data as Envelope<{ items: UserOverride[] }>
}

export async function getUserOverride(userId: number) {
  const res = await api.get(`${base}/users/${userId}/override`)
  return res.data as Envelope<UserOverride>
}

export async function putUserOverride(
  userId: number,
  payload: OverridePayload
) {
  const res = await api.put(`${base}/users/${userId}/override`, payload)
  return res.data as Envelope<UserOverride>
}

export async function deleteUserOverride(userId: number) {
  const res = await api.delete(`${base}/users/${userId}/override`)
  return res.data as Envelope<{ user_id: number }>
}

export async function getEffective(userId: number, at?: string) {
  const res = await api.get(`${base}/users/${userId}/effective`, {
    params: at ? { at } : undefined,
  })
  return res.data as Envelope<EffectiveView>
}

export async function simulate(userId: number, at?: string) {
  const res = await api.get(`${base}/simulate`, {
    params: { user_id: userId, at },
  })
  return res.data as Envelope<EffectiveView>
}

export async function listPlanBindings() {
  const res = await api.get(`${base}/plan-bindings`)
  return res.data as Envelope<{ items: PlanBinding[] }>
}

export async function putPlanBinding(planId: number, policyId: number) {
  const res = await api.put(`${base}/plans/${planId}/binding`, {
    policy_id: policyId,
  })
  return res.data as Envelope<PlanBinding>
}

export async function deletePlanBinding(planId: number) {
  const res = await api.delete(`${base}/plans/${planId}/binding`)
  return res.data as Envelope<{ plan_id: number }>
}

export async function applyPolicy(payload: {
  user_ids: number[]
  policy_id: number
  mode?: 'merge' | 'replace'
  expires_at?: number
  reason?: string
}) {
  const res = await api.post(`${base}/apply`, payload)
  return res.data as Envelope<{
    requested: number
    applied: number
    skipped: number
    failed: number
    items: {
      user_id: number
      username: string
      status: string
      message?: string
    }[]
  }>
}

export async function listAudit(params: {
  p?: number
  page_size?: number
  start_timestamp?: number
  end_timestamp?: number
}) {
  const res = await api.get(`${base}/audit`, { params })
  return res.data as Envelope<{
    items: AuditLogItem[]
    total: number
    page: number
    page_size: number
  }>
}

export type { Limits, Schedule }
