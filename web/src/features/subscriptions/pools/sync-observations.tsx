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

import { Progress } from '@/components/ui/progress'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import type { SyncObservation } from './sync-api'

export function SyncObservations(props: { observations: SyncObservation[] }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  return (
    <div className='space-y-3'>
      {props.observations.map((o) => (
        <section
          key={o.key}
          className='space-y-2 rounded-lg border p-3'
          aria-label={o.name}
        >
          <div className='flex flex-wrap justify-between gap-2'>
            <span>
              {t('Upstream observation')} · {o.name}
            </span>
            <span>
              {o.utilization == null
                ? t('Unknown')
                : `${formatNumber(o.utilization * 100, locale)}%`}
            </span>
          </div>
          <Progress
            aria-label={o.name}
            value={
              o.utilization == null ? null : Math.min(100, o.utilization * 100)
            }
          />
          <p className='text-muted-foreground text-xs'>
            {t('Observation time')}:{' '}
            {new Date(o.sampled_at * 1000).toLocaleString(locale)} ·{' '}
            {t('Next reset time')}:{' '}
            {new Date(o.window_end * 1000).toLocaleString(locale)}
          </p>
          {o.stale && (
            <p role='status' className='text-destructive text-xs'>
              {t('Upstream observation is stale')}
            </p>
          )}
          {o.pending && (
            <p role='status' className='text-destructive text-xs'>
              {t(o.pending)}
            </p>
          )}
        </section>
      ))}
    </div>
  )
}
