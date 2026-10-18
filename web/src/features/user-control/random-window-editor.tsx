import { useId } from 'react'
import { useTranslation } from 'react-i18next'

import { Field, FieldLabel } from '@/components/ui/field'
import { Switch } from '@/components/ui/switch'

import { TransportRange } from './transport-editor'
import type { RandomWindow } from './types'

export function RandomWindowEditor(props: {
  value?: RandomWindow
  onChange: (value: RandomWindow | undefined) => void
}) {
  const { t } = useTranslation()
  const id = useId()
  const r = props.value
  return (
    <div className='space-y-3 rounded-lg border p-3'>
      <Field orientation='horizontal'>
        <Switch
          id={id}
          checked={r?.enabled ?? false}
          onCheckedChange={(enabled) =>
            props.onChange({
              active_min: 5,
              active_max: 15,
              idle_min: 5,
              idle_max: 15,
              ...r,
              enabled,
            })
          }
        />
        <FieldLabel htmlFor={id}>
          {t('Random continuous activation')}
        </FieldLabel>
      </Field>
      {r?.enabled && (
        <>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Alternate random rest and active intervals inside this window. Requests in the same interval share the same decision; the first interval is a rest interval. Base limits still apply.'
            )}
          </p>
          <TransportRange
            min={r.active_min}
            max={r.active_max}
            floor={1}
            ceiling={1440}
            minLabel={t('Minimum active duration (minutes)')}
            maxLabel={t('Maximum active duration (minutes)')}
            onChange={(min, max) =>
              props.onChange({ ...r, active_min: min, active_max: max })
            }
          />
          <TransportRange
            min={r.idle_min}
            max={r.idle_max}
            floor={1}
            ceiling={1440}
            minLabel={t('Minimum rest duration (minutes)')}
            maxLabel={t('Maximum rest duration (minutes)')}
            onChange={(min, max) =>
              props.onChange({ ...r, idle_min: min, idle_max: max })
            }
          />
        </>
      )}
    </div>
  )
}
