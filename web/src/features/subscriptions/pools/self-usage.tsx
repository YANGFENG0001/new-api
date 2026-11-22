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
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import { poolAPI } from './api'
import { CycleProgress } from './cycle-progress'
import { SyncObservations } from './sync-observations'

export function SelfAlignedUsage() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const usage = useQuery({
    queryKey: ['subscription-pools', 'self-usage'],
    queryFn: poolAPI.usage,
    refetchInterval: 30000,
  })
  const pools = useQuery({
    queryKey: ['subscription-pools', 'self-overview'],
    queryFn: poolAPI.overview,
    refetchInterval: 30000,
  })
  return (
    <div className='space-y-4'>
      {usage.isError && <ErrorState onRetry={() => void usage.refetch()} />}
      {pools.isError && <ErrorState onRetry={() => void pools.refetch()} />}
      {(usage.data?.subscriptions ?? [])
        .filter((s) => s.managed && s.status === 'active')
        .map((s) => (
          <section
            key={s.subscription_id}
            className='space-y-3 rounded-lg border p-3'
          >
            <h4 className='text-sm font-medium'>
              {t('Subscription')} #{s.subscription_id}
              {s.pending && ` · ${t('Pending assignment')}`}
            </h4>
            <p className='text-muted-foreground text-xs'>
              {t('Expiry time')}:{' '}
              {new Date(s.end_time * 1000).toLocaleString(locale)}
            </p>
            <CycleProgress cycles={s.cycles} />
          </section>
        ))}
      {(pools.data ?? []).length > 0 && (
        <section className='space-y-3'>
          <h3 className='text-sm font-semibold'>
            {t('Upstream carpool subscriptions')}
          </h3>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Shown only for your active pool memberships. No other members or account secrets are displayed.'
            )}
          </p>
          {pools.data?.map((p) => (
            <div key={p.id} className='space-y-3 rounded-lg border p-3'>
              <h4 className='font-medium'>{p.name}</h4>
              <p className='text-muted-foreground text-xs'>
                {t('Unified expiry')}:{' '}
                {new Date(p.end_time * 1000).toLocaleString(locale)}
              </p>
              <CycleProgress cycles={p.cycles} />
              <SyncObservations observations={p.sync_observations ?? []} />
              {p.observations.map((o) => (
                <p key={o.key} className='text-muted-foreground text-xs'>
                  {t('Manual upstream observation')}:{' '}
                  {formatNumber(o.used, locale)}
                  {o.limit > 0
                    ? ` / ${formatNumber(o.limit, locale)}`
                    : ''}{' '}
                  {o.unit === 'quota' ? t('Quota units') : o.unit} ·{' '}
                  {new Date(o.sampled_at * 1000).toLocaleString(locale)}
                </p>
              ))}
            </div>
          ))}
        </section>
      )}
    </div>
  )
}
