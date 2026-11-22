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
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import type { z } from 'zod'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { StaticDataTable } from '@/components/data-table'
import { DateTimePicker } from '@/components/datetime-picker'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { toIntlLocale } from '@/i18n/languages'

import { poolAPI, type Pool, type Capabilities, type CycleView } from './api'
import { CycleProgress } from './cycle-progress'
import { snapshotSchema } from './schema'
import { PoolSyncBinding } from './sync-binding'

export function PoolDetail(props: { pool: Pool; caps: Capabilities }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const client = useQueryClient()
  const detail = useQuery({
    queryKey: ['subscription-pools', 'detail', props.pool.id],
    queryFn: () => poolAPI.detail(props.pool.id),
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
  if (detail.isLoading) return <LoadingState />
  if (detail.isError) {
    return <ErrorState onRetry={() => void detail.refetch()} />
  }
  return (
    <div className='space-y-4 pb-5'>
      <Tabs defaultValue='overview'>
        <TabsList>
          {props.caps.sync_read && (
            <TabsTrigger value='sync'>
              {t('Automatic synchronization')}
            </TabsTrigger>
          )}
          <TabsTrigger value='overview'>{t('Overview')}</TabsTrigger>
          <TabsTrigger value='members'>{t('Members')}</TabsTrigger>
          <TabsTrigger value='operations'>{t('Operation records')}</TabsTrigger>
          <TabsTrigger value='upstream'>{t('Manual calibration')}</TabsTrigger>
        </TabsList>
        {props.caps.sync_read && (
          <TabsContent value='sync'>
            <PoolSyncBinding pool={props.pool} caps={props.caps} />
          </TabsContent>
        )}
        <TabsContent value='overview'>
          <CycleProgress cycles={detail.data?.cycles ?? []} />
        </TabsContent>
        <TabsContent value='members'>
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
                id: 'end',
                header: t('Expiry time'),
                cell: (m) => new Date(m.end_time * 1000).toLocaleString(locale),
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
          <StaticDataTable
            data={detail.data?.events ?? []}
            getRowKey={(e) => e.id}
            columns={[
              {
                id: 'time',
                header: t('Time'),
                cell: (e) =>
                  new Date(e.created_at * 1000).toLocaleString(locale),
              },
              {
                id: 'action',
                header: t('Action'),
                cell: (e) => actions[e.action] ?? t('Operation records'),
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
        <TabsContent value='upstream'>
          {props.caps.calibrate && (
            <SnapshotForm
              pool={props.pool}
              cycles={detail.data?.cycles ?? []}
            />
          )}
        </TabsContent>
      </Tabs>
      {props.caps.shared_reset && (
        <div className='space-y-2'>
          <p className='text-destructive text-sm'>
            {t(
              'Pool cycle reset affects every member and does not reset other cycles.'
            )}
          </p>
          <div className='flex flex-wrap gap-2'>
            {props.pool.cycles.map((c) => (
              <Button
                key={c.key}
                variant='outline'
                onClick={() => setReset(c.key)}
              >
                {t('Reset')} · {c.name}
              </Button>
            ))}
          </div>
        </div>
      )}
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
      <Label>{t('Cycle')}</Label>
      <Combobox
        options={props.pool.cycles.map((c) => ({
          value: c.key,
          label: c.name,
        }))}
        value={values.key}
        onValueChange={(v) => form.setValue('key', v ?? '')}
      />
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
        <div>
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
        <div>
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
        <div>
          <Label htmlFor='snapshot-used'>{t('Used')}</Label>
          <Input
            id='snapshot-used'
            type='number'
            min={0}
            {...form.register('used', { valueAsNumber: true })}
          />
        </div>
        <div>
          <Label htmlFor='snapshot-limit'>{t('Quota')}</Label>
          <Input
            id='snapshot-limit'
            type='number'
            min={0}
            {...form.register('limit', { valueAsNumber: true })}
          />
        </div>
      </div>
      <Label htmlFor='snapshot-unit'>{t('Unit')}</Label>
      <Input id='snapshot-unit' {...form.register('unit')} />
      <Label>
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
