import { useTranslation } from 'react-i18next'

import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import type { Limits } from './types'

export function LimitsSummary(props: { value?: Limits; compact?: boolean }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const labels: Record<string, string> = {
    blocked: t('Block all requests'),
    allow_relay: t('Allow relay'),
    allow_playground: t('Allow Playground'),
    rate: t('Rate limits'),
    requests: t('Request quotas'),
    usage: t('Usage quotas'),
    models: t('Model access'),
    channels: t('Channel access'),
    api_keys: t('API key limits'),
    rpm: t('Requests per minute'),
    rpd: t('Requests per day'),
    concurrent: t('Concurrent requests'),
    burst: t('Burst capacity'),
    daily: t('Daily requests'),
    monthly: t('Monthly requests'),
    daily_quota: t('Daily quota (quota units)'),
    monthly_quota: t('Monthly quota (quota units)'),
    input_tokens: t('Input tokens'),
    output_tokens: t('Output tokens'),
    allow: t('Allowed models'),
    block: t('Blocked models'),
    allow_ids: t('Allowed channel IDs'),
    block_ids: t('Blocked channel IDs'),
    allow_types: t('Allowed channel types'),
    block_types: t('Blocked channel types'),
    allow_groups: t('Allowed groups'),
    max_active: t('Max active keys'),
    max_total: t('Max total keys'),
  }
  const entries = Object.entries(props.value ?? {}).filter(
    ([, value]) => value !== undefined
  )
  if (entries.length === 0) return <span>{t('None')}</span>
  if (props.compact) {
    return (
      <span>{entries.map(([key]) => labels[key] ?? t(key)).join(', ')}</span>
    )
  }

  const rows: { label: string; value: unknown }[] = []
  for (const [key, value] of entries) {
    if (typeof value === 'object' && value !== null && !Array.isArray(value)) {
      for (const [field, fieldValue] of Object.entries(value)) {
        if (fieldValue !== undefined) {
          rows.push({
            label: `${labels[key] ?? t(key)} · ${labels[field] ?? t(field)}`,
            value: fieldValue,
          })
        }
      }
    } else rows.push({ label: labels[key] ?? t(key), value })
  }
  return (
    <dl className='grid min-w-0 gap-2 text-sm sm:grid-cols-2'>
      {rows.map((row) => {
        let value = String(row.value ?? '')
        if (typeof row.value === 'boolean') {
          value = row.value ? t('Yes') : t('No')
        }
        if (typeof row.value === 'number') {
          value = formatNumber(row.value, locale)
        }
        if (Array.isArray(row.value)) value = row.value.join(', ') || t('None')
        return (
          <div
            key={row.label}
            className='min-w-0 rounded-md border p-2 [overflow-wrap:anywhere] break-words'
          >
            <dt className='text-muted-foreground text-xs'>{row.label}</dt>
            <dd>{value}</dd>
          </div>
        )
      })}
    </dl>
  )
}
