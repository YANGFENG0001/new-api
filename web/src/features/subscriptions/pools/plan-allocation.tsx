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
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { DateTimePicker } from '@/components/datetime-picker'
import { ErrorState } from '@/components/error-state'
import { Combobox } from '@/components/ui/combobox'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

import { poolAPI, type AllocationConfig } from './api'
import { CycleEditor } from './cycle-editor'

// Controlled fields are shared by plan creation and editing. The parent saves
// the plan and these defaults together; editing fields never writes to the API.
export function PlanAllocationFields(props: {
  value: AllocationConfig
  onChange: (value: AllocationConfig) => void
  disabled?: boolean
}) {
  const { t } = useTranslation()
  const pools = useQuery({
    queryKey: ['subscription-pools', 'list'],
    queryFn: poolAPI.list,
    enabled: props.value.mode === 'fixed',
    staleTime: 10000,
  })
  const value = props.value
  return (
    <fieldset
      disabled={props.disabled}
      className='space-y-4 rounded-lg border p-4'
    >
      <Label htmlFor='plan-allocation-mode'>{t('Allocation mode')}</Label>
      <Combobox
        id='plan-allocation-mode'
        value={value.mode}
        options={[
          { value: 'legacy', label: t('Traditional subscription') },
          {
            value: 'independent',
            label: t('Independent multi-cycle subscription'),
          },
          { value: 'fixed', label: t('Default upstream pool') },
          { value: 'assign', label: t('Administrator assignment required') },
        ]}
        onValueChange={(mode) => {
          if (mode) props.onChange({ ...value, mode })
        }}
      />
      <p className='text-muted-foreground text-xs'>
        {t(
          'Defaults only affect new subscriptions. Existing members keep their assigned pool.'
        )}
      </p>
      {value.mode === 'fixed' && (
        <>
          <Label htmlFor='plan-default-pool'>
            {t('Default upstream pool')}
          </Label>
          <Combobox
            id='plan-default-pool'
            value={value.pool_id ? String(value.pool_id) : ''}
            disabled={pools.isPending || pools.isError}
            options={(pools.data ?? []).map((pool) => ({
              value: String(pool.id),
              label: pool.name,
            }))}
            onValueChange={(id) =>
              props.onChange({ ...value, pool_id: Number(id) })
            }
          />
          {pools.isError && <ErrorState onRetry={() => void pools.refetch()} />}
          <p className='text-muted-foreground text-xs'>
            {t(
              'Cycle settings are managed in the upstream pool, not copied into the plan.'
            )}
          </p>
          {(
            pools.data?.find((pool) => pool.id === value.pool_id)?.cycles ?? []
          ).map((cycle) => (
            <p key={cycle.key} className='text-sm'>
              {cycle.name} · {cycle.shared ? t('Shared') : t('Independent')}
            </p>
          ))}
        </>
      )}
      {value.mode === 'assign' && (
        <p className='text-sm'>
          {t(
            'Purchased subscriptions stay pending and cannot relay until an administrator assigns a pool.'
          )}
        </p>
      )}
      {value.mode === 'independent' && (
        <CycleEditor
          value={value.cycles}
          onChange={(cycles) => props.onChange({ ...value, cycles })}
        />
      )}
      {value.mode !== 'legacy' && (
        <>
          <Label>
            <Switch
              checked={value.fixed_end_time > 0}
              onCheckedChange={(on) =>
                props.onChange({
                  ...value,
                  fixed_end_time: on
                    ? Math.floor(Date.now() / 1000) + 86400
                    : 0,
                })
              }
            />
            {t('Fixed expiry instead of relative validity')}
          </Label>
          {value.fixed_end_time > 0 ? (
            <DateTimePicker
              value={new Date(value.fixed_end_time * 1000)}
              onChange={(date) =>
                props.onChange({
                  ...value,
                  fixed_end_time: date ? Math.floor(date.getTime() / 1000) : 0,
                })
              }
            />
          ) : (
            value.mode === 'fixed' && (
              <p className='text-sm'>{t('Follow pool expiry')}</p>
            )
          )}
        </>
      )}
    </fieldset>
  )
}
