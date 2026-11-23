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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState, type ReactNode } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { StatusBadge } from '@/components/status-badge'
import {
  Accordion,
  AccordionItem,
  AccordionTrigger,
  AccordionContent,
} from '@/components/ui/accordion'
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

import type { Pool, Capabilities } from './api'
import { QuotaAmountInput } from './quota-amount-input'
import { SyncAccountPicker } from './sync-account-picker'
import {
  syncAPI,
  syncStaleSeconds,
  syncCycleName,
  syncBindingSchema,
  type SyncBinding,
  type SyncSource,
  type SyncObservation,
} from './sync-api'
import { SyncObservations } from './sync-observations'
import { SyncAccounts, SyncSourceStatus } from './sync-usage'

export function PoolSyncBinding(props: {
  pool: Pool
  caps: Capabilities
  onDraftChange?: (value: SyncBinding) => void
  renderLocalCycle?: (key: string, followsReset: boolean) => ReactNode
  onPoolAligned?: () => Promise<void>
}) {
  const source = useQuery({
    queryKey: ['subscription-sync', 'sources'],
    queryFn: syncAPI.sources,
    refetchInterval: 30000,
  })
  const binding = useQuery({
    queryKey: ['subscription-sync', 'binding', props.pool.id],
    queryFn: () => syncAPI.binding(props.pool.id),
    refetchInterval: 30000,
  })
  if (source.isLoading || binding.isLoading) return <LoadingState />
  if (source.isError || binding.isError) {
    return (
      <ErrorState
        onRetry={() => {
          void source.refetch()
          void binding.refetch()
        }}
      />
    )
  }
  if (!binding.data || !source.data) return null
  return (
    <BindingEditor
      key={
        props.onDraftChange
          ? props.pool.id
          : `${props.pool.id}-${binding.data.binding.version}`
      }
      pool={props.pool}
      caps={props.caps}
      sources={source.data.sources}
      initial={binding.data.binding}
      observations={binding.data.observations}
      onDraftChange={props.onDraftChange}
      renderLocalCycle={props.renderLocalCycle}
      onPoolAligned={props.onPoolAligned}
    />
  )
}

function BindingEditor(props: {
  pool: Pool
  caps: Capabilities
  sources: SyncSource[]
  initial: SyncBinding
  observations: SyncObservation[]
  onDraftChange?: (value: SyncBinding) => void
  renderLocalCycle?: (key: string, followsReset: boolean) => ReactNode
  onPoolAligned?: () => Promise<void>
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const form = useForm<SyncBinding>({
    resolver: zodResolver(syncBindingSchema),
    defaultValues: {
      ...props.initial,
      stale_policy: props.initial.stale_policy || 'stop',
    },
  })
  const values = form.watch()
  const draftJSON = JSON.stringify(values)
  const onDraftChange = props.onDraftChange
  useEffect(() => {
    onDraftChange?.(JSON.parse(draftJSON) as SyncBinding)
  }, [draftJSON, onDraftChange])
  const Container = props.onDraftChange ? 'div' : 'form'
  const source = props.sources.find((s) => s.id === values.source_id)
  const state = useQuery({
    queryKey: ['subscription-sync', 'state', values.source_id],
    queryFn: () => syncAPI.state(values.source_id),
    enabled: values.source_id > 0,
    refetchInterval: 30000,
  })
  const read = useMutation({
    mutationFn: () => syncAPI.run(values.source_id, true),
  })
  const [confirm, setConfirm] = useState<SyncBinding | null>(null)
  const [disconnect, setDisconnect] = useState(false)
  const unbind = useMutation({
    mutationFn: () => syncAPI.unbind(props.pool.id, values.version),
    onSuccess: async () => {
      setDisconnect(false)
      const fresh = await syncAPI.binding(props.pool.id)
      form.reset(fresh.binding)
      await client.invalidateQueries({ queryKey: ['subscription-sync'] })
    },
  })
  const [reset, setReset] = useState<SyncObservation | null>(null)
  const save = useMutation({
    mutationFn: syncAPI.saveBinding,
    onSuccess: () => {
      setConfirm(null)
      void client.invalidateQueries({ queryKey: ['subscription-sync'] })
    },
  })
  const align = useMutation({
    mutationFn: (o: SyncObservation) =>
      syncAPI.confirmReset(
        props.pool.id,
        o.key,
        o.digest ?? '',
        props.pool.version
      ),
    onSuccess: async () => {
      setReset(null)
      await Promise.all([
        client.invalidateQueries({ queryKey: ['subscription-sync'] }),
        client.invalidateQueries({ queryKey: ['subscription-pools'] }),
      ])
      await props.onPoolAligned?.()
    },
  })
  // A local cache read is not a new upstream fetch. Keep the preview until a
  // later scheduled/manual fetch has actually replaced the saved catalog.
  const savedAfterRead =
    (source?.last_fetch ?? 0) * 1000 >= read.submittedAt &&
    state.dataUpdatedAt >= read.submittedAt
  const accounts =
    read.data && !savedAfterRead
      ? read.data.accounts
      : (state.data?.accounts ?? [])
  function updateCycle(
    key: string,
    patch: Partial<SyncBinding['cycles'][number]>
  ) {
    const old = values.cycles.find((c) => c.local_key === key) ?? {
      local_key: key,
      upstream_key: '',
      align_reset: false,
      enforce: false,
      threshold: 1,
      max_shift_seconds: 300,
    }
    const next = { ...old, ...patch }
    const upstreamSeconds = source?.mapping.cycles.find(
      (c) => c.key === next.upstream_key
    )?.seconds
    if (upstreamSeconds != null) {
      next.max_shift_seconds = Math.min(next.max_shift_seconds, upstreamSeconds)
    }
    const rest = values.cycles.filter((c) => c.local_key !== key)
    form.setValue('cycles', next.upstream_key ? [...rest, next] : rest, {
      shouldDirty: true,
      shouldValidate: true,
    })
  }
  return (
    <div className='min-w-0 space-y-4'>
      <Alert>
        <AlertTitle>{t('Automatic synchronization')}</AlertTitle>
        <AlertDescription>
          {t(
            'Bind an actual upstream account, not a subscription plan. Independent and shared cycle settings remain unchanged.'
          )}
        </AlertDescription>
      </Alert>
      {source && (
        <div className='flex flex-wrap items-center gap-3 rounded-lg border p-3 text-sm'>
          <span className='font-medium'>{source.name}</span>
          <StatusBadge
            copyable={false}
            label={
              source.enabled
                ? t('Automatic synchronization enabled')
                : t('Automatic synchronization disabled')
            }
            variant={source.enabled ? 'success' : 'neutral'}
          />
          <SyncSourceStatus source={source} />
          {!source.enabled && (
            <p className='text-muted-foreground w-full text-xs'>
              {t(
                'Enable automatic synchronization in the source settings to refresh this binding on schedule.'
              )}
            </p>
          )}
        </div>
      )}
      <Container
        className='min-w-0 space-y-4'
        onSubmit={form.handleSubmit((data) => setConfirm(data))}
      >
        <fieldset disabled={save.isPending} className='space-y-4'>
          <SyncAccountPicker
            id='binding'
            sources={props.sources}
            accounts={accounts}
            sourceId={values.source_id}
            accountId={values.account_id}
            sourceDisabled={!props.caps.sync_binding || !props.caps.sync_apply}
            accountDisabled={
              !props.caps.sync_binding ||
              !props.caps.sync_apply ||
              state.isPending
            }
            onSourceChange={(id) => {
              form.setValue('source_id', id)
              form.setValue('account_id', '')
              form.setValue('cycles', [])
              read.reset()
            }}
            onAccountChange={(id) =>
              form.setValue('account_id', id, { shouldDirty: true })
            }
          />
          {props.caps.sync_run && (
            <Button
              type='button'
              variant='outline'
              disabled={!source || read.isPending}
              onClick={() => read.mutate()}
            >
              {t('Read upstream accounts')}
            </Button>
          )}
          <Label>
            <Switch
              disabled={!props.caps.sync_binding || !props.caps.sync_apply}
              checked={values.enabled}
              onCheckedChange={(v) => form.setValue('enabled', v)}
            />
            {t('Enable this binding')}
          </Label>
          <Label>
            <Switch
              disabled={!props.caps.sync_binding || !props.caps.sync_apply}
              checked={values.sync_expiry}
              onCheckedChange={(v) => form.setValue('sync_expiry', v)}
            />
            {t('Follow upstream expiry')}
          </Label>
          <div className='space-y-3'>
            {props.pool.cycles.map((local) => {
              const mapping = values.cycles.find(
                (c) => c.local_key === local.key
              )
              return (
                <section
                  key={local.key}
                  className='grid min-w-0 gap-5 rounded-xl border p-4 lg:grid-cols-2'
                  aria-label={local.name}
                >
                  {props.renderLocalCycle ? (
                    props.renderLocalCycle(
                      local.key,
                      !!mapping?.align_reset && values.enabled
                    )
                  ) : (
                    <h4>{local.name}</h4>
                  )}
                  <div className='bg-muted/30 min-w-0 space-y-3 rounded-lg p-3'>
                    <Label htmlFor={`binding-${local.key}`}>
                      {t('Upstream cycle')}
                    </Label>
                    <Combobox
                      id={`binding-${local.key}`}
                      disabled={
                        !props.caps.sync_binding || !props.caps.sync_apply
                      }
                      value={mapping?.upstream_key ?? '__none'}
                      options={[
                        { value: '__none', label: t('Not synchronized') },
                        ...(
                          accounts.find((a) => a.id === values.account_id)
                            ?.cycles ??
                          source?.mapping.cycles ??
                          []
                        ).map((c) => ({
                          value: c.key,
                          label: syncCycleName(c.name || c.key, c.seconds),
                        })),
                      ]}
                      onValueChange={(v) =>
                        updateCycle(local.key, {
                          upstream_key: v === '__none' ? '' : (v ?? ''),
                        })
                      }
                    />
                    {mapping && (
                      <>
                        <Label>
                          <Switch
                            disabled={
                              !props.caps.sync_binding || !props.caps.sync_apply
                            }
                            checked={mapping.align_reset}
                            onCheckedChange={(v) =>
                              updateCycle(local.key, { align_reset: v })
                            }
                          />
                          {t('Align reset time')}
                        </Label>
                        <Label>
                          <Switch
                            disabled={
                              !props.caps.sync_binding || !props.caps.sync_apply
                            }
                            checked={mapping.enforce}
                            onCheckedChange={(v) =>
                              updateCycle(local.key, { enforce: v })
                            }
                          />
                          {t('Enforce upstream utilization')}
                        </Label>
                        {mapping.enforce && (
                          <>
                            <Label htmlFor={`threshold-${local.key}`}>
                              {t('Block at utilization (%)')}
                            </Label>
                            <Input
                              id={`threshold-${local.key}`}
                              type='number'
                              min={0.01}
                              max={100}
                              step='any'
                              disabled={
                                !props.caps.sync_binding ||
                                !props.caps.sync_apply
                              }
                              value={mapping.threshold * 100}
                              onChange={(e) =>
                                updateCycle(local.key, {
                                  threshold: Number(e.target.value) / 100,
                                })
                              }
                            />
                          </>
                        )}
                        <Accordion>
                          <AccordionItem value='advanced'>
                            <AccordionTrigger>
                              {t('Advanced synchronization settings')}
                            </AccordionTrigger>
                            <AccordionContent className='space-y-3'>
                              <Label htmlFor={`capacity-${local.key}`}>
                                {t(
                                  'Quota units per full upstream window (optional)'
                                )}
                              </Label>
                              <QuotaAmountInput
                                id={`capacity-${local.key}`}
                                label={t(
                                  'Quota units per full upstream window (optional)'
                                )}
                                disabled={
                                  !props.caps.sync_binding ||
                                  !props.caps.sync_apply
                                }
                                value={mapping.quota_per_full_window ?? 0}
                                onChange={(quota_per_full_window) =>
                                  updateCycle(local.key, {
                                    quota_per_full_window,
                                  })
                                }
                              />
                              <p className='text-muted-foreground text-xs'>
                                {t(
                                  'Set an explicit capacity to estimate upstream progress from local settled usage between polls. Zero keeps the upstream percentage unchanged. Billing remains separate.'
                                )}
                              </p>
                              {mapping.align_reset && (
                                <>
                                  <Label htmlFor={`shift-${local.key}`}>
                                    {t(
                                      'Automatic reset shift tolerance (seconds)'
                                    )}
                                  </Label>
                                  <Input
                                    id={`shift-${local.key}`}
                                    type='number'
                                    min={0}
                                    max={local.seconds}
                                    disabled={
                                      !props.caps.sync_binding ||
                                      !props.caps.sync_apply
                                    }
                                    value={mapping.max_shift_seconds}
                                    onChange={(e) =>
                                      updateCycle(local.key, {
                                        max_shift_seconds: Number(
                                          e.target.value
                                        ),
                                      })
                                    }
                                  />
                                </>
                              )}
                            </AccordionContent>
                          </AccordionItem>
                        </Accordion>
                      </>
                    )}
                  </div>
                </section>
              )
            })}
          </div>
          <Label htmlFor='binding-stale'>
            {t('When upstream data is stale')}
          </Label>
          <Combobox
            id='binding-stale'
            disabled={!props.caps.sync_binding || !props.caps.sync_apply}
            value={values.stale_policy}
            options={[
              { value: 'stop', label: t('Pause new requests') },
              {
                value: 'local',
                label: t('Use local quota during grace period'),
              },
            ]}
            onValueChange={(v) => {
              if (v === 'stop' || v === 'local') {
                form.setValue('stale_policy', v)
              }
            }}
          />
          {values.stale_policy === 'local' && (
            <>
              <Label htmlFor='binding-grace'>
                {t('Grace period (seconds)')}
              </Label>
              <Input
                id='binding-grace'
                type='number'
                min={0}
                max={86400}
                disabled={!props.caps.sync_binding || !props.caps.sync_apply}
                {...form.register('grace_seconds', { valueAsNumber: true })}
              />
            </>
          )}
          {Object.keys(form.formState.errors).length > 0 && (
            <p role='alert' className='text-destructive'>
              {t(
                'Check the upstream account and all cycle mappings before saving.'
              )}
            </p>
          )}
          {!props.onDraftChange && (
            <Button
              type='submit'
              disabled={
                save.isPending ||
                !props.caps.sync_binding ||
                !props.caps.sync_apply
              }
            >
              {t('Save')}
            </Button>
          )}
        </fieldset>
      </Container>
      {read.isError && <ErrorState onRetry={() => read.mutate()} />}
      {state.isError && <ErrorState onRetry={() => void state.refetch()} />}
      {!props.observations.length && values.account_id && (
        <SyncAccounts
          accounts={accounts.filter((a) => a.id === values.account_id)}
          staleSeconds={syncStaleSeconds(source)}
        />
      )}
      {props.initial.last_error && (
        <p role='alert' className='text-destructive'>
          {t(props.initial.last_error)}
        </p>
      )}
      {props.caps.sync_binding && props.initial.source_id > 0 && (
        <Button variant='outline' onClick={() => setDisconnect(true)}>
          {t('Unbind synchronization')}
        </Button>
      )}
      {!props.onDraftChange && (
        <SyncObservations observations={props.observations} />
      )}
      {props.caps.sync_apply &&
        props.observations
          .filter(
            (o) =>
              o.pending === 'Upstream reset shift requires confirmation' &&
              !o.stale
          )
          .map((o) => (
            <Button key={o.key} variant='outline' onClick={() => setReset(o)}>
              {t('Confirm reset alignment')} · {o.name}
            </Button>
          ))}
      <ConfirmDialog
        open={disconnect}
        onOpenChange={setDisconnect}
        title={t('Unbind synchronization')}
        desc={t(
          'Stop synchronization and release this upstream account binding. Local subscriptions and recorded usage are preserved.'
        )}
        isLoading={unbind.isPending}
        handleConfirm={() => unbind.mutate()}
      />
      <ConfirmDialog
        open={confirm !== null}
        onOpenChange={(v) => {
          if (!v) setConfirm(null)
        }}
        title={t('Save synchronization binding')}
        desc={t(
          'Selected time and utilization controls affect all members of this pool. Local usage is preserved.'
        )}
        isLoading={save.isPending}
        handleConfirm={() => {
          if (confirm) save.mutate(confirm)
        }}
      />
      <ConfirmDialog
        open={reset !== null}
        onOpenChange={(v) => {
          if (!v) setReset(null)
        }}
        title={t('Confirm reset alignment')}
        desc={t(
          'Move this reset boundary to the observed upstream time without clearing recorded usage.'
        )}
        isLoading={align.isPending}
        handleConfirm={() => {
          if (reset) align.mutate(reset)
        }}
      />
    </div>
  )
}
