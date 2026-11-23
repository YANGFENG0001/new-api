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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { StaticDataTable } from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { toIntlLocale } from '@/i18n/languages'

import type { Capabilities } from './api'
import { syncAPI, syncStaleSeconds, type SyncSource } from './sync-api'
import { SyncChangeTable } from './sync-change-table'
import { SyncSourceEditor } from './sync-source-editor'
import { SyncAccounts, SyncSourceStatus, SyncTimestamp } from './sync-usage'

export function SyncSourcesPanel(props: { caps: Capabilities }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const client = useQueryClient()
  const list = useQuery({
    queryKey: ['subscription-sync', 'sources'],
    queryFn: syncAPI.sources,
    refetchInterval: 30000,
  })
  const [selected, setSelected] = useState<SyncSource | 'new' | null>(null)
  const [active, setActive] = useState(0)
  const [expandedEvent, setExpandedEvent] = useState(0)
  const [confirm, setConfirm] = useState(false)
  const activeSource = list.data?.sources.find((s) => s.id === active)
  const state = useQuery({
    queryKey: ['subscription-sync', 'state', active],
    queryFn: () => syncAPI.state(active),
    enabled: active > 0,
    refetchInterval: 30000,
  })
  const run = useMutation({
    mutationFn: (preview: boolean) => syncAPI.run(active, preview),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ['subscription-sync'] })
      void client.invalidateQueries({ queryKey: ['subscription-pools'] })
      setConfirm(false)
    },
  })
  const showPreview =
    !!run.data?.preview &&
    !(
      (activeSource?.last_fetch ?? 0) * 1000 >= run.submittedAt &&
      state.dataUpdatedAt >= run.submittedAt
    )
  if (list.isLoading) return <LoadingState />
  if (list.isError) return <ErrorState onRetry={() => void list.refetch()} />
  if (!list.data) return null
  if (selected) {
    return (
      <div className='h-full min-h-0 overflow-y-auto px-1 pb-6'>
        <SyncSourceEditor
          key={selected === 'new' ? 'new' : selected.id}
          source={selected === 'new' ? undefined : selected}
          template={list.data.template}
          privileged={list.data.privileged}
          canEdit={!!props.caps.sync_credentials}
          onClose={() => setSelected(null)}
        />
      </div>
    )
  }
  return (
    <div className='h-full min-h-0 space-y-4 overflow-y-auto px-1 pb-6'>
      <div className='flex flex-wrap gap-2'>
        {props.caps.sync_credentials && (
          <Button onClick={() => setSelected('new')}>
            {t('Add synchronization source')}
          </Button>
        )}
      </div>
      <StaticDataTable
        data={list.data.sources}
        getRowKey={(s) => s.id}
        emptyContent={t('No data')}
        columns={[
          {
            id: 'name',
            header: t('Name'),
            cell: (s) => (
              <Button
                variant='link'
                onClick={() => {
                  setActive(s.id)
                  run.reset()
                }}
              >
                {s.name}
              </Button>
            ),
          },
          {
            id: 'status',
            header: t('Automatic synchronization'),
            cell: (s) => (
              <StatusBadge
                copyable={false}
                variant={s.enabled ? 'success' : 'neutral'}
                label={s.enabled ? t('Enabled') : t('Disabled')}
              />
            ),
          },
          {
            id: 'fetch',
            header: t('Last fetch'),
            cell: (s) => (
              <SyncTimestamp value={s.last_fetch} fallback='Never' relative />
            ),
          },
          {
            id: 'observation',
            header: t('Observation time'),
            cell: (s) => (
              <SyncTimestamp
                value={s.observed_at}
                fallback='Not reported'
                relative
              />
            ),
          },
          {
            id: 'error',
            header: t('Status'),
            cell: (s) => <SyncSourceStatus source={s} />,
          },
          {
            id: 'edit',
            header: t('Actions'),
            cell: (s) => (
              <Button variant='outline' onClick={() => setSelected(s)}>
                {props.caps.sync_credentials ? t('Edit') : t('View')}
              </Button>
            ),
          },
        ]}
      />
      {active > 0 && (
        <section className='min-w-0 space-y-4 rounded-xl border p-4'>
          <div className='flex flex-wrap items-center justify-between gap-3'>
            <h3 className='text-base font-semibold break-words'>
              {list.data.sources.find((s) => s.id === active)?.name}
            </h3>
            {showPreview && (
              <div className='flex flex-wrap items-center gap-2'>
                <StatusBadge
                  copyable={false}
                  variant='info'
                  label={t('Preview data')}
                />
                <Button variant='ghost' onClick={() => run.reset()}>
                  {t('Show saved observations')}
                </Button>
              </div>
            )}
          </div>
          <dl className='bg-muted/40 grid gap-4 rounded-lg p-3 text-xs sm:grid-cols-3'>
            <div className='space-y-1'>
              <dt className='text-muted-foreground'>{t('Last fetch')}</dt>
              <dd>
                <SyncTimestamp
                  value={activeSource?.last_fetch}
                  fallback='Never'
                  relative
                />
              </dd>
            </div>
            <div className='space-y-1'>
              <dt className='text-muted-foreground'>{t('Observation time')}</dt>
              <dd>
                <SyncTimestamp
                  value={activeSource?.observed_at}
                  fallback='Not reported'
                  relative
                />
              </dd>
            </div>
            <div className='space-y-1'>
              <dt className='text-muted-foreground'>
                {t('Next synchronization')}
              </dt>
              <dd>
                {activeSource?.enabled ? (
                  <SyncTimestamp
                    value={activeSource.next_run}
                    fallback='Awaiting observation'
                    relative
                  />
                ) : (
                  t('Disabled')
                )}
              </dd>
            </div>
          </dl>
          <div className='flex flex-wrap gap-2'>
            {props.caps.sync_run && (
              <Button
                variant='outline'
                disabled={run.isPending}
                onClick={() => run.mutate(true)}
              >
                {t('Test read and preview')}
              </Button>
            )}
            {props.caps.sync_run && (
              <Button
                disabled={run.isPending || !run.data?.preview}
                onClick={() => setConfirm(true)}
              >
                {t('Synchronize now')}
              </Button>
            )}
          </div>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Preview reads the configured interface without changing quotas, reset times or saved observations.'
            )}
          </p>
          {run.isPending && <LoadingState inline message={t('Loading...')} />}
          {run.isError && (
            <ErrorState onRetry={() => run.mutate(run.variables ?? true)} />
          )}
          {state.isError && <ErrorState onRetry={() => void state.refetch()} />}
          {state.isLoading && <LoadingState inline />}
          {!state.isLoading && (
            <SyncAccounts
              accounts={
                showPreview || state.dataUpdatedAt < run.submittedAt
                  ? (run.data?.accounts ?? state.data?.accounts ?? [])
                  : (state.data?.accounts ?? [])
              }
              staleSeconds={syncStaleSeconds(activeSource)}
            />
          )}
          {run.data && <SyncChangeTable changes={run.data.changes} />}
          <details>
            <summary>{t('Synchronization history')}</summary>
            <StaticDataTable
              data={state.data?.events ?? []}
              getRowKey={(e) => e.id}
              emptyContent={t('No data')}
              columns={[
                {
                  id: 'time',
                  header: t('Time'),
                  cell: (e) =>
                    new Date(e.created_at * 1000).toLocaleString(locale),
                },
                {
                  id: 'operation',
                  header: t('Actions'),
                  cell: (e) => {
                    if (e.action === 'auth_check') {
                      return t('Check authentication')
                    }
                    if (e.action === 'auth_refresh') return t('Renew token now')
                    if (e.action === 'auth_replace') return t('Replace token')
                    return t('Synchronization history')
                  },
                },
                {
                  id: 'status',
                  header: t('Status'),
                  cell: (e) =>
                    e.message ? t(e.message) : t('Saved successfully'),
                },
                {
                  id: 'details',
                  header: t('Actions'),
                  cell: (e) =>
                    (e.changes?.length ?? 0) > 0 ? (
                      <Button
                        variant='outline'
                        onClick={() => setExpandedEvent(e.id)}
                      >
                        {t('View')}
                      </Button>
                    ) : null,
                },
              ]}
            />
            {expandedEvent > 0 && (
              <SyncChangeTable
                changes={
                  state.data?.events.find((e) => e.id === expandedEvent)
                    ?.changes ?? []
                }
              />
            )}
          </details>
        </section>
      )}
      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        title={t('Synchronize now')}
        desc={t(
          'Apply the approved bindings to a new upstream read. Local billing records are not overwritten.'
        )}
        isLoading={run.isPending}
        handleConfirm={() => run.mutate(false)}
      />
    </div>
  )
}
