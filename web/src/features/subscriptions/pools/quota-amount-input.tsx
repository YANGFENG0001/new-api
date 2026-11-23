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

import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { toIntlLocale } from '@/i18n/languages'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatNumber } from '@/lib/format'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { useQuotaInputUnit } from './use-quota-input-unit'

// Currency is an input convenience, never an inferred upstream capacity or unit.
// Values crossing the API boundary remain exact integer site quota units.
export function QuotaAmountInput(props: {
  id: string
  name?: string
  preferenceKey?: string
  label: string
  value: number
  onChange: (quota: number) => void
  disabled?: boolean
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const currency = useSystemConfigStore((state) => state.config.currency)
  const { unit, setUnit } = useQuotaInputUnit(
    props.preferenceKey || props.name || props.id,
    currency.quotaDisplayType
  )
  const [draft, setDraft] = useState<string | null>(null)
  const quotaPerUnit =
    currency.quotaPerUnit > 0 && Number.isFinite(currency.quotaPerUnit)
      ? currency.quotaPerUnit
      : DEFAULT_CURRENCY_CONFIG.quotaPerUnit
  const rate =
    currency.usdExchangeRate > 0 && Number.isFinite(currency.usdExchangeRate)
      ? currency.usdExchangeRate
      : DEFAULT_CURRENCY_CONFIG.usdExchangeRate
  let factor = 1
  if (unit === 'USD') factor = quotaPerUnit
  if (unit === 'CNY') factor = quotaPerUnit / rate
  const displayedAmount = Number.isFinite(props.value)
    ? Number((props.value / factor).toFixed(10)).toString()
    : ''
  const amount = draft ?? displayedAmount
  const unitOptions = [
    { value: 'quota', label: t('Quota units') },
    { value: 'USD', label: t('USD ($)') },
    { value: 'CNY', label: t('CNY (￥)') },
  ]
  const showConversion = Number.isFinite(props.value) && props.value > 0
  let conversionText: string | null = null
  if (showConversion) {
    if (unit !== 'quota') {
      conversionText = t('Stored as {{quota}} quota units', {
        quota: formatNumber(props.value, locale),
      })
    } else if (currency.quotaDisplayType !== 'TOKENS') {
      conversionText = t('Equivalent to {{amount}}', {
        amount: formatQuotaWithCurrency(props.value, {
          locale,
          digitsLarge: 2,
          digitsSmall: 4,
          abbreviate: false,
        }),
      })
    }
  }
  return (
    <div className='space-y-1.5'>
      <div className='grid min-w-0 grid-cols-[minmax(0,1fr)_auto] gap-2'>
        <Input
          id={props.id}
          name={props.name ?? props.preferenceKey ?? props.id}
          aria-label={props.label}
          type='number'
          min={0}
          max={Number.MAX_SAFE_INTEGER / factor}
          step={unit === 'quota' ? 1 : 'any'}
          disabled={props.disabled}
          value={amount}
          onFocus={() => setDraft(displayedAmount)}
          onBlur={() => setDraft(null)}
          onChange={(event) => {
            const text = event.target.value
            setDraft(text)
            const input = text.trim() === '' ? Number.NaN : Number(text)
            const quota = Math.round(input * factor)
            const valid =
              Number.isFinite(input) &&
              input >= 0 &&
              Number.isSafeInteger(quota) &&
              quota >= 0 &&
              (unit !== 'quota' || Number.isInteger(input))
            props.onChange(valid ? quota : Number.NaN)
          }}
        />
        <Select
          items={unitOptions}
          value={unit}
          disabled={props.disabled}
          onValueChange={(value) => {
            if (value === 'quota' || value === 'USD' || value === 'CNY') {
              setUnit(value)
            }
            setDraft(null)
          }}
        >
          <SelectTrigger
            aria-label={`${props.label} · ${t('Input unit')}`}
            className='min-w-[7.5rem] shrink-0'
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent alignItemWithTrigger={false}>
            <SelectGroup>
              {unitOptions.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
      </div>
      {conversionText && (
        <p className='text-muted-foreground text-xs tabular-nums'>
          {conversionText}
        </p>
      )}
      <p className='text-muted-foreground text-xs'>
        {t(
          'Currency inputs use the site exchange rate and are saved as quota units. Upstream percentages do not define a monetary capacity.'
        )}
      </p>
    </div>
  )
}
