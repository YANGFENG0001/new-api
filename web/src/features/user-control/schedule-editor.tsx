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
import { ArrowDown, ArrowUp, Clock, Copy, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'

import { LimitsEditor } from './limits-editor'
import type { Schedule, ScheduleRule } from './types'

const WEEKDAYS = [
  { value: 1, key: 'Mon' },
  { value: 2, key: 'Tue' },
  { value: 3, key: 'Wed' },
  { value: 4, key: 'Thu' },
  { value: 5, key: 'Fri' },
  { value: 6, key: 'Sat' },
  { value: 7, key: 'Sun' },
]

function newRule(index: number): ScheduleRule {
  return {
    id: `rule-${Date.now().toString(36)}-${index}`,
    name: '',
    enabled: true,
    priority: 0,
    days: [],
    dates: [],
    start: '',
    end: '',
    limits: {},
  }
}

export function ScheduleEditor({
  value,
  onChange,
}: {
  value?: Schedule | null
  onChange: (next: Schedule | null) => void
}) {
  const { t } = useTranslation()
  const schedule: Schedule = value ?? {
    timezone: 'Asia/Shanghai',
    rules: [],
  }
  const rules = schedule.rules ?? []

  const update = (patch: Partial<Schedule>) => {
    const next: Schedule = { ...schedule, ...patch }
    const hasRules = (next.rules?.length ?? 0) > 0
    const hasDefault = next.default && Object.keys(next.default).length > 0
    onChange(hasRules || hasDefault ? next : null)
  }

  const setRule = (index: number, patch: Partial<ScheduleRule>) => {
    const next = rules.map((rule, i) =>
      i === index ? { ...rule, ...patch } : rule
    )
    update({ rules: next })
  }

  const move = (index: number, direction: -1 | 1) => {
    const target = index + direction
    if (target < 0 || target >= rules.length) return
    const next = [...rules]
    ;[next[index], next[target]] = [next[target], next[index]]
    update({ rules: next })
  }

  const remove = (index: number) => {
    update({ rules: rules.filter((_, i) => i !== index) })
  }

  const duplicate = (index: number) => {
    const source = rules[index]
    const copy: ScheduleRule = {
      ...source,
      id: `rule-${Date.now().toString(36)}-${index}`,
      name: source.name ? `${source.name} (copy)` : '',
      limits: JSON.parse(
        JSON.stringify(source.limits)
      ) as ScheduleRule['limits'],
    }
    const next = [...rules]
    next.splice(index + 1, 0, copy)
    update({ rules: next })
  }

  const toggleDay = (index: number, day: number) => {
    const rule = rules[index]
    const days = new Set(rule.days ?? [])
    if (days.has(day)) days.delete(day)
    else days.add(day)
    setRule(index, { days: [...days].sort((a, b) => a - b) })
  }

  const isOvernight = (rule: ScheduleRule) =>
    Boolean(rule.start && rule.end && rule.end < rule.start)

  return (
    <div className='space-y-3'>
      <div className='flex flex-wrap items-end justify-between gap-3'>
        <div className='space-y-1'>
          <Label className='text-muted-foreground text-xs font-normal'>
            {t('Schedule timezone')}
          </Label>
          <Input
            className='w-56'
            value={schedule.timezone ?? 'Asia/Shanghai'}
            onChange={(event) => update({ timezone: event.target.value })}
            placeholder='Asia/Shanghai'
          />
        </div>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={() => update({ rules: [...rules, newRule(rules.length)] })}
        >
          <Plus className='size-4' />
          {t('Add time window')}
        </Button>
      </div>

      <p className='text-muted-foreground text-xs'>
        {t(
          'Rules are evaluated independently. When several rules share the highest priority that matches now, their limits are merged and the strictest value wins. Lower-priority matches are ignored. An empty weekday list means every day; an empty date list means every date.'
        )}
      </p>

      {rules.length === 0 && (
        <div className='text-muted-foreground rounded-md border border-dashed p-6 text-center text-sm'>
          {t('No time windows. Limits apply all day.')}
        </div>
      )}

      <div className='space-y-3'>
        {rules.map((rule, index) => {
          const enabled = rule.enabled !== false
          return (
            <div
              key={rule.id ?? index}
              className={
                enabled ? 'rounded-lg border' : 'rounded-lg border opacity-60'
              }
            >
              <div className='bg-muted/30 flex flex-wrap items-center gap-2 border-b p-2'>
                <Switch
                  checked={enabled}
                  onCheckedChange={(checked) =>
                    setRule(index, { enabled: checked })
                  }
                />
                <Input
                  className='h-8 w-44'
                  value={rule.name ?? ''}
                  onChange={(event) =>
                    setRule(index, { name: event.target.value })
                  }
                  placeholder={t('Window name')}
                />
                <Label className='text-muted-foreground text-xs'>
                  {t('Priority')}
                </Label>
                <Input
                  className='h-8 w-20'
                  type='number'
                  value={rule.priority ?? 0}
                  onChange={(event) =>
                    setRule(index, {
                      priority:
                        Number.parseInt(event.target.value || '0', 10) || 0,
                    })
                  }
                />
                <Badge variant='outline'>
                  <Clock className='mr-1 size-3' />
                  {rule.start || rule.end
                    ? `${rule.start || '00:00'} - ${rule.end || '24:00'}`
                    : t('All day')}
                </Badge>
                {isOvernight(rule) && (
                  <Badge variant='secondary'>{t('Crosses midnight')}</Badge>
                )}
                <div className='ml-auto flex items-center gap-1'>
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon'
                    className='size-8'
                    onClick={() => move(index, -1)}
                    disabled={index === 0}
                    aria-label={t('Move up')}
                  >
                    <ArrowUp className='size-4' />
                  </Button>
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon'
                    className='size-8'
                    onClick={() => move(index, 1)}
                    disabled={index === rules.length - 1}
                    aria-label={t('Move down')}
                  >
                    <ArrowDown className='size-4' />
                  </Button>
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon'
                    className='size-8'
                    onClick={() => duplicate(index)}
                    aria-label={t('Duplicate')}
                  >
                    <Copy className='size-4' />
                  </Button>
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon'
                    className='text-destructive size-8'
                    onClick={() => remove(index)}
                    aria-label={t('Delete')}
                  >
                    <Trash2 className='size-4' />
                  </Button>
                </div>
              </div>

              <div className='space-y-4 p-3'>
                <div className='space-y-2'>
                  <Label className='text-muted-foreground text-xs'>
                    {t('Weekdays')}
                  </Label>
                  <div className='flex flex-wrap gap-1'>
                    {WEEKDAYS.map((day) => {
                      const active = (rule.days ?? []).includes(day.value)
                      return (
                        <Button
                          key={day.value}
                          type='button'
                          size='sm'
                          variant={active ? 'default' : 'outline'}
                          onClick={() => toggleDay(index, day.value)}
                        >
                          {t(day.key)}
                        </Button>
                      )
                    })}
                  </div>
                </div>

                <div className='grid gap-3 md:grid-cols-4'>
                  <div className='space-y-1'>
                    <Label className='text-muted-foreground text-xs'>
                      {t('Start time')}
                    </Label>
                    <Input
                      type='time'
                      value={rule.start ?? ''}
                      onChange={(event) =>
                        setRule(index, { start: event.target.value })
                      }
                    />
                  </div>
                  <div className='space-y-1'>
                    <Label className='text-muted-foreground text-xs'>
                      {t('End time')}
                    </Label>
                    <Input
                      type='time'
                      value={rule.end ?? ''}
                      onChange={(event) =>
                        setRule(index, { end: event.target.value })
                      }
                    />
                  </div>
                  <div className='space-y-1'>
                    <Label className='text-muted-foreground text-xs'>
                      {t('Start date')}
                    </Label>
                    <Input
                      type='date'
                      value={rule.start_date ?? ''}
                      onChange={(event) =>
                        setRule(index, { start_date: event.target.value })
                      }
                    />
                  </div>
                  <div className='space-y-1'>
                    <Label className='text-muted-foreground text-xs'>
                      {t('End date')}
                    </Label>
                    <Input
                      type='date'
                      value={rule.end_date ?? ''}
                      onChange={(event) =>
                        setRule(index, { end_date: event.target.value })
                      }
                    />
                  </div>
                </div>

                <div className='space-y-1'>
                  <Label className='text-muted-foreground text-xs'>
                    {t('Specific dates (YYYY-MM-DD, comma separated)')}
                  </Label>
                  <Input
                    value={(rule.dates ?? []).join(', ')}
                    onChange={(event) =>
                      setRule(index, {
                        dates: event.target.value
                          .split(/[,\n]/)
                          .map((item) => item.trim())
                          .filter(Boolean),
                      })
                    }
                    placeholder='2026-10-01, 2026-10-02'
                  />
                </div>

                <Separator />
                <div className='space-y-2'>
                  <Label className='text-sm font-medium'>
                    {t('Limits for this window')}
                  </Label>
                  <LimitsEditor
                    value={rule.limits}
                    onChange={(limits) => setRule(index, { limits })}
                  />
                </div>
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}
