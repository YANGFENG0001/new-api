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
import { useId } from 'react'
import { useTranslation } from 'react-i18next'

import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

import { ResourceSelector } from './resource-selector'
import { ScopedRulesEditor } from './scoped-rules-editor'
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

function parseIds(values: string[]): number[] {
  return values
    .map(Number)
    .filter((item) => Number.isSafeInteger(item) && item > 0)
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
  const { t } = useTranslation()
  const fieldId = useId()
  return (
    <div className='min-w-0 space-y-1'>
      <Label
        htmlFor={fieldId}
        className='text-muted-foreground text-xs font-normal'
      >
        {label}
      </Label>
      <Input
        id={fieldId}
        type='number'
        min='0'
        step={step}
        value={value && value > 0 ? String(value) : ''}
        onChange={(event) => onChange(toInt(event.target.value))}
        placeholder={placeholder ?? t('0 = unlimited')}
      />
    </div>
  )
}

export function LimitsEditor({
  value,
  onChange,
  scoped = false,
}: {
  value: Limits
  onChange: (next: Limits) => void
  scoped?: boolean
}) {
  const { t } = useTranslation()
  const editorId = useId()
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
        {!scoped && (
          <>
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
          </>
        )}
      </div>

      <section className='space-y-2'>
        <h4 className='text-sm font-medium'>{t('Rate limits')}</h4>
        <div className='grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4'>
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
          {!scoped && (
            <NumberField
              label={t('Burst capacity')}
              value={value.rate?.burst}
              onChange={(v) => set({ rate: { ...value.rate, burst: v } })}
            />
          )}
        </div>
      </section>

      <section className='space-y-2'>
        <h4 className='text-sm font-medium'>{t('Request quotas')}</h4>
        <div className='grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4'>
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
        <div className='grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4'>
          <div className='min-w-0 space-y-1'>
            <Label
              htmlFor={`${editorId}-field-0`}
              className='text-muted-foreground text-xs font-normal'
            >
              {t('Daily spend (USD)')}
            </Label>
            <Input
              id={`${editorId}-field-0`}
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
              placeholder={t('0 = unlimited')}
            />
          </div>
          <div className='min-w-0 space-y-1'>
            <Label
              htmlFor={`${editorId}-field-1`}
              className='text-muted-foreground text-xs font-normal'
            >
              {t('Monthly spend (USD)')}
            </Label>
            <Input
              id={`${editorId}-field-1`}
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
              placeholder={t('0 = unlimited')}
            />
          </div>
          <NumberField
            label={t('Monthly input tokens')}
            value={value.usage?.input_tokens}
            onChange={(v) =>
              set({ usage: { ...value.usage, input_tokens: v } })
            }
          />
          <NumberField
            label={t('Monthly output tokens')}
            value={value.usage?.output_tokens}
            onChange={(v) =>
              set({ usage: { ...value.usage, output_tokens: v } })
            }
          />
        </div>
      </section>

      {!scoped && (
        <>
          <section className='space-y-2'>
            <h4 className='text-sm font-medium'>{t('Model access')}</h4>
            <div className='grid gap-3 md:grid-cols-2'>
              <div className='min-w-0 space-y-1'>
                <Label
                  htmlFor={`${editorId}-field-2`}
                  className='text-muted-foreground text-xs font-normal'
                >
                  {t('Allowed models (comma separated)')}
                </Label>
                <ResourceSelector
                  id={`${editorId}-field-2`}
                  kind='model'
                  value={value.models?.allow ?? []}
                  onChange={(items) =>
                    set({
                      models: {
                        ...value.models,
                        allow: items,
                      },
                    })
                  }
                  placeholder={t('Empty = all models')}
                />
              </div>
              <div className='min-w-0 space-y-1'>
                <Label
                  htmlFor={`${editorId}-field-3`}
                  className='text-muted-foreground text-xs font-normal'
                >
                  {t('Blocked models (comma separated)')}
                </Label>
                <ResourceSelector
                  id={`${editorId}-field-3`}
                  kind='model'
                  value={value.models?.block ?? []}
                  onChange={(items) =>
                    set({
                      models: {
                        ...value.models,
                        block: items,
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
              <div className='min-w-0 space-y-1'>
                <Label
                  htmlFor={`${editorId}-field-4`}
                  className='text-muted-foreground text-xs font-normal'
                >
                  {t('Allowed channel IDs')}
                </Label>
                <ResourceSelector
                  id={`${editorId}-field-4`}
                  kind='channel'
                  value={value.channels?.allow_ids?.map(String) ?? []}
                  onChange={(items) =>
                    set({
                      channels: {
                        ...value.channels,
                        allow_ids: parseIds(items),
                      },
                    })
                  }
                />
              </div>
              <div className='min-w-0 space-y-1'>
                <Label
                  htmlFor={`${editorId}-field-5`}
                  className='text-muted-foreground text-xs font-normal'
                >
                  {t('Blocked channel IDs')}
                </Label>
                <ResourceSelector
                  id={`${editorId}-field-5`}
                  kind='channel'
                  value={value.channels?.block_ids?.map(String) ?? []}
                  onChange={(items) =>
                    set({
                      channels: {
                        ...value.channels,
                        block_ids: parseIds(items),
                      },
                    })
                  }
                />
              </div>
              <div className='min-w-0 space-y-1'>
                <Label
                  htmlFor={`${editorId}-field-6`}
                  className='text-muted-foreground text-xs font-normal'
                >
                  {t('Allowed channel types')}
                </Label>
                <ResourceSelector
                  id={`${editorId}-field-6`}
                  kind='channel-type'
                  value={value.channels?.allow_types?.map(String) ?? []}
                  onChange={(items) =>
                    set({
                      channels: {
                        ...value.channels,
                        allow_types: parseIds(items),
                      },
                    })
                  }
                />
              </div>
              <div className='min-w-0 space-y-1'>
                <Label
                  htmlFor={`${editorId}-field-7`}
                  className='text-muted-foreground text-xs font-normal'
                >
                  {t('Blocked channel types')}
                </Label>
                <ResourceSelector
                  id={`${editorId}-field-7`}
                  kind='channel-type'
                  value={value.channels?.block_types?.map(String) ?? []}
                  onChange={(items) =>
                    set({
                      channels: {
                        ...value.channels,
                        block_types: parseIds(items),
                      },
                    })
                  }
                />
              </div>
              <div className='space-y-1 md:col-span-2'>
                <Label
                  htmlFor={`${editorId}-field-8`}
                  className='text-muted-foreground text-xs font-normal'
                >
                  {t('Allowed groups')}
                </Label>
                <ResourceSelector
                  id={`${editorId}-field-8`}
                  kind='group'
                  value={value.channels?.allow_groups ?? []}
                  onChange={(items) =>
                    set({
                      channels: {
                        ...value.channels,
                        allow_groups: items,
                      },
                    })
                  }
                />
              </div>
            </div>
          </section>

          <section className='space-y-2'>
            <h4 className='text-sm font-medium'>{t('API key limits')}</h4>
            <div className='grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4'>
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
          <ScopedRulesEditor
            value={value.scoped_rules ?? []}
            renderLimits={(limits, onChange) => (
              <LimitsEditor scoped value={limits} onChange={onChange} />
            )}
            onChange={(rules) =>
              set({ scoped_rules: rules.length ? rules : undefined })
            }
          />
        </>
      )}
    </div>
  )
}
