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
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'

import type { CycleRule, PoolSyncCreation } from './api'
import { SyncAccountPicker } from './sync-account-picker'
import { syncAPI, syncCycleName } from './sync-api'
import { SyncSourceStatus } from './sync-usage'

export type PoolSyncDraft = {
  sync: PoolSyncCreation
  name: string
  end_time: number
  cycles: CycleRule[]
}

// Reads the saved catalog; only the explicit refresh button contacts the source.
export function PoolSyncImport(props: {
  onImport: (draft: PoolSyncDraft) => void
  onClear: () => void
  canRun: boolean
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [sourceId, setSourceId] = useState(0)
  const [accountId, setAccountId] = useState('')
  const [confirmRefresh, setConfirmRefresh] = useState(false)
  const sources = useQuery({
    queryKey: ['subscription-sync', 'sources'],
    queryFn: syncAPI.sources,
  })
  const source = sources.data?.sources.find((s) => s.id === sourceId)
  const state = useQuery({
    queryKey: ['subscription-sync', 'state', sourceId],
    queryFn: () => syncAPI.state(sourceId),
    enabled: sourceId > 0,
  })
  const read = useMutation({
    mutationFn: () => syncAPI.run(sourceId, false),
    onSuccess: (result) => {
      setConfirmRefresh(false)
      client.setQueryData(['subscription-sync', 'state', sourceId], {
        accounts: result.accounts,
        events: [],
      })
      void client.invalidateQueries({ queryKey: ['subscription-sync'] })
      void client.invalidateQueries({ queryKey: ['subscription-pools'] })
    },
  })
  if (sources.isLoading) return <LoadingState />
  if (sources.isError) {
    return <ErrorState onRetry={() => void sources.refetch()} />
  }
  return (
    <section
      className='space-y-4 rounded-lg border p-4'
      aria-label={t('Create from upstream')}
    >
      <Alert>
        <AlertTitle>{t('Create from upstream')}</AlertTitle>
        <AlertDescription>
          {t(
            'Select a source and account to fill cycles, reset times and expiry. You can edit the imported settings before saving.'
          )}
        </AlertDescription>
      </Alert>
      <SyncAccountPicker
        id='pool-sync'
        sources={sources.data?.sources ?? []}
        accounts={state.data?.accounts ?? []}
        sourceId={sourceId}
        accountId={accountId}
        accountDisabled={state.isPending}
        onSourceChange={(id) => {
          setSourceId(id)
          setAccountId('')
          props.onClear()
          read.reset()
        }}
        onAccountChange={(v) => {
          const account = state.data?.accounts.find((a) => a.id === v)
          if (!account || !source) return
          setAccountId(account.id)
          const now = Date.now() / 1000
          props.onImport({
            sync: {
              source_id: source.id,
              source_version: source.version,
              account_id: account.id,
              sync_expiry: true,
            },
            name: `${source.name} · ${account.name || account.id}`.slice(
              0,
              128
            ),
            end_time: account.expiry > 0 ? account.expiry : 253402300799,
            cycles: account.cycles
              .filter(
                (c) =>
                  !c.error &&
                  c.utilization != null &&
                  c.sampled_at > 0 &&
                  c.sampled_at <= now + 60 &&
                  c.window_start > 0 &&
                  c.window_end > now
              )
              .map((c) => ({
                key: c.key,
                name: syncCycleName(c.name || c.key, c.seconds),
                seconds: c.seconds,
                next_reset: c.window_end,
                limit: 0,
                unlimited: true,
                shared: false,
                enabled: true,
                upstream_limit: 0,
                enforce_upstream: false,
              })),
          })
        }}
      />
      {source && <SyncSourceStatus source={source} />}
      {source && !source.enabled && (
        <p role='note' className='text-warning text-sm'>
          {t(
            'Enable automatic synchronization in the source settings to refresh this binding on schedule.'
          )}
        </p>
      )}
      {props.canRun && (
        <Button
          type='button'
          variant='outline'
          disabled={!source || read.isPending}
          onClick={() => setConfirmRefresh(true)}
        >
          {t('Refresh saved upstream data')}
        </Button>
      )}
      {state.isError && <ErrorState onRetry={() => void state.refetch()} />}
      {read.isError && <ErrorState onRetry={() => setConfirmRefresh(true)} />}
      <ConfirmDialog
        open={confirmRefresh}
        onOpenChange={setConfirmRefresh}
        title={t('Refresh saved upstream data')}
        desc={t(
          'Fetch and calibrate all pools linked to this source. Recorded local usage is preserved.'
        )}
        isLoading={read.isPending}
        handleConfirm={() => read.mutate()}
      />
      <p className='text-muted-foreground text-xs'>
        {t(
          'Unreported cycles are not imported. Upstream percentages do not define local quota capacity; local limits remain editable.'
        )}
      </p>
    </section>
  )
}
