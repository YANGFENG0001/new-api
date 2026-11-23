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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DateTimePicker } from '@/components/datetime-picker'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

import type { CycleRule } from './api'
import { newCycle } from './schema'
import { syncCycleName } from './sync-api'

function DurationInput(props: {
  value: number
  onChange: (n: number) => void
  id: string
  disabled?: boolean
}) {
  const { t } = useTranslation()
  const [unit, setUnit] = useState(props.value % 3600 === 0 ? 3600 : 1)
  return (
    <div className='flex gap-2'>
      <Input
        id={props.id}
        disabled={props.disabled}
        type='number'
        min={1}
        value={props.value / unit}
        onChange={(e) => props.onChange(Number(e.target.value) * unit)}
      />
      <Combobox
        disabled={props.disabled}
        value={String(unit)}
        options={[
          { value: '1', label: t('Seconds') },
          { value: '60', label: t('Minutes') },
          { value: '3600', label: t('Hours') },
          { value: '86400', label: t('Days') },
        ]}
        onValueChange={(v) => {
          if (v) setUnit(Number(v))
        }}
      />
    </div>
  )
}
export function CycleEditor(props: {
  value: CycleRule[]
  onChange: (v: CycleRule[]) => void
  pooled?: boolean
  lockedKeys?: string[]
  disabled?: boolean
  single?: boolean
  followReset?: boolean
}) {
  const { t } = useTranslation()
  function update(index: number, patch: Partial<CycleRule>) {
    props.onChange(
      props.value.map((r, i) =>
        i === index
          ? {
              ...r,
              ...patch,
              name: patch.seconds
                ? syncCycleName(r.name, patch.seconds)
                : (patch.name ?? r.name),
            }
          : r
      )
    )
  }
  return (
    <fieldset disabled={props.disabled} className='space-y-4'>
      {!props.single && (
        <p className='text-muted-foreground text-sm'>
          {t(
            'Cycle quotas use site quota units. Each cycle is checked independently.'
          )}
        </p>
      )}
      {props.value.map((rule, i) => (
        <section
          key={rule.key}
          className={
            props.single
              ? 'min-w-0 space-y-3'
              : 'space-y-3 rounded-lg border p-4'
          }
          aria-label={rule.name}
        >
          <div className='grid gap-3 sm:grid-cols-2'>
            <div>
              <Label htmlFor={`${rule.key}-name`}>{t('Name')}</Label>
              <Input
                id={`${rule.key}-name`}
                value={rule.name}
                onChange={(e) => update(i, { name: e.target.value })}
              />
            </div>
            <div>
              <Label htmlFor={`${rule.key}-duration`}>
                {t('Cycle duration')}
              </Label>
              <DurationInput
                id={`${rule.key}-duration`}
                disabled={props.followReset}
                value={rule.seconds}
                onChange={(seconds) => update(i, { seconds })}
              />
            </div>
            <div>
              <Label>{t('Next reset time')}</Label>
              <DateTimePicker
                disabled={props.followReset}
                value={new Date(rule.next_reset * 1000)}
                onChange={(d) =>
                  update(i, {
                    next_reset: d ? Math.floor(d.getTime() / 1000) : 0,
                  })
                }
              />
            </div>
            <div>
              <Label htmlFor={`${rule.key}-quota`}>{t('Quota units')}</Label>
              <Input
                id={`${rule.key}-quota`}
                type='number'
                min={1}
                step={1}
                disabled={rule.unlimited}
                value={rule.limit}
                onChange={(e) => update(i, { limit: Number(e.target.value) })}
              />
            </div>
          </div>
          <div className='flex flex-wrap gap-5'>
            <Label>
              <Switch
                checked={rule.enabled}
                onCheckedChange={(enabled) => update(i, { enabled })}
              />
              {t('Enabled')}
            </Label>
            <Label>
              <Switch
                checked={rule.unlimited}
                onCheckedChange={(unlimited) => update(i, { unlimited })}
              />
              {t('Unlimited')}
            </Label>
            {props.pooled && (
              <Label>
                <Switch
                  checked={rule.shared}
                  onCheckedChange={(shared) =>
                    update(i, {
                      shared,
                      enforce_upstream: shared ? false : rule.enforce_upstream,
                    })
                  }
                />
                {t('Shared by all pool members')}
              </Label>
            )}
          </div>
          {props.pooled && !rule.shared && (
            <div className='space-y-2'>
              <Label htmlFor={`${rule.key}-upstream`}>
                {t('Upstream total quota (0 means unknown)')}
              </Label>
              <Input
                id={`${rule.key}-upstream`}
                type='number'
                min={0}
                value={rule.upstream_limit}
                onChange={(e) =>
                  update(i, { upstream_limit: Number(e.target.value) })
                }
              />
              <Label>
                <Switch
                  checked={rule.enforce_upstream}
                  onCheckedChange={(enforce_upstream) =>
                    update(i, { enforce_upstream })
                  }
                />
                {t('Enforce upstream total quota')}
              </Label>
            </div>
          )}
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={props.lockedKeys?.includes(rule.key)}
            onClick={() =>
              props.onChange(props.value.filter((_, n) => n !== i))
            }
          >
            {t('Remove')}
          </Button>
        </section>
      ))}
      {!props.single && (
        <Button
          type='button'
          variant='outline'
          disabled={props.value.length >= 32}
          onClick={() => props.onChange([...props.value, newCycle(t('Cycle'))])}
        >
          {t('Add cycle')}
        </Button>
      )}
      {!props.single && (
        <p className='text-muted-foreground text-xs'>
          {t(
            'Existing cycle keys cannot be removed. Disable them instead. Duration, scope and quota changes apply at the next boundary; moving the next reset preserves usage.'
          )}
        </p>
      )}
    </fieldset>
  )
}
