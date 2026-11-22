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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { toIntlLocale } from '@/i18n/languages'

import { poolAPI, type Pool, type Capabilities } from './api'
import { PoolDetail } from './pool-detail'
import { PoolEditor } from './pool-editor'

export function PoolPanel(props: { caps: Capabilities }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const query = useQuery({
    queryKey: ['subscription-pools', 'list'],
    queryFn: poolAPI.list,
  })
  const [selected, setSelected] = useState<Pool | 'new' | null>(null)
  const [search, setSearch] = useState('')
  if (selected) {
    return (
      <div className='h-full space-y-5 overflow-y-auto px-1'>
        <PoolEditor
          key={selected === 'new' ? 'new' : selected.id}
          pool={selected === 'new' ? undefined : selected}
          canManage={props.caps.manage}
          onClose={() => setSelected(null)}
        />
        {selected !== 'new' && (
          <PoolDetail
            pool={query.data?.find((p) => p.id === selected.id) ?? selected}
            caps={props.caps}
          />
        )}
      </div>
    )
  }
  return (
    <div className='flex h-full min-h-0 flex-col gap-4'>
      <div className='flex flex-wrap items-center gap-3'>
        <Input
          className='max-w-sm'
          aria-label={t('Search')}
          placeholder={t('Search')}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        {props.caps.manage && (
          <Button onClick={() => setSelected('new')}>
            {t('Create upstream pool')}
          </Button>
        )}
      </div>
      {query.isLoading && <p role='status'>{t('Loading...')}</p>}
      {query.isError && (
        <Button variant='outline' onClick={() => void query.refetch()}>
          {t('Retry')}
        </Button>
      )}
      <div className='min-h-0 flex-1 overflow-auto'>
        <StaticDataTable
          data={(query.data ?? []).filter((p) =>
            p.name.toLowerCase().includes(search.toLowerCase())
          )}
          getRowKey={(p) => p.id}
          emptyContent={t('No data')}
          columns={[
            {
              id: 'name',
              header: t('Name'),
              cell: (p) => (
                <Button variant='link' onClick={() => setSelected(p)}>
                  {p.name}
                </Button>
              ),
            },
            {
              id: 'cycles',
              header: t('Cycles'),
              cell: (p) =>
                p.cycles
                  .map(
                    (c) =>
                      `${c.name} · ${c.shared ? t('Shared') : t('Independent')}`
                  )
                  .join(' / '),
            },
            {
              id: 'end',
              header: t('Unified expiry'),
              cell: (p) => new Date(p.end_time * 1000).toLocaleString(locale),
            },
            {
              id: 'status',
              header: t('Status'),
              cell: (p) => (p.enabled ? t('Enabled') : t('Disabled')),
            },
          ]}
        />
      </div>
    </div>
  )
}
