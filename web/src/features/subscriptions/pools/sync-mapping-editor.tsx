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
import { nanoid } from 'nanoid'
import { useFieldArray, type UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

import type { SyncCycleMapping } from './sync-api'
import type { SyncSourceForm } from './sync-source-schema'

export function SyncMappingEditor(props: {
  form: UseFormReturn<SyncSourceForm>
}) {
  const { t } = useTranslation()
  const value = props.form.watch('mapping')
  const cycles = useFieldArray({
    control: props.form.control,
    name: 'mapping.cycles',
  })
  const fields = [
    ['success_path', t('Success field')],
    ['accounts_path', t('Account list field')],
    ['id_path', t('Stable account ID field')],
    ['name_path', t('Account name field')],
    ['sampled_at_path', t('Observation time field')],
    ['source_path', t('Observation source field')],
    ['expiry_path', t('Expiry field (optional)')],
  ] as const
  function update(index: number, patch: Partial<SyncCycleMapping>) {
    props.form.setValue(
      `mapping.cycles.${index}`,
      { ...value.cycles[index], ...patch },
      { shouldDirty: true }
    )
  }
  return (
    <div className='space-y-4'>
      <div className='grid gap-3 sm:grid-cols-2'>
        {fields.map(([key, label]) => (
          <div key={key}>
            <Label htmlFor={`map-${key}`}>{label}</Label>
            <Input
              id={`map-${key}`}
              value={value[key]}
              onChange={(e) =>
                props.form.setValue(`mapping.${key}`, e.target.value, {
                  shouldDirty: true,
                })
              }
            />
          </div>
        ))}
        <div>
          <Label htmlFor='sync-time-format'>{t('Time format')}</Label>
          <Combobox
            id='sync-time-format'
            value={value.time_format}
            options={[
              { value: 'rfc3339', label: t('Date with time zone') },
              { value: 'unix_s', label: t('Unix seconds') },
              { value: 'unix_ms', label: t('Unix milliseconds') },
            ]}
            onValueChange={(v) => {
              if (v === 'rfc3339' || v === 'unix_s' || v === 'unix_ms') {
                props.form.setValue('mapping.time_format', v, {
                  shouldDirty: true,
                })
              }
            }}
          />
        </div>
      </div>
      {cycles.fields.map((field, index) => (
        <fieldset
          key={field.id}
          className='space-y-3 rounded-lg border p-3'
          aria-label={value.cycles[index].name || t('Cycles')}
        >
          <div className='grid gap-3 sm:grid-cols-2'>
            {(
              [
                ['key', t('Cycle key')],
                ['name', t('Name')],
                ['utilization_path', t('Utilization field')],
                ['reset_path', t('Reset time field')],
                ['sampled_at_path', t('Observation time override (optional)')],
              ] as const
            ).map(([key, label]) => (
              <div key={key}>
                <Label htmlFor={`sync-map-${index}-${key}`}>{label}</Label>
                <Input
                  id={`sync-map-${index}-${key}`}
                  value={value.cycles[index][key]}
                  onChange={(e) => update(index, { [key]: e.target.value })}
                />
              </div>
            ))}
            <div>
              <Label htmlFor={`sync-seconds-${index}`}>
                {t('Cycle duration (seconds)')}
              </Label>
              <Input
                id={`sync-seconds-${index}`}
                type='number'
                min={1}
                max={315360000}
                value={value.cycles[index].seconds}
                onChange={(e) =>
                  update(index, { seconds: Number(e.target.value) })
                }
              />
            </div>
            <div>
              <Label htmlFor={`sync-scale-${index}`}>
                {t('Utilization format')}
              </Label>
              <Combobox
                id={`sync-scale-${index}`}
                value={value.cycles[index].scale}
                options={[
                  { value: 'ratio', label: t('Ratio (0–1)') },
                  { value: 'percent', label: t('Percent (0–100)') },
                ]}
                onValueChange={(v) => {
                  if (v === 'ratio' || v === 'percent') {
                    update(index, { scale: v })
                  }
                }}
              />
            </div>
          </div>
          <Button
            type='button'
            variant='outline'
            disabled={value.cycles.length <= 1}
            onClick={() => cycles.remove(index)}
          >
            {t('Remove')}
          </Button>
        </fieldset>
      ))}
      <Button
        type='button'
        variant='outline'
        disabled={value.cycles.length >= 32}
        onClick={() =>
          cycles.append({
            key: `cycle-${nanoid(8)}`,
            name: '',
            seconds: 3600,
            scale: 'ratio',
            utilization_path: '',
            reset_path: '',
            sampled_at_path: '',
          })
        }
      >
        {t('Add cycle')}
      </Button>
    </div>
  )
}
