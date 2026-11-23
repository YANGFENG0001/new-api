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
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { StatusBadge } from '@/components/status-badge'
import { toIntlLocale } from '@/i18n/languages'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatNumber } from '@/lib/format'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { poolAPI, type Usage, type Overview } from './api'
import { CycleProgress } from './cycle-progress'
import { SyncObservations } from './sync-observations'
import { SyncTimestamp } from './sync-usage'

export function SelfAlignedUsage(
  props: {
    subscription?: Usage
    subscriptions?: Usage[]
    title?: string
    planTitles?: ReadonlyMap<number, string>
  } = {}
) {
  const { t } = useTranslation()
  const usage = useQuery({
    queryKey: ['subscription-pools', 'self-usage'],
    queryFn: poolAPI.usage,
    enabled: !props.subscription && !props.subscriptions,
    staleTime: 10000,
    refetchInterval: 30000,
  })
  const pools = useQuery({
    queryKey: ['subscription-pools', 'self-overview'],
    queryFn: poolAPI.overview,
    enabled: !props.subscriptions || props.subscriptions.some((s) => s.managed),
    staleTime: 10000,
    refetchInterval: 30000,
  })
  const subscriptions = (
    props.subscription
      ? [props.subscription]
      : (props.subscriptions ?? usage.data?.subscriptions ?? [])
  ).filter((s) => s.managed)
  return (
    <div className='space-y-4'>
      {!props.subscription && !props.subscriptions && usage.isError && (
        <ErrorState onRetry={() => void usage.refetch()} />
      )}
      {pools.isError && <ErrorState onRetry={() => void pools.refetch()} />}
      {subscriptions.map((s) => {
        const pool = pools.data?.find((p) =>
          p.subscription_ids?.includes(s.subscription_id)
        )
        const firstMember =
          pool &&
          subscriptions.find((member) =>
            pool.subscription_ids?.includes(member.subscription_id)
          )
        const title = props.title || props.planTitles?.get(s.plan_id)
        // A shared cycle is already represented by its pool. Independent member
        // limits are different counters and must never disappear with that pool.
        const personalCycles = s.cycles.filter(
          (c) => !c.shared || !pool?.cycles.some((p) => p.key === c.key)
        )
        return (
          <section
            key={s.subscription_id}
            className='space-y-3 rounded-lg border p-3'
          >
            <div className='flex flex-wrap items-center gap-2'>
              <h4 className='text-sm font-medium'>
                {title && `${title} · `}
                {t('Subscription')} #{s.subscription_id}
              </h4>
              <StatusBadge
                copyable={false}
                variant={
                  s.status === 'active' && !s.pending ? 'success' : 'neutral'
                }
                label={t(
                  s.status === 'active' && s.pending
                    ? 'Pending assignment'
                    : ({
                        active: 'Active',
                        cancelled: 'Cancelled',
                        expired: 'Expired',
                      }[s.status] ?? 'Unavailable')
                )}
              />
            </div>
            <p className='text-muted-foreground text-xs'>
              {t('Expiry time')}:{' '}
              <SyncTimestamp
                value={s.end_time === 253402300799 ? 0 : s.end_time}
                fallback='Never'
              />
            </p>
            {s.pending && s.status === 'active' ? (
              <p className='text-muted-foreground text-sm'>
                {t(
                  'This subscription is unavailable until its pool assignment or availability is resolved.'
                )}
              </p>
            ) : (
              <CycleProgress cycles={personalCycles} />
            )}
            {pool && firstMember?.subscription_id === s.subscription_id && (
              <PoolUsage pool={pool} />
            )}
          </section>
        )
      })}
    </div>
  )
}

function PoolUsage(props: { pool: Overview }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const displayType = useSystemConfigStore(
    (state) => state.config.currency.quotaDisplayType
  )
  const pool = props.pool
  function formatObservationAmount(value: number, unit: string): string {
    if (unit === 'quota' && displayType !== 'TOKENS') {
      return formatQuotaWithCurrency(value, {
        locale,
        digitsLarge: 2,
        digitsSmall: 4,
        abbreviate: false,
      })
    }
    return formatNumber(value, locale)
  }
  return (
    <div className='space-y-3 border-t pt-3'>
      <h4 className='font-medium'>{pool.name}</h4>
      <p className='text-muted-foreground text-xs'>
        {t('Upstream total usage')} · {t('Unified expiry')}:{' '}
        <SyncTimestamp
          value={pool.end_time === 253402300799 ? 0 : pool.end_time}
          fallback='Never'
        />
      </p>
      <CycleProgress
        cycles={pool.cycles}
        observations={pool.sync_observations}
        preferObservations
      />
      <SyncObservations
        observations={(pool.sync_observations ?? []).filter(
          (o) => !pool.cycles.some((c) => c.key === o.key)
        )}
      />
      {pool.observations.map((o) => {
        const showUnitSuffix = o.unit !== 'quota' || displayType === 'TOKENS'
        return (
          <p key={o.key} className='text-muted-foreground text-xs'>
            {t('Manual upstream observation')}:{' '}
            {formatObservationAmount(o.used, o.unit)}
            {o.limit > 0
              ? ` / ${formatObservationAmount(o.limit, o.unit)}`
              : ''}
            {showUnitSuffix
              ? ` ${o.unit === 'quota' ? t('Quota units') : o.unit}`
              : ''}{' '}
            · {new Date(o.sampled_at * 1000).toLocaleString(locale)}
          </p>
        )
      })}
    </div>
  )
}
