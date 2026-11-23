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

import { poolAPI, type Pool, type Capabilities } from './api'
import { PoolDetail } from './pool-detail'
import { PoolEditor } from './pool-editor'
import { PoolSyncBinding } from './sync-binding'
import { SyncTimestamp } from './sync-usage'

export function PoolPanel(props: { caps: Capabilities }) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['subscription-pools', 'list'],
    queryFn: poolAPI.list,
  })
  const [selected, setSelected] = useState<Pool | 'new' | null>(null)
  const [tab, setTab] = useState('overview')
  const [search, setSearch] = useState('')
  function saved(pool: Pool) {
    setSelected(pool)
    setTab('overview')
  }
  if (selected) {
    if (selected === 'new') {
      return (
        <div className='h-full overflow-y-auto px-1'>
          <PoolEditor
            canManage={props.caps.manage}
            onClose={() => setSelected(null)}
            onSaved={props.caps.sync_read ? saved : undefined}
            canCreateFromSync={
              !!props.caps.sync_read &&
              !!props.caps.sync_binding &&
              !!props.caps.sync_apply
            }
            canRunSync={!!props.caps.sync_run}
          />
        </div>
      )
    }
    const cached = query.data?.find((p) => p.id === selected.id)
    const pool =
      cached && cached.version >= selected.version ? cached : selected
    let settings
    if (props.caps.manage) {
      settings = (
        <PoolEditor
          key={pool.id}
          pool={pool}
          embedded
          canManage={props.caps.manage}
          caps={props.caps}
          onClose={() => setSelected(null)}
          onSaved={(result) => setSelected(result)}
        />
      )
    } else if (props.caps.sync_read) {
      settings = <PoolSyncBinding pool={pool} caps={props.caps} />
    }
    return (
      <div className='h-full space-y-5 overflow-y-auto px-1'>
        <div className='flex flex-wrap items-center justify-between gap-3'>
          <h2 className='text-lg font-semibold break-words'>{pool.name}</h2>
          <Button variant='outline' onClick={() => setSelected(null)}>
            {t('Back')}
          </Button>
        </div>
        <PoolDetail
          key={pool.id}
          pool={pool}
          caps={props.caps}
          tab={tab}
          onTabChange={setTab}
          settings={settings}
        />
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
                <Button
                  variant='link'
                  onClick={() => {
                    setSelected(p)
                    setTab('overview')
                  }}
                >
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
              id: 'sync',
              header: t('Automatic synchronization'),
              cell: (p) =>
                props.caps.sync_read ? (
                  <Button
                    variant='outline'
                    onClick={() => {
                      setSelected(p)
                      setTab('settings')
                    }}
                  >
                    {t('Automatic synchronization')}
                  </Button>
                ) : (
                  '—'
                ),
            },
            {
              id: 'end',
              header: t('Unified expiry'),
              cell: (p) => (
                <SyncTimestamp
                  value={p.end_time === 253402300799 ? 0 : p.end_time}
                  fallback='Never'
                />
              ),
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
