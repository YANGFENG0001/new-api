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

import { syncSourceSchema, syncRequestSchema } from './sync-api'

const recordJSON = z.string().refine((value) => {
  try {
    return z.record(z.string(), z.string()).safeParse(JSON.parse(value)).success
  } catch {
    return false
  }
}, 'Invalid JSON')
export const syncSourceFormSchema = syncSourceSchema
  .extend({
    refresh_mode: z.enum(['manual', 'login', 'oauth']),
    refresh_url: z.string().max(4096),
    auto_refresh: z.boolean(),
    refresh_before: z.number().int().min(1).max(604800),
    credential_revision: z.number().int().min(0),
    replace_request: z.boolean(),
    auth_type: z.enum(['none', 'bearer', 'header', 'basic']),
    token: z.string(),
    auth_header: z.string(),
    username: z.string(),
    password: z.string(),
    refresh_token: z.string(),
    client_id: z.string(),
    client_secret: z.string(),
    headers_json: recordJSON,
    query_json: recordJSON,
    body: z.string().refine((value) => {
      if (!value) return true
      try {
        JSON.parse(value)
        return true
      } catch {
        return false
      }
    }, 'Invalid JSON'),
  })
  .superRefine((value, ctx) => {
    if (!value.replace_request) return
    if (
      (value.auth_type === 'bearer' || value.auth_type === 'header') &&
      !value.token &&
      value.refresh_mode === 'manual'
    ) {
      ctx.addIssue({ code: 'custom', path: ['token'], message: 'Required' })
    }
    if (value.auth_type === 'header' && !value.auth_header) {
      ctx.addIssue({
        code: 'custom',
        path: ['auth_header'],
        message: 'Required',
      })
    }
    if (value.refresh_mode !== 'manual') {
      if (!value.refresh_url) {
        ctx.addIssue({
          code: 'custom',
          path: ['refresh_url'],
          message: 'Required',
        })
      }
      if (
        value.refresh_mode === 'login' &&
        (!value.username || !value.password)
      ) {
        ctx.addIssue({
          code: 'custom',
          path: ['password'],
          message: 'Required',
        })
      }
      if (value.refresh_mode === 'oauth' && !value.refresh_token) {
        ctx.addIssue({
          code: 'custom',
          path: ['refresh_token'],
          message: 'Required',
        })
      }
    }
    if (value.body && value.method !== 'POST') {
      ctx.addIssue({
        code: 'custom',
        path: ['body'],
        message: 'Invalid parameters',
      })
    }
  })
export type SyncSourceForm = z.infer<typeof syncSourceFormSchema>
export function sourceRequest(values: SyncSourceForm) {
  if (!values.replace_request) return undefined
  return syncRequestSchema.parse({
    auth_type: values.auth_type,
    token: values.token,
    auth_header: values.auth_header,
    username: values.username,
    password: values.password,
    refresh_token: values.refresh_token,
    client_id: values.client_id,
    client_secret: values.client_secret,
    headers: JSON.parse(values.headers_json),
    query: JSON.parse(values.query_json),
    body: values.body,
  })
}
