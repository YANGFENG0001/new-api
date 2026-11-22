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

import { DateTimePicker } from '@/components/datetime-picker'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

import { poolAPI, type AllocationConfig } from './api'
import { CycleEditor } from './cycle-editor'
import { allocationSchema } from './schema'

export function PlanAllocationSettings(props: { planId?: number }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const caps = useQuery({
    queryKey: ['subscription-pools', 'capabilities'],
    queryFn: poolAPI.capabilities,
  })
  const config = useQuery({
    queryKey: ['subscription-pools', 'plan', props.planId],
    queryFn: () => poolAPI.config(props.planId ?? 0),
    enabled: !!props.planId && !!caps.data?.read && open,
  })
  if (!caps.data?.read) return null
  return (
    <section className='space-y-3 rounded-lg border p-4'>
      <h3 className='text-sm font-semibold'>
        {t('Allocation and aligned cycles')}
      </h3>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Defaults only affect new subscriptions. Existing members keep their assigned pool.'
        )}
      </p>
      <Button
        type='button'
        variant='outline'
        disabled={!props.planId}
        onClick={() => setOpen(true)}
      >
        {t('Configure allocation')}
      </Button>
      {!props.planId && (
        <p className='text-muted-foreground text-xs'>
          {t(
            'Save the plan first, then configure allocation before offering it for purchase.'
          )}
        </p>
      )}
      <Dialog
        open={open}
        onOpenChange={setOpen}
        title={t('Allocation and aligned cycles')}
        contentClassName='sm:max-w-4xl'
      >
        {config.data && (
          <AllocationForm
            key={`${props.planId}-${config.data.version}`}
            planId={props.planId ?? 0}
            config={config.data}
            canManage={!!caps.data.manage}
            onClose={() => setOpen(false)}
          />
        )}
        {config.isLoading && <p>{t('Loading...')}</p>}
        {config.isError && (
          <Button onClick={() => void config.refetch()}>{t('Retry')}</Button>
        )}
      </Dialog>
    </section>
  )
}
function AllocationForm(props: {
  planId: number
  config: AllocationConfig
  canManage: boolean
  onClose: () => void
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const form = useForm<z.infer<typeof allocationSchema>>({
    resolver: zodResolver(allocationSchema),
    defaultValues: {
      ...props.config,
      plan_id: props.planId,
      mode: (props.config.mode || 'independent') as
        | 'independent'
        | 'fixed'
        | 'assign',
    },
  })
  const pools = useQuery({
    queryKey: ['subscription-pools', 'list'],
    queryFn: poolAPI.list,
  })
  const save = useMutation({
    mutationFn: (v: AllocationConfig) => poolAPI.saveConfig(props.planId, v),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ['subscription-pools'] })
      props.onClose()
    },
  })
  const values = form.watch()
  return (
    <form
      className='space-y-4'
      onSubmit={(event) => {
        event.stopPropagation()
        void form.handleSubmit((v) => save.mutate(v))(event)
      }}
    >
      <p className='text-muted-foreground text-sm'>
        {t(
          'Enabling aligned cycles replaces legacy quota settings for new subscriptions only.'
        )}
      </p>
      <fieldset
        disabled={!props.canManage || save.isPending}
        className='space-y-4'
      >
        <Label>{t('Allocation mode')}</Label>
        <Combobox
          value={values.mode}
          options={[
            { value: 'independent', label: t('Independent subscription') },
            { value: 'fixed', label: t('Default upstream pool') },
            { value: 'assign', label: t('Administrator assignment required') },
          ]}
          onValueChange={(v) =>
            form.setValue('mode', v as 'independent' | 'fixed' | 'assign')
          }
        />
        {values.mode === 'fixed' && (
          <>
            <Label>{t('Default upstream pool')}</Label>
            <Combobox
              value={String(values.pool_id || '')}
              options={(pools.data ?? []).map((p) => ({
                value: String(p.id),
                label: p.name,
              }))}
              onValueChange={(v) => form.setValue('pool_id', Number(v))}
            />
            <p className='text-muted-foreground text-sm'>
              {t(
                'Cycle settings are managed in the upstream pool, not copied into the plan.'
              )}
            </p>
          </>
        )}
        {values.mode === 'assign' && (
          <p className='text-muted-foreground text-sm'>
            {t(
              'Purchased subscriptions stay pending and cannot relay until an administrator assigns a pool.'
            )}
          </p>
        )}
        {values.mode === 'independent' && (
          <CycleEditor
            value={values.cycles}
            onChange={(v) => form.setValue('cycles', v)}
          />
        )}
        <Label>
          <Switch
            checked={values.fixed_end_time > 0}
            onCheckedChange={(v) =>
              form.setValue(
                'fixed_end_time',
                v ? Math.floor(Date.now() / 1000) + 86400 : 0
              )
            }
          />
          {t('Fixed expiry instead of relative validity')}
        </Label>
        {values.fixed_end_time > 0 && (
          <DateTimePicker
            value={new Date(values.fixed_end_time * 1000)}
            onChange={(d) =>
              form.setValue(
                'fixed_end_time',
                d ? Math.floor(d.getTime() / 1000) : 0
              )
            }
          />
        )}
        {Object.keys(form.formState.errors).length > 0 && (
          <p role='alert' className='text-destructive'>
            {t('Check pool name, expiry and all cycle settings before saving.')}
          </p>
        )}
        {save.isError && (
          <p role='alert' className='text-destructive'>
            {t(save.error.message)}
          </p>
        )}
        <Button type='submit' disabled={save.isPending}>
          {t('Save')}
        </Button>
      </fieldset>
    </form>
  )
}
