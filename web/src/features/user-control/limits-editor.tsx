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
import { useTranslation } from 'react-i18next'

import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

import type { Limits } from './types'

const QUOTA_UNIT = 500000

function toInt(value: string): number | undefined {
  const parsed = Number.parseInt(value, 10)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : undefined
}

function toQuotaUnits(value: string): number | undefined {
  const parsed = Number.parseFloat(value)
  return Number.isFinite(parsed) && parsed > 0
    ? Math.round(parsed * QUOTA_UNIT)
    : undefined
}

function quotaToUsd(value?: number): string {
  return value && value > 0 ? String(value / QUOTA_UNIT) : ''
}

function splitList(value?: string[]): string {
  return (value ?? []).join(', ')
}

function parseList(value: string): string[] {
  return value
    .split(/[,\n]/)
    .map((item) => item.trim())
    .filter(Boolean)
}

function parseIdList(value: string): number[] {
  return parseList(value)
    .map((item) => Number.parseInt(item, 10))
    .filter((item) => Number.isFinite(item) && item > 0)
}

function NumberField({
  label,
  value,
  onChange,
  placeholder,
  step,
}: {
  label: string
  value: number | undefined
  onChange: (value: number | undefined) => void
  placeholder?: string
  step?: string
}) {
  return (
    <div className='space-y-1'>
      <Label className='text-muted-foreground text-xs font-normal'>
        {label}
      </Label>
      <Input
        type='number'
        min='0'
        step={step}
        value={value && value > 0 ? String(value) : ''}
        onChange={(event) => onChange(toInt(event.target.value))}
        placeholder={placeholder ?? '0 = unlimited'}
      />
    </div>
  )
}

export function LimitsEditor({
  value,
  onChange,
}: {
  value: Limits
  onChange: (next: Limits) => void
}) {
  const { t } = useTranslation()
  const set = (patch: Partial<Limits>) => onChange({ ...value, ...patch })

  return (
    <div className='space-y-4'>
      <div className='flex flex-wrap gap-6'>
        <label className='flex items-center gap-2 text-sm'>
          <Switch
            checked={value.blocked === true}
            onCheckedChange={(checked) =>
              set({ blocked: checked || undefined })
            }
          />
          {t('Block all requests')}
        </label>
        <label className='flex items-center gap-2 text-sm'>
          <Switch
            checked={value.allow_relay !== false}
            onCheckedChange={(checked) =>
              set({ allow_relay: checked ? undefined : false })
            }
          />
          {t('Allow relay')}
        </label>
        <label className='flex items-center gap-2 text-sm'>
          <Switch
            checked={value.allow_playground !== false}
            onCheckedChange={(checked) =>
              set({ allow_playground: checked ? undefined : false })
            }
          />
          {t('Allow Playground')}
        </label>
      </div>

      <section className='space-y-2'>
        <h4 className='text-sm font-medium'>{t('Rate limits')}</h4>
        <div className='grid grid-cols-2 gap-3 md:grid-cols-4'>
          <NumberField
            label={t('Requests per minute')}
            value={value.rate?.rpm}
            onChange={(v) => set({ rate: { ...value.rate, rpm: v } })}
          />
          <NumberField
            label={t('Requests per day')}
            value={value.rate?.rpd}
            onChange={(v) => set({ rate: { ...value.rate, rpd: v } })}
          />
          <NumberField
            label={t('Concurrent requests')}
            value={value.rate?.concurrent}
            onChange={(v) => set({ rate: { ...value.rate, concurrent: v } })}
          />
          <NumberField
            label={t('Burst capacity')}
            value={value.rate?.burst}
            onChange={(v) => set({ rate: { ...value.rate, burst: v } })}
          />
        </div>
      </section>

      <section className='space-y-2'>
        <h4 className='text-sm font-medium'>{t('Request quotas')}</h4>
        <div className='grid grid-cols-2 gap-3 md:grid-cols-4'>
          <NumberField
            label={t('Daily requests')}
            value={value.requests?.daily}
            onChange={(v) => set({ requests: { ...value.requests, daily: v } })}
          />
          <NumberField
            label={t('Monthly requests')}
            value={value.requests?.monthly}
            onChange={(v) =>
              set({ requests: { ...value.requests, monthly: v } })
            }
          />
        </div>
      </section>

      <section className='space-y-2'>
        <h4 className='text-sm font-medium'>{t('Usage quotas')}</h4>
        <div className='grid grid-cols-2 gap-3 md:grid-cols-4'>
          <div className='space-y-1'>
            <Label className='text-muted-foreground text-xs font-normal'>
              {t('Daily spend (USD)')}
            </Label>
            <Input
              type='number'
              min='0'
              step='0.01'
              value={quotaToUsd(value.usage?.daily_quota)}
              onChange={(event) =>
                set({
                  usage: {
                    ...value.usage,
                    daily_quota: toQuotaUnits(event.target.value),
                  },
                })
              }
              placeholder='0 = unlimited'
            />
          </div>
          <div className='space-y-1'>
            <Label className='text-muted-foreground text-xs font-normal'>
              {t('Monthly spend (USD)')}
            </Label>
            <Input
              type='number'
              min='0'
              step='0.01'
              value={quotaToUsd(value.usage?.monthly_quota)}
              onChange={(event) =>
                set({
                  usage: {
                    ...value.usage,
                    monthly_quota: toQuotaUnits(event.target.value),
                  },
                })
              }
              placeholder='0 = unlimited'
            />
          </div>
          <NumberField
            label={t('Input tokens')}
            value={value.usage?.input_tokens}
            onChange={(v) =>
              set({ usage: { ...value.usage, input_tokens: v } })
            }
          />
          <NumberField
            label={t('Output tokens')}
            value={value.usage?.output_tokens}
            onChange={(v) =>
              set({ usage: { ...value.usage, output_tokens: v } })
            }
          />
        </div>
      </section>

      <section className='space-y-2'>
        <h4 className='text-sm font-medium'>{t('Model access')}</h4>
        <div className='grid gap-3 md:grid-cols-2'>
          <div className='space-y-1'>
            <Label className='text-muted-foreground text-xs font-normal'>
              {t('Allowed models (comma separated)')}
            </Label>
            <Input
              value={splitList(value.models?.allow)}
              onChange={(event) =>
                set({
                  models: {
                    ...value.models,
                    allow: parseList(event.target.value),
                  },
                })
              }
              placeholder={t('Empty = all models')}
            />
          </div>
          <div className='space-y-1'>
            <Label className='text-muted-foreground text-xs font-normal'>
              {t('Blocked models (comma separated)')}
            </Label>
            <Input
              value={splitList(value.models?.block)}
              onChange={(event) =>
                set({
                  models: {
                    ...value.models,
                    block: parseList(event.target.value),
                  },
                })
              }
            />
          </div>
        </div>
      </section>

      <section className='space-y-2'>
        <h4 className='text-sm font-medium'>{t('Channel access')}</h4>
        <div className='grid gap-3 md:grid-cols-2'>
          <div className='space-y-1'>
            <Label className='text-muted-foreground text-xs font-normal'>
              {t('Allowed channel IDs')}
            </Label>
            <Input
              value={splitList(value.channels?.allow_ids?.map(String))}
              onChange={(event) =>
                set({
                  channels: {
                    ...value.channels,
                    allow_ids: parseIdList(event.target.value),
                  },
                })
              }
            />
          </div>
          <div className='space-y-1'>
            <Label className='text-muted-foreground text-xs font-normal'>
              {t('Blocked channel IDs')}
            </Label>
            <Input
              value={splitList(value.channels?.block_ids?.map(String))}
              onChange={(event) =>
                set({
                  channels: {
                    ...value.channels,
                    block_ids: parseIdList(event.target.value),
                  },
                })
              }
            />
          </div>
          <div className='space-y-1'>
            <Label className='text-muted-foreground text-xs font-normal'>
              {t('Allowed channel types')}
            </Label>
            <Input
              value={splitList(value.channels?.allow_types?.map(String))}
              onChange={(event) =>
                set({
                  channels: {
                    ...value.channels,
                    allow_types: parseIdList(event.target.value),
                  },
                })
              }
            />
          </div>
          <div className='space-y-1'>
            <Label className='text-muted-foreground text-xs font-normal'>
              {t('Blocked channel types')}
            </Label>
            <Input
              value={splitList(value.channels?.block_types?.map(String))}
              onChange={(event) =>
                set({
                  channels: {
                    ...value.channels,
                    block_types: parseIdList(event.target.value),
                  },
                })
              }
            />
          </div>
          <div className='space-y-1 md:col-span-2'>
            <Label className='text-muted-foreground text-xs font-normal'>
              {t('Allowed groups')}
            </Label>
            <Input
              value={splitList(value.channels?.allow_groups)}
              onChange={(event) =>
                set({
                  channels: {
                    ...value.channels,
                    allow_groups: parseList(event.target.value),
                  },
                })
              }
            />
          </div>
        </div>
      </section>

      <section className='space-y-2'>
        <h4 className='text-sm font-medium'>{t('API key limits')}</h4>
        <div className='grid grid-cols-2 gap-3 md:grid-cols-4'>
          <NumberField
            label={t('Max active keys')}
            value={value.api_keys?.max_active}
            onChange={(v) =>
              set({ api_keys: { ...value.api_keys, max_active: v } })
            }
          />
          <NumberField
            label={t('Max total keys')}
            value={value.api_keys?.max_total}
            onChange={(v) =>
              set({ api_keys: { ...value.api_keys, max_total: v } })
            }
          />
        </div>
      </section>
    </div>
  )
}
