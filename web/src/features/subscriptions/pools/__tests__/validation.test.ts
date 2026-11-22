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
import { describe, expect, test } from 'vitest'

import {
  allocationSchema,
  cycleSchema,
  emptyPool,
  newCycle,
  poolSchema,
  snapshotSchema,
} from '../schema'

describe('aligned subscription validation', () => {
  test('new custom cycles are independent and accept arbitrary elapsed durations', () => {
    const cycle = { ...newCycle('Custom'), seconds: 137 }
    expect(cycle.shared).toBe(false)
    expect(cycleSchema.safeParse(cycle).success).toBe(true)
  })

  test('limited cycles reject zero quota but unlimited cycles accept it', () => {
    const cycle = { ...newCycle('Custom'), limit: 0 }
    expect(cycleSchema.safeParse(cycle).success).toBe(false)
    expect(cycleSchema.safeParse({ ...cycle, unlimited: true }).success).toBe(
      true
    )
  })

  test('upstream enforcement needs a positive total and cannot duplicate shared enforcement', () => {
    const cycle = { ...newCycle('Custom'), enforce_upstream: true }
    expect(cycleSchema.safeParse(cycle).success).toBe(false)
    expect(
      cycleSchema.safeParse({ ...cycle, upstream_limit: 900 }).success
    ).toBe(true)
    expect(
      cycleSchema.safeParse({ ...cycle, upstream_limit: 900, shared: true })
        .success
    ).toBe(false)
  })

  test('pool saves reject duplicate cycle keys, expired dates and unsafe quota integers', () => {
    const cycle = newCycle('Custom')
    const pool = { ...emptyPool(), name: 'Pool', cycles: [cycle] }
    expect(poolSchema.safeParse(pool).success).toBe(true)
    expect(
      poolSchema.safeParse({ ...pool, cycles: [cycle, cycle] }).success
    ).toBe(false)
    expect(poolSchema.safeParse({ ...pool, end_time: 1 }).success).toBe(false)
    expect(
      cycleSchema.safeParse({ ...cycle, limit: Number.MAX_SAFE_INTEGER + 1 })
        .success
    ).toBe(false)
  })

  test('allocation modes require an actual pool or independent cycles', () => {
    const config = {
      plan_id: 1,
      mode: 'fixed',
      pool_id: 0,
      version: 0,
      fixed_end_time: 0,
      cycles: [],
    }
    expect(allocationSchema.safeParse(config).success).toBe(false)
    expect(
      allocationSchema.safeParse({ ...config, mode: 'independent' }).success
    ).toBe(false)
    expect(
      allocationSchema.safeParse({ ...config, mode: 'assign' }).success
    ).toBe(true)
  })

  test('manual calibration rejects incompatible units and inverted windows', () => {
    const snapshot = {
      key: 'custom',
      unit: 'quota',
      used: 10,
      limit: 100,
      window_start: 100,
      window_end: 200,
      calibrate: true,
    }
    expect(snapshotSchema.safeParse(snapshot).success).toBe(true)
    expect(
      snapshotSchema.safeParse({ ...snapshot, window_end: 99 }).success
    ).toBe(false)
    expect(
      snapshotSchema.safeParse({ ...snapshot, unit: 'tokens' }).success
    ).toBe(false)
    expect(
      snapshotSchema.safeParse({
        ...snapshot,
        unit: 'tokens',
        calibrate: false,
      }).success
    ).toBe(true)
  })
})
