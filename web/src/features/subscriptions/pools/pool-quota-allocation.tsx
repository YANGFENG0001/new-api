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

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { toIntlLocale } from '@/i18n/languages'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatNumber } from '@/lib/format'
import {
  useSystemConfigStore,
  type CurrencyDisplayType,
} from '@/stores/system-config-store'

import type { CycleOverride, CycleRule } from './api'
import { QuotaAmountInput } from './quota-amount-input'
import {
  defaultOverrideLimit,
  formatMultiplier,
  scaleCycleQuota,
} from './quota-share'

const MULTIPLIER_PRESETS = [0.5, 1, 2, 3]

function formatQuotaLabel(
  value: number,
  displayType: CurrencyDisplayType,
  locale: Intl.LocalesArgument
): string {
  if (displayType === 'TOKENS') return formatNumber(value, locale)
  return formatQuotaWithCurrency(value, {
    locale,
    digitsLarge: 2,
    digitsSmall: 4,
    abbreviate: false,
  })
}

export function PoolQuotaAllocationFields(props: {
  idPrefix: string
  multiplier: number
  overrides: CycleOverride[]
  poolCycles?: CycleRule[]
  disabled?: boolean
  onChange: (next: {
    quota_multiplier: number
    cycle_overrides: CycleOverride[]
  }) => void
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const displayType = useSystemConfigStore(
    (s) => s.config.currency.quotaDisplayType
  )
  const multiplier =
    Number.isFinite(props.multiplier) && props.multiplier > 0
      ? props.multiplier
      : 1
  const overrides = props.overrides ?? []

  function setMultiplier(next: number) {
    if (!Number.isFinite(next) || next <= 0) return
    props.onChange({
      quota_multiplier: Math.min(100, Math.max(0.01, next)),
      cycle_overrides: overrides,
    })
  }

  function upsertOverride(nextOverride: CycleOverride) {
    const exists = overrides.some((ov) => ov.key === nextOverride.key)
    const nextOverrides = exists
      ? overrides.map((ov) => (ov.key === nextOverride.key ? nextOverride : ov))
      : [...overrides, nextOverride]
    props.onChange({
      quota_multiplier: multiplier,
      cycle_overrides: nextOverrides,
    })
  }

  function removeOverride(key: string) {
    props.onChange({
      quota_multiplier: multiplier,
      cycle_overrides: overrides.filter((ov) => ov.key !== key),
    })
  }

  const visibleCycles = (props.poolCycles ?? []).filter((c) => c.enabled)

  return (
    <div className='bg-muted/20 space-y-3 rounded-md border p-3'>
      <div className='space-y-1.5'>
        <Label htmlFor={`${props.idPrefix}-multiplier`}>
          {t('Independent cycle quota multiplier')}
        </Label>
        <div className='flex flex-wrap items-center gap-2'>
          <Input
            id={`${props.idPrefix}-multiplier`}
            type='number'
            min={0.01}
            max={100}
            step='any'
            disabled={props.disabled}
            className='w-28'
            value={multiplier}
            onChange={(e) => {
              const parsed = Number(e.target.value)
              if (Number.isFinite(parsed) && parsed > 0) {
                setMultiplier(parsed)
              }
            }}
          />
          <div className='flex flex-wrap gap-1'>
            {MULTIPLIER_PRESETS.map((preset) => (
              <Button
                key={preset}
                type='button'
                size='sm'
                variant={multiplier === preset ? 'default' : 'outline'}
                disabled={props.disabled}
                onClick={() => setMultiplier(preset)}
              >
                {preset}×
              </Button>
            ))}
          </div>
        </div>
        <p className='text-muted-foreground text-xs'>
          {t(
            'Scales independent cycle limits for members in this pool (for example, 2× grants double weekly quota). Shared cycles always follow the pool.'
          )}
        </p>
      </div>

      {visibleCycles.length > 0 && (
        <div className='space-y-2'>
          <p className='text-sm font-medium'>
            {t('Effective pool cycle quotas')}
          </p>
          {visibleCycles.map((cycle) => {
            const override = overrides.find((ov) => ov.key === cycle.key)
            if (cycle.shared) {
              return (
                <div
                  key={cycle.key}
                  className='bg-background flex flex-wrap items-center justify-between gap-2 rounded-md border px-3 py-2 text-sm'
                >
                  <div className='flex items-center gap-2'>
                    <span className='font-medium'>{cycle.name}</span>
                    <Badge variant='secondary'>{t('Shared')}</Badge>
                  </div>
                  <span className='text-muted-foreground text-xs'>
                    {cycle.unlimited
                      ? t('Unlimited')
                      : formatQuotaLabel(cycle.limit, displayType, locale)}{' '}
                    · {t('Shared across the upstream pool')}
                  </span>
                </div>
              )
            }

            const scaledLimit = cycle.unlimited
              ? 0
              : scaleCycleQuota(cycle.limit, multiplier)
            const effectiveUnlimited = override
              ? override.unlimited
              : cycle.unlimited
            const effectiveLimit = override ? override.limit : scaledLimit

            return (
              <div
                key={cycle.key}
                className='bg-background space-y-2.5 rounded-md border p-3'
              >
                <div className='flex flex-wrap items-center justify-between gap-2'>
                  <div className='flex flex-wrap items-center gap-2 text-sm'>
                    <span className='font-medium'>{cycle.name}</span>
                    <Badge variant='outline'>{t('Independent')}</Badge>
                    <span className='font-medium'>
                      {effectiveUnlimited
                        ? t('Unlimited')
                        : formatQuotaLabel(effectiveLimit, displayType, locale)}
                    </span>
                    {!override && !cycle.unlimited && multiplier !== 1 && (
                      <span className='text-muted-foreground text-xs'>
                        ({formatMultiplier(multiplier)} ×{' '}
                        {formatQuotaLabel(cycle.limit, displayType, locale)})
                      </span>
                    )}
                    {override && (
                      <Badge variant='secondary'>{t('Custom override')}</Badge>
                    )}
                  </div>
                  <Label className='flex items-center gap-2 text-xs font-normal'>
                    <Switch
                      disabled={props.disabled}
                      checked={!!override}
                      onCheckedChange={(checked) => {
                        if (checked) {
                          upsertOverride({
                            key: cycle.key,
                            limit: defaultOverrideLimit(
                              scaledLimit,
                              cycle.limit
                            ),
                            unlimited: cycle.unlimited,
                          })
                        } else {
                          removeOverride(cycle.key)
                        }
                      }}
                    />
                    {t('Override cycle quota')}
                  </Label>
                </div>

                {override && (
                  <div className='space-y-2 border-t pt-2'>
                    <Label className='flex items-center gap-2 text-xs font-normal'>
                      <Switch
                        disabled={props.disabled}
                        checked={override.unlimited}
                        onCheckedChange={(unlimited) =>
                          upsertOverride({
                            ...override,
                            unlimited,
                            limit: unlimited
                              ? 0
                              : defaultOverrideLimit(
                                  override.limit,
                                  scaledLimit
                                ),
                          })
                        }
                      />
                      {t('Unlimited')}
                    </Label>
                    {!override.unlimited && (
                      <QuotaAmountInput
                        id={`${props.idPrefix}-override-${cycle.key}`}
                        label={`${cycle.name} ${t('Override cycle quota')}`}
                        disabled={props.disabled}
                        value={override.limit}
                        onChange={(limit) =>
                          upsertOverride({ ...override, limit })
                        }
                      />
                    )}
                  </div>
                )}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
