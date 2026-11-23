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

export function scaleCycleQuota(limit: number, multiplier: number): number {
  if (limit <= 0 || !Number.isFinite(multiplier) || multiplier <= 0) {
    return limit
  }
  if (multiplier === 1) return limit
  return Math.max(
    1,
    Math.min(Number.MAX_SAFE_INTEGER, Math.round(limit * multiplier))
  )
}

export function formatMultiplier(multiplier?: number): string {
  const value = multiplier && multiplier > 0 ? multiplier : 1
  return `${Number(value.toFixed(2))}×`
}

export function defaultOverrideLimit(...candidates: number[]): number {
  for (const candidate of candidates) {
    if (Number.isFinite(candidate) && candidate > 0) {
      return candidate
    }
  }
  return 1000000
}
