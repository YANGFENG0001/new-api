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
import { useEffect, useState } from 'react'

import { useAuthStore } from '@/stores/auth-store'
import type { CurrencyDisplayType } from '@/stores/system-config-store'

export type QuotaInputUnit = 'quota' | 'USD' | 'CNY'

const INPUT_UNITS: ReadonlySet<QuotaInputUnit> = new Set([
  'quota',
  'USD',
  'CNY',
])
const STORAGE_PREFIX = 'new-api:subscription-quota-input-unit:v1'

export function defaultQuotaInputUnit(
  displayType: CurrencyDisplayType
): QuotaInputUnit {
  if (displayType === 'USD' || displayType === 'CNY') return displayType
  return 'quota'
}

export function quotaInputUnitStorageKey(
  adminId: number | null | undefined,
  fieldId: string
): string | null {
  if (!adminId || !Number.isSafeInteger(adminId) || adminId < 1) return null
  return `${STORAGE_PREFIX}:${adminId}:${encodeURIComponent(fieldId)}`
}

function readStoredUnit(
  storageKey: string | null,
  fallback: QuotaInputUnit
): QuotaInputUnit {
  if (!storageKey || typeof window === 'undefined') return fallback
  try {
    const stored = window.localStorage.getItem(storageKey)
    return stored && INPUT_UNITS.has(stored as QuotaInputUnit)
      ? (stored as QuotaInputUnit)
      : fallback
  } catch {
    return fallback
  }
}

export function useQuotaInputUnit(
  fieldKey: string,
  displayType: CurrencyDisplayType
): {
  unit: QuotaInputUnit
  setUnit: (unit: QuotaInputUnit) => void
} {
  const adminId = useAuthStore((state) => state.auth.user?.id)
  const fallback = defaultQuotaInputUnit(displayType)
  const storageKey = quotaInputUnitStorageKey(adminId, fieldKey)
  const [unit, setUnitState] = useState(() =>
    readStoredUnit(storageKey, fallback)
  )

  useEffect(() => {
    setUnitState(readStoredUnit(storageKey, fallback))
  }, [storageKey, fallback])

  const setUnit = (nextUnit: QuotaInputUnit): void => {
    setUnitState(nextUnit)
    if (!storageKey) return
    try {
      window.localStorage.setItem(storageKey, nextUnit)
    } catch {
      // Unit selection remains usable when storage is blocked or full.
    }
  }

  return { unit, setUnit }
}
