import { useTranslation } from 'react-i18next'

import type { AuditLogItem } from './types'

export function RuntimeAuditDetails(props: { item: AuditLogItem }) {
  const { t } = useTranslation()
  if (props.item.action !== 'user_control.runtime.request') return null
  const info = props.item.other?.audit_info
  if (!info || typeof info !== 'object' || !('params' in info)) return null
  const params = info.params
  if (!params || typeof params !== 'object') return null
  const labels: Record<string, string> = {
    original_model: t('Original model'),
    actual_model: t('Actual model'),
    channel_id: t('Channel ID'),
    rules: t('Matched rule IDs'),
    schedule_hits: t('Schedule hits'),
    simulated: t('Simulated response'),
    actions: t('Request and response actions'),
  }
  const actionLabels: Record<string, string> = {
    request_body: t('Request body rewrite'),
    route_model: t('Route to model'),
    reasoning_effort: t('Reasoning effort override'),
    prompt: t('Prompt concatenation'),
    response: t('Response replacement (simulation)'),
  }
  return (
    <dl className='min-w-48 space-y-1 text-xs [overflow-wrap:anywhere]'>
      {Object.entries(labels).map(([key, label]) => {
        const raw = (params as Record<string, unknown>)[key]
        if (typeof raw !== 'string' || !raw) return null
        let value = raw
        if (key === 'simulated') value = raw === 'true' ? t('Yes') : t('No')
        if (key === 'actions' || key === 'rules' || key === 'schedule_hits') {
          try {
            const parsed: unknown = JSON.parse(raw)
            if (Array.isArray(parsed)) {
              value =
                parsed
                  .filter((v): v is string => typeof v === 'string')
                  .map((v) => (key === 'actions' ? (actionLabels[v] ?? v) : v))
                  .join(', ') || t('None')
            }
          } catch {
            value = t('None')
          }
        }
        return (
          <div key={key}>
            <dt className='text-muted-foreground inline'>{label}: </dt>
            <dd className='inline'>{value}</dd>
          </div>
        )
      })}
    </dl>
  )
}
