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
import { t, type TFunction } from 'i18next'

import dayjs from '@/lib/dayjs'
import { formatQuota, formatNumber } from '@/lib/format'

import type { SubscriptionPlan, PlanRecord } from '../types'

export function formatPlanQuota(record: PlanRecord, t: TFunction): string {
  const allocation = record.allocation
  if (!allocation || allocation.mode === 'legacy') {
    return record.plan.total_amount > 0
      ? formatQuota(record.plan.total_amount)
      : t('Unlimited')
  }
  if (allocation.mode === 'assign') {
    return t('Administrator assignment required')
  }
  if (!allocation.cycles.length) {
    return allocation.available ? t('Unknown') : t('Unavailable')
  }
  return allocation.cycles
    .map((cycle) => {
      const quota = cycle.unlimited ? t('Unlimited') : formatQuota(cycle.limit)
      let duration = `${formatNumber(cycle.seconds)} ${t('seconds')}`
      if (cycle.seconds % 86400 === 0) {
        duration = `${formatNumber(cycle.seconds / 86400)} ${t('days')}`
      } else if (cycle.seconds % 3600 === 0) {
        duration = `${formatNumber(cycle.seconds / 3600)} ${t('hours')}`
      } else if (cycle.seconds % 60 === 0) {
        duration = `${formatNumber(cycle.seconds / 60)} ${t('minutes')}`
      }
      return `${cycle.name} · ${duration} · ${cycle.shared ? t('Shared') : t('Independent')} · ${quota}`
    })
    .join('\n')
}

export function isPlanFixedExpiryExpired(
  record: PlanRecord,
  nowSeconds = Math.floor(Date.now() / 1000)
): boolean {
  const fixedEnd = record.allocation?.fixed_end_time ?? 0
  return fixedEnd > 0 && fixedEnd !== 253402300799 && fixedEnd <= nowSeconds
}

export function formatPlanValidity(record: PlanRecord, t: TFunction): string {
  const allocation = record.allocation
  if (!allocation || allocation.mode === 'legacy') {
    return formatDuration(record.plan, t)
  }
  if (allocation.fixed_end_time === 253402300799) return t('Never')
  if (allocation.fixed_end_time > 0) {
    return formatTimestamp(allocation.fixed_end_time)
  }
  if (allocation.mode === 'fixed') return t('Follow pool expiry')
  return formatDuration(record.plan, t)
}

export function formatDuration(
  plan: Partial<SubscriptionPlan>,
  t: TFunction
): string {
  const unit = plan?.duration_unit || 'month'
  const value = plan?.duration_value || 1
  const unitLabels: Record<string, string> = {
    year: t('years'),
    month: t('months'),
    day: t('days'),
    hour: t('hours'),
    custom: t('Custom (seconds)'),
  }
  if (unit === 'custom') {
    const seconds = plan?.custom_seconds || 0
    if (seconds >= 86400) return `${Math.floor(seconds / 86400)} ${t('days')}`
    if (seconds >= 3600) return `${Math.floor(seconds / 3600)} ${t('hours')}`
    return `${seconds} ${t('seconds')}`
  }
  return `${value} ${unitLabels[unit] || unit}`
}

export function formatResetPeriod(
  plan: Partial<SubscriptionPlan>,
  t: TFunction
): string {
  const period = plan?.quota_reset_period || 'never'
  if (period === 'daily') return t('Daily')
  if (period === 'weekly') return t('Weekly')
  if (period === 'monthly') return t('Monthly')
  if (period === 'custom') {
    const seconds = Number(plan?.quota_reset_custom_seconds || 0)
    if (seconds >= 86400) return `${Math.floor(seconds / 86400)} ${t('days')}`
    if (seconds >= 3600) return `${Math.floor(seconds / 3600)} ${t('hours')}`
    if (seconds >= 60) return `${Math.floor(seconds / 60)} ${t('minutes')}`
    return `${seconds} ${t('seconds')}`
  }
  return t('No Reset')
}

export function formatTimestamp(ts: number): string {
  if (!ts) return '-'
  if (ts === 253402300799) return t('Never')
  return dayjs(ts * 1000).format('YYYY-MM-DD HH:mm:ss')
}
