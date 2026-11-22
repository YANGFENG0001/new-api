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

import { StaticDataTable } from '@/components/data-table'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import type { SyncChange } from './sync-api'

export function SyncChangeTable(props: { changes: SyncChange[] }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  return (
    <StaticDataTable
      data={props.changes}
      getRowKey={(c, i) => `${c.pool_id}-${c.key}-${i}`}
      emptyContent={t('No changes')}
      columns={[
        {
          id: 'pool',
          header: t('Upstream carpool pools'),
          cell: (c) => `#${c.pool_id} · ${c.key || '—'}`,
        },
        {
          id: 'result',
          header: t('Status'),
          cell: (c) => (
            <div className='space-y-1'>
              <p>{t(c.message)}</p>
              {c.before > 0 && c.after > 0 && (
                <p>
                  {new Date(c.before * 1000).toLocaleString(locale)} →{' '}
                  {new Date(c.after * 1000).toLocaleString(locale)}
                </p>
              )}
              {c.utilization != null && (
                <p>
                  {c.previous_utilization == null
                    ? t('Unknown')
                    : `${formatNumber(c.previous_utilization * 100, locale)}%`}{' '}
                  → {formatNumber(c.utilization * 100, locale)}%
                </p>
              )}
            </div>
          ),
        },
      ]}
    />
  )
}
