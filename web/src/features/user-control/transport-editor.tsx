import { useId } from 'react'
import { useTranslation } from 'react-i18next'

import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'

import type { ControlActions } from './types'

export function TransportEditor(props: {
  value: ControlActions
  onChange: (patch: Partial<ControlActions>) => void
}) {
  const { t } = useTranslation()
  const id = useId()
  return (
    <div className='min-w-0 space-y-4 rounded-lg border p-3'>
      <Field orientation='horizontal'>
        <Switch
          id={`${id}-delay`}
          checked={Boolean(props.value.delay)}
          onCheckedChange={(on) =>
            props.onChange({
              delay: on ? { min_ms: 1000, max_ms: 3000 } : undefined,
            })
          }
        />
        <FieldLabel htmlFor={`${id}-delay`}>
          {t('Random upstream delay')}
        </FieldLabel>
      </Field>
      {props.value.delay && (
        <TransportRange
          min={props.value.delay.min_ms}
          max={props.value.delay.max_ms}
          minLabel={t('Minimum delay (ms)')}
          maxLabel={t('Maximum delay (ms)')}
          onChange={(min, max) =>
            props.onChange({ delay: { min_ms: min, max_ms: max } })
          }
        />
      )}
      <Field orientation='horizontal'>
        <Switch
          id={`${id}-delivery`}
          checked={Boolean(props.value.delivery)}
          onCheckedChange={(on) =>
            props.onChange({
              delivery: on ? { min_cps: 10, max_cps: 30 } : undefined,
            })
          }
        />
        <FieldLabel htmlFor={`${id}-delivery`}>
          {t('Buffered character delivery')}
        </FieldLabel>
      </Field>
      {props.value.delivery && (
        <>
          <TransportRange
            min={props.value.delivery.min_cps}
            max={props.value.delivery.max_cps}
            floor={1}
            minLabel={t('Minimum characters per second')}
            maxLabel={t('Maximum characters per second')}
            onChange={(min, max) =>
              props.onChange({ delivery: { min_cps: min, max_cps: max } })
            }
          />
          <p className='text-muted-foreground text-xs'>
            {t(
              'Text streams only. The complete upstream response is cached before delivery, increasing first-token latency. Limits: 16 MiB, 30 minutes and 32 concurrent deliveries per instance. Billing remains based on real usage, including client disconnects.'
            )}
          </p>
        </>
      )}
      <p className='text-muted-foreground text-xs'>
        {t(
          'Delay may be zero; delivery rate must be positive. Use integers with maximum not less than minimum. No configured upper cap; request timeouts still apply.'
        )}
      </p>
    </div>
  )
}

// Shared by transport, continuous random windows and reusable strategy pools.
export function TransportRange(props: {
  min: number
  max: number
  floor?: number
  ceiling?: number
  minLabel: string
  maxLabel: string
  onChange: (min: number, max: number) => void
}) {
  const { t } = useTranslation()
  const id = useId()
  const invalid =
    !Number.isSafeInteger(props.min) ||
    !Number.isSafeInteger(props.max) ||
    props.min < (props.floor ?? 0) ||
    props.max < props.min ||
    (props.ceiling !== undefined && props.max > props.ceiling)
  return (
    <div className='grid min-w-0 gap-3 sm:grid-cols-2'>
      <Field data-invalid={invalid}>
        <FieldLabel htmlFor={`${id}-min`}>{props.minLabel}</FieldLabel>
        <Input
          id={`${id}-min`}
          type='number'
          min={props.floor ?? 0}
          max={props.ceiling}
          step={1}
          value={props.min}
          aria-invalid={invalid}
          onChange={(e) => props.onChange(Number(e.target.value), props.max)}
        />
      </Field>
      <Field data-invalid={invalid}>
        <FieldLabel htmlFor={`${id}-max`}>{props.maxLabel}</FieldLabel>
        <Input
          id={`${id}-max`}
          type='number'
          min={props.floor ?? 0}
          max={props.ceiling}
          step={1}
          value={props.max}
          aria-invalid={invalid}
          onChange={(e) => props.onChange(props.min, Number(e.target.value))}
        />
        {invalid && (
          <FieldError>
            {t('Enter an ordered range within the allowed bounds.')}
          </FieldError>
        )}
      </Field>
    </div>
  )
}
