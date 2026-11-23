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

const quota = z.number().int().min(0).max(Number.MAX_SAFE_INTEGER)
export const cycleSchema = z
  .object({
    key: z.string().regex(/^[a-zA-Z0-9_-]{1,64}$/),
    name: z.string().trim().min(1).max(128),
    seconds: z.number().int().min(1).max(315360000),
    next_reset: z.number().int().positive(),
    limit: quota,
    unlimited: z.boolean(),
    shared: z.boolean(),
    enabled: z.boolean(),
    upstream_limit: quota,
    enforce_upstream: z.boolean(),
  })
  .refine((v) => v.unlimited || v.limit > 0, {
    message: 'A limited cycle requires a positive quota',
    path: ['limit'],
  })
  .refine((v) => !v.enforce_upstream || (!v.shared && v.upstream_limit > 0), {
    message: 'Invalid subscription cycle',
    path: ['upstream_limit'],
  })
export const poolSchema = z
  .object({
    id: z.number(),
    name: z.string().trim().min(1).max(128),
    note: z.string().max(4096),
    enabled: z.boolean(),
    end_time: z.number().int().positive().max(253402300799),
    version: z.number(),
    external_source: z.string().max(64),
    external_id: z.string().max(128),
    updated_at: z.number(),
    cycles: z.array(cycleSchema).min(1).max(32),
    channels: z.array(z.number().int().positive()).max(1000),
  })
  .refine((v) => new Set(v.cycles.map((c) => c.key)).size === v.cycles.length, {
    path: ['cycles'],
    message: 'Invalid or duplicate cycle key',
  })
  .refine((v) => v.end_time > Math.floor(Date.now() / 1000), {
    path: ['end_time'],
    message: 'Subscription expiry is in the past',
  })
export const allocationSchema = z
  .object({
    plan_id: z.number(),
    mode: z.enum(['legacy', 'independent', 'fixed', 'assign']),
    pool_id: z.number(),
    fixed_end_time: z.number().int().min(0),
    version: z.number(),
    cycles: z.array(cycleSchema).max(32),
  })
  .refine((v) => v.mode !== 'fixed' || v.pool_id > 0, {
    path: ['pool_id'],
    message: 'Please select an upstream pool',
  })
  .refine((v) => v.mode !== 'independent' || v.cycles.length > 0, {
    path: ['cycles'],
    message: 'At least one subscription cycle is required',
  })
export function newCycle(name: string): z.infer<typeof cycleSchema> {
  return {
    key: crypto.randomUUID(),
    name,
    seconds: 18000,
    next_reset: Math.floor(Date.now() / 1000) + 18000,
    limit: 1000000,
    unlimited: false,
    shared: false,
    enabled: true,
    upstream_limit: 0,
    enforce_upstream: false,
  }
}
export function emptyPool(): z.infer<typeof poolSchema> {
  return {
    id: 0,
    name: '',
    note: '',
    enabled: true,
    end_time: Math.floor(Date.now() / 1000) + 30 * 86400,
    version: 0,
    external_source: '',
    external_id: '',
    updated_at: 0,
    cycles: [],
    channels: [],
  }
}

export const snapshotSchema = z
  .object({
    key: z.string().min(1),
    unit: z.string().min(1).max(32),
    used: quota,
    limit: quota,
    window_start: z.number().int().positive(),
    window_end: z.number().int().positive(),
    calibrate: z.boolean(),
  })
  .refine((v) => v.window_end > v.window_start, {
    path: ['window_end'],
    message: 'Invalid upstream snapshot',
  })
  .refine((v) => !v.calibrate || v.unit === 'quota', {
    path: ['unit'],
    message: 'Only matching quota units can be calibrated',
  })
