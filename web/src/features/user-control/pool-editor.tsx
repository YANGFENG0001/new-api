import { useTranslation } from 'react-i18next'

import { Checkbox } from '@/components/ui/checkbox'

import { TransportRange } from './transport-editor'
import type { PolicyView, RandomPool } from './types'

export function PoolEditor(props: {
  value: RandomPool
  policies: PolicyView[]
  selfID?: number
  onChange: (value: RandomPool) => void
}) {
  const { t } = useTranslation()
  const candidates = props.policies.filter(
    (p) => p.policy.id !== props.selfID && !p.limits.random_pool
  )
  return (
    <section className='min-w-0 space-y-3 rounded-lg border p-3'>
      <h3 className='text-sm font-semibold'>{t('Strategy pool')}</h3>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Select reusable complete policies. One enabled member is selected with equal probability for each continuous holding interval. Member schedules and all baseline limits still apply. Nested pools are not supported.'
        )}
      </p>
      <div className='max-h-56 space-y-2 overflow-y-auto rounded-md border p-3'>
        {candidates.map((p) => (
          <label
            key={p.policy.id}
            className='flex items-center gap-2 text-sm break-words'
          >
            <Checkbox
              checked={props.value.policy_ids.includes(p.policy.id)}
              onCheckedChange={(checked) =>
                props.onChange({
                  ...props.value,
                  policy_ids: checked
                    ? [...props.value.policy_ids, p.policy.id]
                    : props.value.policy_ids.filter((id) => id !== p.policy.id),
                })
              }
            />
            {p.policy.name}
            {!p.policy.enabled && (
              <span className='text-muted-foreground'>({t('Disabled')})</span>
            )}
          </label>
        ))}
        {candidates.length === 0 && (
          <p className='text-muted-foreground text-sm'>
            {t('No policies yet')}
          </p>
        )}
      </div>
      <TransportRange
        min={props.value.hold_min}
        max={props.value.hold_max}
        floor={1}
        ceiling={1440}
        minLabel={t('Minimum policy holding duration (minutes)')}
        maxLabel={t('Maximum policy holding duration (minutes)')}
        onChange={(min, max) =>
          props.onChange({ ...props.value, hold_min: min, hold_max: max })
        }
      />
    </section>
  )
}
