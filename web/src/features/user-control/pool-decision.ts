import type { TFunction } from 'i18next'

export function formatPoolDecision(hit: string, t: TFunction): string {
  if (!hit.startsWith('pool:')) return hit
  const fields = hit.split(':')
  const get = (key: string) => {
    const index = fields.indexOf(key)
    return index >= 0 ? fields[index + 1] : undefined
  }
  const statuses: Record<string, string> = {
    outside_window: t('Outside scheduled windows'),
    miss: t('Pool probability missed'),
    disabled: t('Selected policy is disabled'),
    selected: t('Policy selected'),
  }
  const status = statuses[fields.at(-1) ?? ''] ?? t('Policy selected')
  return t(
    'Pool #{{id}}: {{status}}; policy #{{policy}}; segment {{segment}}',
    {
      id: fields[1],
      status,
      policy: get('policy') ?? '-',
      segment: get('segment') ?? '-',
    }
  )
}
