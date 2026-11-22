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

import { poolAPI, type Pool } from './api'
import { CycleEditor } from './cycle-editor'
import { poolSchema, emptyPool } from './schema'

export function PoolEditor(props: {
  pool?: Pool
  onClose: () => void
  canManage: boolean
  onSaved?: (pool: Pool) => void
  embedded?: boolean
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const form = useForm<Pool>({
    resolver: zodResolver(poolSchema),
    defaultValues: props.pool ?? emptyPool(),
  })
  const catalog = useQuery({
    queryKey: ['subscription-pools', 'catalog'],
    queryFn: poolAPI.catalog,
  })
  const save = useMutation({
    mutationFn: (data: Pool) =>
      data.id ? poolAPI.save(data) : poolAPI.create(data),
    onSuccess: (pool) => {
      void client.invalidateQueries({ queryKey: ['subscription-pools'] })
      toast.success(t('Saved successfully'))
      if (props.onSaved) props.onSaved(pool)
      else props.onClose()
    },
  })
  const values = form.watch()
  const issue = Object.keys(form.formState.errors).length > 0
  return (
    <form
      className='space-y-5 pb-6'
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
      {!props.pool && props.onSaved && (
        <Alert>
          <AlertTitle>{t('Automatic synchronization')}</AlertTitle>
          <AlertDescription>
            {t(
              'Save this pool to select a synchronization source and upstream account. The synchronization tab opens next.'
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
              value={new Date(values.end_time * 1000)}
              onChange={(d) =>
                form.setValue(
                  'end_time',
                  d ? Math.floor(d.getTime() / 1000) : 0,
                  { shouldValidate: true }
                )
              }
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
            onCheckedChange={(v) => form.setValue('enabled', v)}
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
            onChange={(v) => form.setValue('channels', v.map(Number))}
          />
          <p className='text-muted-foreground mt-2 text-xs'>
            {t(
              'Each channel must use a single upstream account. Channels cannot belong to multiple pools.'
            )}
          </p>
        </div>
        <CycleEditor
          value={values.cycles}
          onChange={(v) => form.setValue('cycles', v, { shouldValidate: true })}
          pooled
          lockedKeys={props.pool?.cycles.map((c) => c.key)}
        />
        <p className='text-muted-foreground text-xs'>
          {t(
            'Configure automatic upstream synchronization in the pool detail tab after saving.'
          )}
        </p>
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
        <Button type='submit' disabled={save.isPending}>
          {save.isPending
            ? t('Saving...')
            : t(
                !props.pool && props.onSaved
                  ? 'Save and configure synchronization'
                  : 'Save'
              )}
        </Button>
      </fieldset>
    </form>
  )
}
