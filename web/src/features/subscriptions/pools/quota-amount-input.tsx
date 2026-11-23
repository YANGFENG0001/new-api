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

import { Combobox } from '@/components/ui/combobox'
import { Input } from '@/components/ui/input'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

// Currency is an input convenience, never an inferred upstream capacity or unit.
// Values crossing the API boundary remain exact integer site quota units.
export function QuotaAmountInput(props: {
  id: string
  label: string
  value: number
  onChange: (quota: number) => void
  disabled?: boolean
}) {
  const { t } = useTranslation()
  const currency = useSystemConfigStore((s) => s.config.currency)
  const [unit, setUnit] = useState('quota')
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
  const amount = Number.isFinite(props.value)
    ? String(props.value / factor)
    : ''
  return (
    <div className='space-y-2'>
      <div className='grid min-w-0 grid-cols-[minmax(0,1fr)_minmax(7rem,auto)] gap-2'>
        <Input
          id={props.id}
          aria-label={props.label}
          type='number'
          min={0}
          max={Number.MAX_SAFE_INTEGER / factor}
          step={unit === 'quota' ? 1 : 'any'}
          disabled={props.disabled}
          value={draft ?? amount}
          onFocus={() => setDraft(amount)}
          onBlur={() => setDraft(null)}
          onChange={(e) => {
            setDraft(e.target.value)
            const input = Number(e.target.value)
            const quota = Math.round(input * factor)
            const valid =
              input >= 0 &&
              Number.isSafeInteger(quota) &&
              (unit !== 'quota' || Number.isInteger(input))
            props.onChange(valid ? quota : Number.NaN)
          }}
        />
        <Combobox
          aria-label={`${props.label} · ${t('Input unit')}`}
          value={unit}
          disabled={props.disabled}
          options={[
            { value: 'quota', label: t('Quota units') },
            { value: 'USD', label: t('USD ($)') },
            { value: 'CNY', label: t('CNY (￥)') },
          ]}
          onValueChange={(v) => {
            if (v) setUnit(v)
            setDraft(null)
          }}
        />
      </div>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Currency inputs use the site exchange rate and are saved as quota units. Upstream percentages do not define a monetary capacity.'
        )}
      </p>
    </div>
  )
}
