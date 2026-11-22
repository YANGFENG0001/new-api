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
import { formatNumber } from '@/lib/format'

import type { CycleView } from './api'

export function CycleProgress(props: { cycles: CycleView[] }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  return (
    <div className='grid gap-3 sm:grid-cols-2'>
      {props.cycles.map((c) => {
        let capacity = formatNumber(c.limit, locale)
        if (c.limit_unknown) capacity = t('Unknown')
        else if (c.unlimited) capacity = t('Unlimited')
        return (
          <div key={c.key} className='space-y-2 rounded-md border p-3 text-sm'>
            <div className='flex flex-wrap items-center gap-2'>
              <strong className='break-all'>
                {c.name === 'Subscription quota'
                  ? t('Subscription quota')
                  : c.name}
              </strong>
              <Badge variant='secondary'>
                {c.shared ? t('Shared') : t('Independent')}
              </Badge>
              {c.pending && (
                <Badge variant='outline'>{t('Changes pending')}</Badge>
              )}
            </div>
            <p>
              {t('Used')}: {formatNumber(c.used + c.adjustment, locale)} /{' '}
              {capacity} {t('Quota units')}
            </p>
            {!c.unlimited && c.limit > 0 && (
              <Progress
                value={Math.min(
                  100,
                  ((c.used + c.adjustment + c.reserved) / c.limit) * 100
                )}
                aria-label={c.name}
              />
            )}
            {c.reserved > 0 && (
              <p>
                {t('Reserved')}: {formatNumber(c.reserved, locale)}
              </p>
            )}
            <p className='text-muted-foreground text-xs'>
              {t('Next reset time')}:{' '}
              {c.next_reset
                ? new Date(c.next_reset * 1000).toLocaleString(locale)
                : t('Never')}
            </p>
            <p className='text-muted-foreground text-xs'>
              {c.adjustment
                ? t('Manual calibration plus local usage')
                : t('Local usage statistics')}
            </p>
          </div>
        )
      })}
    </div>
  )
}
