import { zodResolver } from '@hookform/resolvers/zod'
import { useEffect, useId } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { Dialog } from '@/components/dialog'
import { MultiSelect } from '@/components/multi-select'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

import { savePool } from './api'
import { validPoolConfig, equalProbabilities } from './pool-validation'
import { ScheduleEditor } from './schedule-editor'
import { TransportRange } from './transport-editor'
import {
  isBehaviorPolicy,
  type PolicyView,
  type PoolConfig,
  type PoolTiming,
  type PoolView,
} from './types'

const schema = z.object({
  name: z.string().trim().min(1).max(64),
  description: z.string().max(255),
  enabled: z.boolean(),
  priority: z.number().int(),
  config: z.custom<PoolConfig>(
    (data) =>
      !!data &&
      typeof data === 'object' &&
      Array.isArray((data as PoolConfig).members) &&
      validPoolConfig(data as PoolConfig)
  ),
})
type Form = z.infer<typeof schema>

export function PoolTimingEditor(props: {
  value: PoolTiming
  onChange: (value: PoolTiming) => void
}) {
  const { t } = useTranslation()
  const id = useId()
  return (
    <div className='min-w-0 space-y-3'>
      <Label htmlFor={`${id}-probability`}>
        {t('Pool activation probability (%)')}
      </Label>
      <Input
        id={`${id}-probability`}
        type='number'
        min={0}
        max={100}
        step={0.01}
        value={props.value.probability / 100}
        onChange={(e) =>
          props.onChange({
            ...props.value,
            probability: Math.round(Number(e.target.value) * 100),
          })
        }
      />
      <TransportRange
        min={props.value.hold_min}
        max={props.value.hold_max}
        floor={1}
        ceiling={1440}
        minLabel={t('Minimum policy holding duration (minutes)')}
        maxLabel={t('Maximum policy holding duration (minutes)')}
        onChange={(hold_min, hold_max) =>
          props.onChange({ ...props.value, hold_min, hold_max })
        }
      />
    </div>
  )
}

export function ScheduledPoolEditor(props: {
  open: boolean
  pool: PoolView | null
  policies: PolicyView[]
  defaultTimezone: string
  onOpenChange: (open: boolean) => void
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const id = useId()
  const form = useForm<Form>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: '',
      description: '',
      enabled: true,
      priority: 0,
      config: { probability: 10000, hold_min: 5, hold_max: 15, members: [] },
    },
  })
  const config = useWatch({ control: form.control, name: 'config' })
  const enabled = useWatch({ control: form.control, name: 'enabled' })
  const saving = form.formState.isSubmitting
  useEffect(() => {
    if (!props.open) return
    form.reset(
      props.pool
        ? { ...props.pool.pool, config: structuredClone(props.pool.config) }
        : {
            name: '',
            description: '',
            enabled: true,
            priority: 0,
            config: {
              probability: 10000,
              hold_min: 5,
              hold_max: 15,
              members: [],
            },
          }
    )
  }, [props.open, props.pool, form])
  const setConfig = (value: PoolConfig) =>
    form.setValue('config', value, { shouldDirty: true })
  const candidates = props.policies.filter(isBehaviorPolicy)
  const options = candidates.map((p) => ({
    value: String(p.policy.id),
    label: p.policy.name,
    hint: p.policy.enabled ? undefined : t('Disabled'),
  }))
  for (const member of config.members) {
    if (!options.some((o) => o.value === String(member.policy_id))) {
      options.push({
        value: String(member.policy_id),
        label: `#${member.policy_id}`,
        hint: t('Invalid reference'),
      })
    }
  }
  const submit = form.handleSubmit(async (values) => {
    try {
      requireServerSuccess(
        await savePool({
          ...values,
          id: props.pool?.pool.id,
          version: props.pool?.pool.version,
        })
      )
      props.onSaved()
      props.onOpenChange(false)
    } catch (err) {
      handleServerError(err, t('Failed to save pool'))
    }
  })
  return (
    <Dialog
      open={props.open}
      onOpenChange={(open) => {
        if (!saving) props.onOpenChange(open)
      }}
      title={props.pool ? t('Edit strategy pool') : t('New strategy pool')}
      description={t(
        'Pools contain timing and policy references only. Each continuous segment first draws pool activation, then selects one policy.'
      )}
      contentClassName='sm:max-w-4xl'
      showCloseButton={false}
      footer={
        <>
          <Button
            variant='outline'
            disabled={saving}
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button type='submit' form={id} disabled={saving}>
            {t('Save')}
          </Button>
        </>
      }
    >
      <form
        id={id}
        onSubmit={submit}
        className='min-w-0 space-y-4'
        inert={saving}
      >
        <Label htmlFor={`${id}-name`}>{t('Name')}</Label>
        <Input
          id={`${id}-name`}
          {...form.register('name')}
          aria-invalid={!!form.formState.errors.name}
        />
        <Label htmlFor={`${id}-desc`}>{t('Description')}</Label>
        <Textarea id={`${id}-desc`} {...form.register('description')} />
        <Label htmlFor={`${id}-priority`}>{t('Priority')}</Label>
        <Input
          id={`${id}-priority`}
          type='number'
          {...form.register('priority', { valueAsNumber: true })}
        />
        <label className='flex items-center gap-2'>
          <Switch
            checked={enabled}
            onCheckedChange={(value) => form.setValue('enabled', value)}
          />
          {t('Enabled')}
        </label>
        <Label htmlFor={`${id}-members`}>{t('Member policies')}</Label>
        <MultiSelect
          id={`${id}-members`}
          options={options}
          selected={config.members.map((m) => String(m.policy_id))}
          onChange={(ids) =>
            setConfig({
              ...config,
              members: ids.map(
                (value) =>
                  config.members.find((m) => String(m.policy_id) === value) ?? {
                    policy_id: Number(value),
                    probability: 0,
                  }
              ),
            })
          }
        />
        <Button
          type='button'
          variant='outline'
          onClick={() =>
            setConfig({
              ...config,
              members: equalProbabilities(
                config.members.map((m) => m.policy_id)
              ),
            })
          }
        >
          {t('Distribute probabilities equally')}
        </Button>
        {config.members.map((m, index) => (
          <div
            key={m.policy_id}
            className='grid min-w-0 gap-2 rounded border p-3 sm:grid-cols-2'
          >
            <Label htmlFor={`${id}-${m.policy_id}`}>
              {t('Selection probability for {{name}} (%)', {
                name: options.find((o) => o.value === String(m.policy_id))
                  ?.label,
              })}
            </Label>
            <Input
              id={`${id}-${m.policy_id}`}
              type='number'
              min={0}
              max={100}
              step={0.01}
              value={m.probability / 100}
              onChange={(e) =>
                setConfig({
                  ...config,
                  members: config.members.map((item, i) =>
                    i === index
                      ? {
                          ...item,
                          probability: Math.round(Number(e.target.value) * 100),
                        }
                      : item
                  ),
                })
              }
            />
          </div>
        ))}
        <p className='text-muted-foreground text-sm'>
          {t(
            'Member probabilities must total 100%. They are conditional on pool activation; each segment selects at most one policy.'
          )}
        </p>
        <PoolTimingEditor
          value={config}
          onChange={(timing) => setConfig({ ...config, ...timing })}
        />
        <label className='flex items-center gap-2'>
          <Switch
            checked={!!config.schedule}
            onCheckedChange={(on) =>
              setConfig({
                ...config,
                schedule: on
                  ? { timezone: props.defaultTimezone, rules: [] }
                  : undefined,
                window_options: {},
              })
            }
          />
          {t('Use specific time windows')}
        </label>
        {config.schedule && (
          <ScheduleEditor
            timingOnly
            value={config.schedule}
            defaultTimezone={props.defaultTimezone}
            onChange={(schedule) => {
              const ids = new Set(schedule?.rules?.map((r) => r.id))
              setConfig({
                ...config,
                schedule,
                window_options: Object.fromEntries(
                  Object.entries(config.window_options ?? {}).filter(([key]) =>
                    ids.has(key)
                  )
                ),
              })
            }}
            renderWindowOptions={(windowId) => (
              <div className='space-y-3'>
                <label className='flex items-center gap-2'>
                  <Switch
                    checked={!!config.window_options?.[windowId]}
                    onCheckedChange={(on) => {
                      const options = { ...config.window_options }
                      if (on) {
                        options[windowId] = {
                          probability: config.probability,
                          hold_min: config.hold_min,
                          hold_max: config.hold_max,
                        }
                      } else delete options[windowId]
                      setConfig({ ...config, window_options: options })
                    }}
                  />
                  {t('Override pool timing for this window')}
                </label>
                {config.window_options?.[windowId] && (
                  <PoolTimingEditor
                    value={config.window_options[windowId]}
                    onChange={(value) =>
                      setConfig({
                        ...config,
                        window_options: {
                          ...config.window_options,
                          [windowId]: value,
                        },
                      })
                    }
                  />
                )}
              </div>
            )}
          />
        )}
        {form.formState.errors.config && (
          <p role='alert' className='text-destructive text-sm'>
            {t('Check pool probabilities, members and holding durations.')}
          </p>
        )}
      </form>
    </Dialog>
  )
}
