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
import { Button } from '@/components/ui/button'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import type { Capabilities } from './api'
import { syncAPI, type SyncSource } from './sync-api'
import { SyncChangeTable } from './sync-change-table'
import { SyncSourceEditor } from './sync-source-editor'

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
  const state = useQuery({
    queryKey: ['subscription-sync', 'state', active],
    queryFn: () => syncAPI.state(active),
    enabled: active > 0,
  })
  const run = useMutation({
    mutationFn: (preview: boolean) => syncAPI.run(active, preview),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ['subscription-sync'] })
      void client.invalidateQueries({ queryKey: ['subscription-pools'] })
      setConfirm(false)
    },
  })
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
            cell: (s) => (s.enabled ? t('Enabled') : t('Disabled')),
          },
          {
            id: 'fetch',
            header: t('Last fetch'),
            cell: (s) =>
              s.last_fetch
                ? new Date(s.last_fetch * 1000).toLocaleString(locale)
                : t('Never'),
          },
          {
            id: 'observation',
            header: t('Observation time'),
            cell: (s) =>
              s.observed_at
                ? new Date(s.observed_at * 1000).toLocaleString(locale)
                : t('Unknown'),
          },
          {
            id: 'error',
            header: t('Status'),
            cell: (s) =>
              t(
                s.last_error ||
                  s.observation_status ||
                  'No upstream observation'
              ),
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
        <section className='space-y-4 rounded-lg border p-4'>
          <h3>{list.data.sources.find((s) => s.id === active)?.name}</h3>
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
          {state.isError && <ErrorState onRetry={() => void state.refetch()} />}
          <StaticDataTable
            data={run.data?.accounts ?? state.data?.accounts ?? []}
            getRowKey={(a) => a.id}
            emptyContent={t('No data')}
            columns={[
              {
                id: 'account',
                header: t('Account'),
                cell: (a) => a.name || a.id,
              },
              {
                id: 'cycles',
                header: t('Cycles'),
                cell: (a) => (
                  <div className='space-y-1'>
                    {a.cycles.map((c) => (
                      <p key={c.key} className='break-words'>
                        {c.name}:{' '}
                        {c.utilization == null
                          ? t('Unknown')
                          : `${formatNumber(c.utilization * 100, locale)}%`}{' '}
                        {c.error && t(c.error)}
                      </p>
                    ))}
                  </div>
                ),
              },
            ]}
          />
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
