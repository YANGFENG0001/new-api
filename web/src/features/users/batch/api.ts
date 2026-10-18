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
  BatchAction,
  BatchActionInfo,
  BatchExecuteResult,
  BatchJob,
  BatchJobItem,
  BatchPreview,
  BatchRequest,
} from './types'

type Envelope<T> = { success: boolean; message?: string; data?: T }

export async function getBatchActions(): Promise<
  Envelope<{ actions: BatchActionInfo[]; can_read: boolean; max_users: number }>
> {
  const res = await api.get('/api/user/batch/actions')
  return res.data
}

export async function previewBatch(
  request: BatchRequest
): Promise<Envelope<BatchPreview>> {
  const res = await api.post('/api/user/batch/preview', request)
  return res.data
}

export async function executeBatch(
  request: BatchRequest,
  batchHash: string,
  proofToken: string
): Promise<Envelope<BatchExecuteResult>> {
  const res = await api.post(
    '/api/user/batch/execute',
    { ...request, batch_hash: batchHash },
    {
      headers: { 'X-Security-Proof': proofToken },
      singleUseAuthorization: true,
    }
  )
  return res.data
}

export async function listBatchJobs(params: {
  page?: number
  page_size?: number
  action?: BatchAction | ''
}): Promise<
  Envelope<{
    items: BatchJob[]
    total: number
    page: number
    page_size: number
  }>
> {
  const res = await api.get('/api/user/batch/jobs', {
    params: { p: params.page, page_size: params.page_size },
  })
  return res.data
}

export async function getBatchJob(
  id: string
): Promise<Envelope<{ job: BatchJob; items: BatchJobItem[] }>> {
  const res = await api.get(`/api/user/batch/jobs/${encodeURIComponent(id)}`)
  return res.data
}

export async function listBatchJobItems(id: string): Promise<
  Envelope<{
    items: BatchJobItem[]
    total: number
    page: number
    page_size: number
  }>
> {
  const res = await api.get(
    `/api/user/batch/jobs/${encodeURIComponent(id)}/items`
  )
  return res.data
}

export async function retryBatchJob(
  id: string,
  proofToken: string
): Promise<Envelope<BatchExecuteResult>> {
  const res = await api.post(
    `/api/user/batch/jobs/${encodeURIComponent(id)}/retry`,
    {},
    {
      headers: { 'X-Security-Proof': proofToken },
      singleUseAuthorization: true,
    }
  )
  return res.data
}

export async function cancelBatchJob(id: string): Promise<Envelope<BatchJob>> {
  const res = await api.post(
    `/api/user/batch/jobs/${encodeURIComponent(id)}/cancel`
  )
  return res.data
}
