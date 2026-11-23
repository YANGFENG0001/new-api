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
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Progress } from '@/components/ui/progress'
import { toIntlLocale } from '@/i18n/languages'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatNumber } from '@/lib/format'
import {
  useSystemConfigStore,
  type CurrencyDisplayType,
} from '@/stores/system-config-store'

import type { CycleView } from './api'
import { syncCycleName, type SyncObservation } from './sync-api'
import { SyncObservations } from './sync-observations'

function formatCycleQuota(
  value: number,
  displayType: CurrencyDisplayType,
  locale: Intl.LocalesArgument
): string {
  if (displayType === 'TOKENS') return formatNumber(value, locale)
  return formatQuotaWithCurrency(value, {
    locale,
    digitsLarge: 2,
    digitsSmall: 4,
    abbreviate: false,
  })
}

function formatCycleDuration(
  seconds: number,
  locale: Intl.LocalesArgument
): string {
  if (!Number.isFinite(seconds) || seconds <= 0) return '-'
  const units = [
    { unit: 'day' as const, seconds: 86400 },
    { unit: 'hour' as const, seconds: 3600 },
    { unit: 'minute' as const, seconds: 60 },
    { unit: 'second' as const, seconds: 1 },
  ]
  for (const unit of units) {
    if (seconds % unit.seconds !== 0) continue
    return new Intl.NumberFormat(locale, {
      style: 'unit',
      unit: unit.unit,
      unitDisplay: 'long',
    }).format(seconds / unit.seconds)
  }
  return new Intl.NumberFormat(locale, {
    style: 'unit',
    unit: 'second',
    unitDisplay: 'long',
  }).format(seconds)
}

export function CycleProgress(props: {
  cycles: CycleView[]
  observations?: SyncObservation[]
  preferObservations?: boolean
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const currency = useSystemConfigStore((state) => state.config.currency)
  const quotaUnitSuffix =
    currency.quotaDisplayType === 'TOKENS' ? ` ${t('Quota units')}` : ''

  return (
    <div className='grid gap-3 sm:grid-cols-2'>
      {props.cycles.map((cycle) => {
        const observation = props.observations?.find(
          (item) => item.key === cycle.key && item.utilization != null
        )
        const currentObservation =
          observation &&
          !observation.stale &&
          !observation.pending &&
          observation.window_end === cycle.next_reset &&
          observation.window_end > Date.now() / 1000 &&
          observation.sampled_at >= cycle.start_time &&
          (!observation.seconds || observation.seconds === cycle.seconds)
        if (props.preferObservations && currentObservation) {
          return (
            <SyncObservations
              key={cycle.key}
              observations={[observation]}
              scope={cycle.shared ? 'Shared' : 'Independent'}
            />
          )
        }
        const localEstimate =
          observation?.local_window_end === cycle.next_reset
            ? observation.local_estimated_utilization
            : null
        const localDelta =
          observation?.local_delta != null &&
          Number.isFinite(observation.local_delta)
            ? formatCycleQuota(
                observation.local_delta,
                currency.quotaDisplayType,
                locale
              )
            : null
        const used = formatCycleQuota(
          cycle.used + cycle.adjustment,
          currency.quotaDisplayType,
          locale
        )
        let capacity = formatCycleQuota(
          cycle.limit,
          currency.quotaDisplayType,
          locale
        )
        if (cycle.limit_unknown) capacity = t('Unknown')
        else if (cycle.unlimited) capacity = t('Unlimited')
        const reserved = formatCycleQuota(
          cycle.reserved,
          currency.quotaDisplayType,
          locale
        )
        const activeAmount =
          cycle.unlimited || cycle.limit_unknown
            ? capacity
            : `${capacity}${quotaUnitSuffix}`
        let pendingAmount = ''
        if (cycle.pending_rule) {
          pendingAmount = cycle.pending_rule.unlimited
            ? t('Unlimited')
            : `${formatCycleQuota(
                cycle.pending_rule.limit,
                currency.quotaDisplayType,
                locale
              )}${quotaUnitSuffix}`
        }
        return (
          <section
            key={cycle.key}
            aria-label={cycle.name}
            className='space-y-2 rounded-md border p-3 text-sm'
          >
            <div className='flex flex-wrap items-center gap-2'>
              <strong className='break-all'>
                {cycle.name === 'Subscription quota'
                  ? t('Subscription quota')
                  : syncCycleName(cycle.name, cycle.seconds)}
              </strong>
              <Badge variant='secondary'>
                {cycle.shared ? t('Shared') : t('Independent')}
              </Badge>
              {cycle.pending && (
                <Badge variant='outline'>{t('Changes pending')}</Badge>
              )}
            </div>
            <p>
              {t('Used')}: {used} / {capacity}
              {quotaUnitSuffix}
            </p>
            {cycle.pending_rule && (
              <div className='space-y-1 text-xs'>
                <p>
                  {t('Active terms: {{duration}} · {{amount}} · {{scope}}', {
                    duration: formatCycleDuration(cycle.seconds, locale),
                    amount: activeAmount,
                    scope: cycle.shared ? t('Shared') : t('Independent'),
                  })}
                </p>
                <p>
                  {t('Next cycle: {{duration}} · {{amount}} · {{scope}}', {
                    duration: formatCycleDuration(
                      cycle.pending_rule.seconds,
                      locale
                    ),
                    amount: pendingAmount,
                    scope: cycle.pending_rule.shared
                      ? t('Shared')
                      : t('Independent'),
                  })}
                </p>
              </div>
            )}
            {localEstimate != null &&
              Number.isFinite(localEstimate) &&
              localEstimate >= 0 && (
                <div className='space-y-2'>
                  <p>
                    <strong>
                      {formatNumber(localEstimate * 100, locale)}%
                    </strong>{' '}
                    · <span>{t('Local estimate for current cycle')}</span>
                    {localDelta != null && (
                      <>
                        {' '}
                        ·{' '}
                        <span>
                          {t('Local delta: {{amount}}', {
                            amount: `${localDelta}${quotaUnitSuffix}`,
                          })}
                        </span>
                      </>
                    )}
                  </p>
                  <Progress
                    value={Math.min(100, localEstimate * 100)}
                    aria-label={t('Local estimate for current cycle')}
                  />
                </div>
              )}
            {!cycle.unlimited && cycle.limit > 0 && (
              <Progress
                value={Math.min(
                  100,
                  ((cycle.used + cycle.adjustment + cycle.reserved) /
                    cycle.limit) *
                    100
                )}
                aria-label={cycle.name}
              />
            )}
            {cycle.reserved > 0 && (
              <p>
                {t('Reserved')}: {reserved}
                {quotaUnitSuffix}
              </p>
            )}
            <p className='text-muted-foreground text-xs'>
              {t('Next reset time')}:{' '}
              {cycle.next_reset
                ? new Date(cycle.next_reset * 1000).toLocaleString(locale)
                : t('Never')}
            </p>
            <p className='text-muted-foreground text-xs'>
              {cycle.adjustment
                ? t('Manual calibration plus local usage')
                : t('Local usage statistics')}
            </p>
            {props.observations?.some((item) => item.key === cycle.key) && (
              <SyncObservations
                historical
                observations={props.observations.filter(
                  (item) => item.key === cycle.key
                )}
              />
            )}
          </section>
        )
      })}
    </div>
  )
}
