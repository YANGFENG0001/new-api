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
import { useEffect, useRef, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { DateTimePicker } from '@/components/datetime-picker'
import { MultiSelect } from '@/components/multi-select'
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'

import {
  poolAPI,
  type Pool,
  type PoolSyncCreation,
  type Capabilities,
} from './api'
import { CycleEditor } from './cycle-editor'
import { PoolSyncImport } from './pool-sync-import'
import { poolSchema, emptyPool, newCycle } from './schema'
import { syncBindingSchema, syncCycleName, type SyncBinding } from './sync-api'
import { PoolSyncBinding } from './sync-binding'

export function PoolEditor(props: {
  pool?: Pool
  onClose: () => void
  canManage: boolean
  onSaved?: (pool: Pool) => void
  embedded?: boolean
  canCreateFromSync?: boolean
  canRunSync?: boolean
  caps?: Capabilities
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [fromSync, setFromSync] = useState(false)
  const [sync, setSync] = useState<PoolSyncCreation | null>(null)
  const [bindingDraft, setBindingDraft] = useState<SyncBinding | undefined>()
  const [savedRevision, setSavedRevision] = useState(0)
  const form = useForm<Pool>({
    resolver: zodResolver(poolSchema),
    defaultValues: props.pool ?? emptyPool(),
  })
  const catalog = useQuery({
    queryKey: ['subscription-pools', 'catalog'],
    queryFn: poolAPI.catalog,
  })
  const save = useMutation({
    mutationFn: (data: Pool) => {
      if (data.id) {
        let sync_binding: SyncBinding | undefined
        if (
          bindingDraft?.source_id &&
          props.caps?.sync_binding &&
          props.caps.sync_apply
        ) {
          sync_binding = syncBindingSchema.parse({
            ...bindingDraft,
            cycles: bindingDraft.cycles.filter((mapping) =>
              data.cycles.some((cycle) => cycle.key === mapping.local_key)
            ),
          })
        }
        return poolAPI.save({
          ...data,
          ...(sync_binding ? { sync_binding } : {}),
        })
      }
      return poolAPI.create({ ...data, ...(fromSync && sync ? { sync } : {}) })
    },
    onSuccess: async (pool) => {
      await Promise.all([
        client.invalidateQueries({ queryKey: ['subscription-pools'] }),
        client.invalidateQueries({ queryKey: ['subscription-sync'] }),
      ])
      form.reset(pool)
      setSavedRevision((revision) => revision + 1)
      toast.success(t('Saved successfully'))
      if (props.onSaved) props.onSaved(pool)
      else props.onClose()
    },
  })
  const values = form.watch()
  const observedVersion = useRef(props.pool?.version)
  const isDirty = form.formState.isDirty
  useEffect(() => {
    if (!props.pool || observedVersion.current === props.pool.version) return
    observedVersion.current = props.pool.version
    // Never overwrite unsaved edits with background observations.
    if (!isDirty) form.reset(props.pool)
  }, [props.pool, form, isDirty])
  const issue = Object.keys(form.formState.errors).length > 0
  return (
    <form
      className='mx-auto w-full max-w-6xl space-y-5 pb-6'
      onSubmit={form.handleSubmit((data) => save.mutate(data))}
    >
      {!props.embedded && (
        <div className='flex flex-wrap items-center justify-between gap-3'>
          <h2 className='text-lg font-semibold'>
            {props.pool ? t('Edit upstream pool') : t('Create upstream pool')}
          </h2>
          <Button type='button' variant='outline' onClick={props.onClose}>
            {t('Back')}
          </Button>
        </div>
      )}
      {!props.pool && props.onSaved && !fromSync && (
        <Alert>
          <AlertTitle>{t('Automatic synchronization')}</AlertTitle>
          <AlertDescription>
            {t(
              'Create from upstream to fill settings automatically, or configure local cycles manually.'
            )}
          </AlertDescription>
        </Alert>
      )}
      <p className='text-muted-foreground text-sm'>
        {t(
          'Times are shown in your browser time zone. Fixed durations do not drift after restarts.'
        )}
      </p>
      <fieldset
        disabled={!props.canManage || save.isPending}
        className='space-y-5'
      >
        {!props.pool && props.canCreateFromSync && (
          <>
            <Label>
              <Switch
                checked={fromSync}
                onCheckedChange={(checked) => {
                  setFromSync(checked)
                  setSync(null)
                  form.setValue('cycles', [])
                }}
              />
              {t('Create from upstream')}
            </Label>
            {fromSync && (
              <PoolSyncImport
                canRun={!!props.canRunSync}
                onClear={() => {
                  setSync(null)
                  form.setValue('cycles', [])
                }}
                onImport={(draft) => {
                  setSync(draft.sync)
                  if (
                    !form.formState.dirtyFields.name ||
                    !form.getValues('name')
                  ) {
                    form.setValue('name', draft.name)
                  }
                  form.setValue('end_time', draft.end_time, {
                    shouldValidate: true,
                  })
                  form.setValue('cycles', draft.cycles, {
                    shouldValidate: true,
                  })
                }}
              />
            )}
          </>
        )}
        <div className='grid gap-4 sm:grid-cols-2'>
          <div>
            <Label htmlFor='pool-name'>{t('Name')}</Label>
            <Input
              id='pool-name'
              {...form.register('name')}
              aria-invalid={!!form.formState.errors.name}
            />
          </div>
          <div>
            <Label>{t('Unified expiry')}</Label>
            <DateTimePicker
              value={
                values.end_time === 253402300799
                  ? undefined
                  : new Date(values.end_time * 1000)
              }
              placeholder={t('No upstream expiry reported')}
              disabled={!!bindingDraft?.enabled && bindingDraft.sync_expiry}
              onChange={(d) => {
                if (sync) setSync({ ...sync, sync_expiry: false })
                form.setValue(
                  'end_time',
                  d ? Math.floor(d.getTime() / 1000) : 253402300799,
                  { shouldValidate: true, shouldDirty: true }
                )
              }}
            />
          </div>
        </div>
        <div>
          <Label htmlFor='pool-note'>{t('Administrator note')}</Label>
          <Textarea id='pool-note' {...form.register('note')} />
        </div>
        <Label>
          <Switch
            checked={values.enabled}
            onCheckedChange={(v) =>
              form.setValue('enabled', v, { shouldDirty: true })
            }
          />
          {t('Enabled')}
        </Label>
        <div>
          <Label htmlFor='pool-channels'>{t('Upstream channels')}</Label>
          <MultiSelect
            id='pool-channels'
            options={(catalog.data ?? []).map((c) => ({
              value: String(c.id),
              label: `${c.name} #${c.id}`,
            }))}
            selected={values.channels.map(String)}
            allowCreate={false}
            onChange={(v) =>
              form.setValue('channels', v.map(Number), { shouldDirty: true })
            }
          />
          <p className='text-muted-foreground mt-2 text-xs'>
            {t(
              'Channels can be shared by multiple pools. Each request is charged to its selected subscription pool, not to a channel account.'
            )}
          </p>
          <p className='text-muted-foreground mt-2 text-xs'>
            {t(
              'Upstream account selection is controlled by the upstream service. Channel access does not guarantee an account match; synchronization calibrates only the account selected for this pool.'
            )}
          </p>
          {!values.channels.length && (
            <p role='status' className='text-warning mt-2 text-sm'>
              {t(
                'This pool has no allowed channels. Select at least one channel so its members can use their subscriptions.'
              )}
            </p>
          )}
        </div>
        {props.pool && props.caps?.sync_read ? (
          <PoolSyncBinding
            key={savedRevision}
            pool={{ ...props.pool, cycles: values.cycles }}
            caps={props.caps}
            onDraftChange={setBindingDraft}
            onPoolAligned={async () => {
              const latest = await poolAPI.list()
              const pool = latest.find((item) => item.id === props.pool?.id)
              if (!pool) return
              form.setValue('version', pool.version)
              form.setValue('end_time', pool.end_time)
              form.setValue(
                'cycles',
                form.getValues('cycles').map((cycle) => {
                  const saved = pool.cycles.find(
                    (item) => item.key === cycle.key
                  )
                  return saved
                    ? {
                        ...cycle,
                        next_reset: saved.next_reset,
                        seconds: saved.seconds,
                        name: syncCycleName(cycle.name, saved.seconds),
                      }
                    : cycle
                })
              )
            }}
            renderLocalCycle={(key, followsReset) => (
              <CycleEditor
                value={values.cycles.filter((cycle) => cycle.key === key)}
                onChange={(next) =>
                  form.setValue(
                    'cycles',
                    values.cycles.flatMap((cycle) =>
                      cycle.key === key ? next : [cycle]
                    ),
                    { shouldValidate: true, shouldDirty: true }
                  )
                }
                pooled
                single
                followReset={followsReset}
                lockedKeys={props.pool?.cycles.map((cycle) => cycle.key)}
              />
            )}
          />
        ) : (
          <CycleEditor
            value={values.cycles}
            onChange={(v) =>
              form.setValue('cycles', v, {
                shouldValidate: true,
                shouldDirty: true,
              })
            }
            pooled
            lockedKeys={props.pool?.cycles.map((c) => c.key)}
          />
        )}
        {props.pool && props.caps?.sync_read && (
          <Button
            type='button'
            variant='outline'
            disabled={values.cycles.length >= 32}
            onClick={() =>
              form.setValue(
                'cycles',
                [...values.cycles, newCycle(t('Cycle'))],
                { shouldDirty: true }
              )
            }
          >
            {t('Add cycle')}
          </Button>
        )}
        {issue && (
          <p role='alert' className='text-destructive text-sm'>
            {t('Check pool name, expiry and all cycle settings before saving.')}
          </p>
        )}
        {save.isError && (
          <p role='alert' className='text-destructive text-sm'>
            {t(save.error.message)}
          </p>
        )}
        {catalog.isError && (
          <Button
            type='button'
            variant='outline'
            onClick={() => void catalog.refetch()}
          >
            {t('Retry')}
          </Button>
        )}
        <p className='text-muted-foreground text-sm'>
          {t(
            'Saving does not move existing subscriptions or clear usage. Shared changes affect all members.'
          )}
        </p>
        <Button
          type='submit'
          disabled={
            save.isPending ||
            (!!props.pool && !!props.caps?.sync_read && !bindingDraft) ||
            (fromSync && (!sync || !values.cycles.length))
          }
        >
          {save.isPending ? t('Saving...') : t('Save')}
        </Button>
      </fieldset>
    </form>
  )
}
