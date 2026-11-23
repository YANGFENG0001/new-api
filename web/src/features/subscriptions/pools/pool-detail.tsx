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
import { useState, type ReactNode } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import type { z } from 'zod'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { StaticDataTable } from '@/components/data-table'
import { DateTimePicker } from '@/components/datetime-picker'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { toIntlLocale } from '@/i18n/languages'

import { poolAPI, type Pool, type Capabilities, type CycleView } from './api'
import { CycleProgress } from './cycle-progress'
import { QuotaAmountInput } from './quota-amount-input'
import { formatMultiplier } from './quota-share'
import { snapshotSchema } from './schema'
import { syncAPI } from './sync-api'
import { PoolSyncBinding } from './sync-binding'
import { SyncObservations } from './sync-observations'
import { SyncTimestamp } from './sync-usage'

export function PoolDetail(props: {
  pool: Pool
  caps: Capabilities
  tab?: string
  onTabChange?: (value: string) => void
  settings?: ReactNode
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const client = useQueryClient()
  const binding = useQuery({
    queryKey: ['subscription-sync', 'binding', props.pool.id],
    queryFn: () => syncAPI.binding(props.pool.id),
    enabled: !!props.caps.sync_read,
    refetchInterval: 30000,
    staleTime: 10000,
  })
  const detail = useQuery({
    queryKey: ['subscription-pools', 'detail', props.pool.id],
    queryFn: () => poolAPI.detail(props.pool.id),
    refetchInterval: 10000,
  })
  const [reset, setReset] = useState<string | null>(null)
  const resetMutation = useMutation({
    mutationFn: (key: string) => poolAPI.reset(props.pool.id, key),
    onSuccess: () => {
      setReset(null)
      void client.invalidateQueries({ queryKey: ['subscription-pools'] })
    },
  })
  const actions: Record<string, string> = {
    binding_save: t('Save synchronization binding'),
    unbind: t('Unbind synchronization'),
    pool_sync: t('Automatic synchronization'),
    reset_confirm: t('Confirm reset alignment'),
    binding_error: t('Unavailable'),
    save: t('Save'),
    assign: t('Assign'),
    reset: t('Reset'),
    personal_reset: t('Reset independent cycles'),
    snapshot: t('Upstream observation'),
    calibrate: t('Manual calibration'),
  }
  const statuses: Record<string, string> = {
    active: t('Active'),
    cancelled: t('Cancelled'),
    expired: t('Expired'),
  }
  const events = [
    ...(detail.data?.events ?? []).map((event) => ({
      ...event,
      recordKey: `local-${event.id}`,
      type: t('Local usage statistics'),
      message: '',
      changes: undefined,
    })),
    ...(detail.data?.sync_events ?? []).map((event) => ({
      ...event,
      recordKey: `sync-${event.id}`,
      type: t('Automatic synchronization'),
      subscription_id: 0,
    })),
  ].sort((a, b) => b.created_at - a.created_at || b.id - a.id)
  return (
    <div className='space-y-4 pb-5'>
      <Tabs
        defaultValue='overview'
        value={
          props.tab === 'sync' || props.tab === 'upstream'
            ? 'settings'
            : props.tab
        }
        onValueChange={(value) => props.onTabChange?.(String(value))}
      >
        <TabsList className='max-w-full justify-start overflow-x-auto'>
          <TabsTrigger value='overview'>{t('Overview')}</TabsTrigger>
          {(props.settings ||
            props.caps.sync_read ||
            props.caps.calibrate ||
            props.caps.shared_reset) && (
            <TabsTrigger value='settings'>{t('Settings')}</TabsTrigger>
          )}
          <TabsTrigger value='members'>{t('Members')}</TabsTrigger>
          <TabsTrigger value='operations'>{t('Operation records')}</TabsTrigger>
        </TabsList>
        <TabsContent value='settings'>
          {props.settings ??
            (props.caps.sync_read && (
              <PoolSyncBinding pool={props.pool} caps={props.caps} />
            ))}
          {(props.caps.calibrate || props.caps.shared_reset) && (
            <Accordion>
              <AccordionItem value='advanced'>
                <AccordionTrigger>{t('Advanced operations')}</AccordionTrigger>
                <AccordionContent className='space-y-4'>
                  {props.caps.calibrate && (
                    <SnapshotForm
                      pool={props.pool}
                      cycles={detail.data?.cycles ?? []}
                    />
                  )}
                  {props.caps.shared_reset && (
                    <div className='space-y-2'>
                      <p className='text-destructive text-sm'>
                        {t(
                          'Pool cycle reset affects every member and does not reset other cycles.'
                        )}
                      </p>
                      <div className='flex flex-wrap gap-2'>
                        {props.pool.cycles.map((cycle) => (
                          <Button
                            key={cycle.key}
                            variant='outline'
                            onClick={() => setReset(cycle.key)}
                          >
                            {t('Reset')} · {cycle.name}
                          </Button>
                        ))}
                      </div>
                    </div>
                  )}
                </AccordionContent>
              </AccordionItem>
            </Accordion>
          )}
        </TabsContent>
        <TabsContent value='overview'>
          {detail.isLoading && <LoadingState />}
          {detail.isError && (
            <ErrorState onRetry={() => void detail.refetch()} />
          )}
          <CycleProgress
            cycles={detail.data?.cycles ?? []}
            observations={
              props.caps.sync_read ? binding.data?.observations : undefined
            }
          />
          {props.caps.sync_read && binding.isError && (
            <ErrorState onRetry={() => void binding.refetch()} />
          )}
          {props.caps.sync_read && (
            <SyncObservations
              observations={(binding.data?.observations ?? []).filter(
                (observation) =>
                  !detail.data?.cycles.some(
                    (cycle) => cycle.key === observation.key
                  )
              )}
            />
          )}
        </TabsContent>
        <TabsContent value='members'>
          {detail.isLoading && <LoadingState />}
          {detail.isError && (
            <ErrorState onRetry={() => void detail.refetch()} />
          )}
          <StaticDataTable
            data={detail.data?.members ?? []}
            getRowKey={(m) => m.subscription_id}
            columns={[
              {
                id: 'id',
                header: t('Subscription'),
                cell: (m) => `#${m.subscription_id}`,
              },
              {
                id: 'name',
                header: t('Username'),
                cell: (m) => m.username || `#${m.user_id}`,
              },
              {
                id: 'share',
                header: t('Quota share'),
                cell: (m) => {
                  const mult = formatMultiplier(m.quota_multiplier)
                  const overrideCount = m.cycle_overrides?.length ?? 0
                  return overrideCount > 0
                    ? `${mult} · ${t('Custom override')} (${overrideCount})`
                    : mult
                },
              },
              {
                id: 'end',
                header: t('Expiry time'),
                cell: (m) => (
                  <SyncTimestamp
                    value={m.end_time === 253402300799 ? 0 : m.end_time}
                    fallback='Never'
                  />
                ),
              },
              {
                id: 'status',
                header: t('Status'),
                cell: (m) => statuses[m.status] ?? t('Unknown'),
              },
            ]}
          />
        </TabsContent>
        <TabsContent value='operations'>
          {detail.isLoading && <LoadingState />}
          {detail.isError && (
            <ErrorState onRetry={() => void detail.refetch()} />
          )}
          <StaticDataTable
            data={events}
            getRowKey={(e) => e.recordKey}
            columns={[
              {
                id: 'time',
                header: t('Time'),
                cell: (e) =>
                  new Date(e.created_at * 1000).toLocaleString(locale),
              },
              {
                id: 'type',
                header: t('Type'),
                cell: (e) => e.type,
              },
              {
                id: 'action',
                header: t('Action'),
                cell: (e) => actions[e.action] ?? t('Operation records'),
              },
              {
                id: 'details',
                header: t('Details'),
                cell: (e) => (
                  <div className='space-y-1 text-xs'>
                    {e.message && <p>{t(e.message)}</p>}
                    {e.changes?.map((change) => (
                      <p key={change.key}>
                        {change.key} ·{' '}
                        {t(change.message || 'Automatic synchronization')}
                        {change.before > 0 &&
                          ` · ${new Date(change.before * 1000).toLocaleString(locale)}`}
                        {change.after > 0 &&
                          ` → ${new Date(change.after * 1000).toLocaleString(locale)}`}
                      </p>
                    ))}
                  </div>
                ),
              },
              {
                id: 'actor',
                header: t('Operator'),
                cell: (e) => `#${e.actor_id}`,
              },
              {
                id: 'sub',
                header: t('Subscription'),
                cell: (e) =>
                  e.subscription_id ? `#${e.subscription_id}` : '—',
              },
            ]}
          />
        </TabsContent>
      </Tabs>
      <ConfirmDialog
        open={reset !== null}
        onOpenChange={(v) => {
          if (!v) setReset(null)
        }}
        title={t('Reset pool cycle')}
        desc={t(
          'Pool cycle reset affects every member and does not reset other cycles.'
        )}
        handleConfirm={() => {
          if (reset) resetMutation.mutate(reset)
        }}
        isLoading={resetMutation.isPending}
      >
        {resetMutation.isError && (
          <p role='alert'>{t(resetMutation.error.message)}</p>
        )}
      </ConfirmDialog>
    </div>
  )
}

function SnapshotForm(props: { pool: Pool; cycles: CycleView[] }) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const form = useForm<z.infer<typeof snapshotSchema>>({
    resolver: zodResolver(snapshotSchema),
    defaultValues: {
      key: props.pool.cycles[0]?.key ?? '',
      unit: 'quota',
      used: 0,
      limit: 0,
      window_start: 0,
      window_end: 0,
      calibrate: false,
    },
  })
  const values = form.watch()
  const [payload, setPayload] = useState<Record<string, unknown> | null>(null)
  const mutation = useMutation({
    mutationFn: () => poolAPI.snapshot(props.pool.id, payload),
    onSuccess: () => {
      setPayload(null)
      void client.invalidateQueries({ queryKey: ['subscription-pools'] })
    },
  })
  return (
    <form
      onSubmit={form.handleSubmit((v) =>
        setPayload({
          ...v,
          event_id: crypto.randomUUID(),
          source: 'manual',
          sampled_at: Math.floor(Date.now() / 1000),
        })
      )}
      className='space-y-3 rounded-lg border p-4'
    >
      <p className='text-muted-foreground text-sm'>
        {t(
          'Observations are separate from local usage. Calibration requires matching quota units and the exact current window.'
        )}
      </p>
      <div className='space-y-1.5'>
        <Label>{t('Cycle')}</Label>
        <Combobox
          options={props.pool.cycles.map((c) => ({
            value: c.key,
            label: c.name,
          }))}
          value={values.key}
          onValueChange={(v) => form.setValue('key', v ?? '')}
        />
      </div>
      <Button
        type='button'
        variant='outline'
        onClick={() => {
          const c = props.cycles.find((v) => v.key === values.key)
          if (c) {
            form.setValue('window_start', c.start_time)
            form.setValue('window_end', c.next_reset)
          }
        }}
      >
        {t('Use current window')}
      </Button>
      <div className='grid gap-3 sm:grid-cols-2'>
        <div className='space-y-1.5'>
          <Label>{t('Window start')}</Label>
          <DateTimePicker
            value={
              values.window_start
                ? new Date(values.window_start * 1000)
                : undefined
            }
            onChange={(d) =>
              form.setValue(
                'window_start',
                d ? Math.floor(d.getTime() / 1000) : 0
              )
            }
          />
        </div>
        <div className='space-y-1.5'>
          <Label>{t('Window end')}</Label>
          <DateTimePicker
            value={
              values.window_end ? new Date(values.window_end * 1000) : undefined
            }
            onChange={(d) =>
              form.setValue(
                'window_end',
                d ? Math.floor(d.getTime() / 1000) : 0
              )
            }
          />
        </div>
        <div className='space-y-1.5'>
          <Label htmlFor='snapshot-used'>{t('Used')}</Label>
          {values.unit.trim().toLowerCase() === 'quota' ? (
            <QuotaAmountInput
              id='snapshot-used'
              label={t('Used')}
              value={values.used}
              onChange={(used) =>
                form.setValue('used', used, { shouldValidate: true })
              }
            />
          ) : (
            <Input
              id='snapshot-used'
              type='number'
              min={0}
              {...form.register('used', { valueAsNumber: true })}
            />
          )}
        </div>
        <div className='space-y-1.5'>
          <Label htmlFor='snapshot-limit'>{t('Quota')}</Label>
          {values.unit.trim().toLowerCase() === 'quota' ? (
            <QuotaAmountInput
              id='snapshot-limit'
              label={t('Quota')}
              value={values.limit}
              onChange={(limit) =>
                form.setValue('limit', limit, { shouldValidate: true })
              }
            />
          ) : (
            <Input
              id='snapshot-limit'
              type='number'
              min={0}
              {...form.register('limit', { valueAsNumber: true })}
            />
          )}
        </div>
      </div>
      <div className='space-y-1.5'>
        <Label htmlFor='snapshot-unit'>{t('Unit')}</Label>
        <Input id='snapshot-unit' {...form.register('unit')} />
      </div>
      <Label className='flex items-center gap-2'>
        <Switch
          checked={values.calibrate}
          onCheckedChange={(v) => form.setValue('calibrate', v)}
        />
        {t('Apply quota calibration')}
      </Label>
      {Object.keys(form.formState.errors).length > 0 && (
        <p role='alert' className='text-destructive'>
          {t('Invalid upstream snapshot')}
        </p>
      )}
      <Button type='submit' disabled={mutation.isPending}>
        {t('Save')}
      </Button>
      <ConfirmDialog
        open={payload !== null}
        onOpenChange={(open) => {
          if (!open) setPayload(null)
        }}
        title={t('Manual calibration')}
        desc={t(
          'Observations are separate from local usage. Calibration requires matching quota units and the exact current window.'
        )}
        isLoading={mutation.isPending}
        handleConfirm={() => mutation.mutate()}
      >
        {mutation.isError && <p role='alert'>{t(mutation.error.message)}</p>}
      </ConfirmDialog>
    </form>
  )
}
