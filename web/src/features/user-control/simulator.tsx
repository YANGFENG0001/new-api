import { useQuery } from '@tanstack/react-query'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { toIntlLocale } from '@/i18n/languages'
import {
  getServerErrorMessage,
  requireServerSuccess,
} from '@/lib/server-error-message'

import { simulate } from './api'
import { LimitsSummary } from './limits-summary'
import { formatPoolDecision } from './pool-decision'
import { UserPicker } from './user-picker'

export function Simulator() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const inputId = useId()
  const [selected, setSelected] = useState<number[]>([])
  const [at, setAt] = useState('')
  const [request, setRequest] = useState<{
    userId: number
    at: string
    attempt: number
  } | null>(null)
  const simulation = useQuery({
    queryKey: ['user-control', 'simulation', request],
    queryFn: async () => {
      if (!request) return null
      // datetime-local has no offset; transmit the selected instant as RFC3339.
      const result = requireServerSuccess(
        await simulate(
          request.userId,
          request.at ? new Date(request.at).toISOString() : undefined
        )
      )
      if (!result.data) throw new Error(t('Simulation failed'))
      return result.data
    },
    enabled: request !== null,
    retry: false,
    refetchOnWindowFocus: false,
    meta: { errorToast: false },
  })
  const result = request ? simulation.data : null
  const sourceLabel = (source: string): string => {
    const split = source.indexOf(':')
    const kind = split < 0 ? source : source.slice(0, split)
    let label = t(kind)
    if (kind === 'list') label = t('Control list')
    if (kind === 'subscription') label = t('Subscription plan')
    if (
      kind === 'override' ||
      kind === 'application_user' ||
      kind === 'override_reset'
    ) {
      label = t('Personal settings')
    }
    if (kind === 'application_list') label = t('Control list')
    if (kind === 'application_plan') label = t('Subscription plan')
    return split < 0 ? label : `${label}: ${source.slice(split + 1)}`
  }

  return (
    <div className='min-w-0 space-y-4'>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Preview uses current configuration. Later edits can change future results.'
        )}
      </p>
      <UserPicker
        selected={selected}
        onChange={(ids) => {
          setSelected(ids)
          setRequest(null)
        }}
      />
      <form
        className='flex min-w-0 flex-wrap items-end gap-2'
        onSubmit={(event) => {
          event.preventDefault()
          if (!selected[0]) return
          setRequest({
            userId: selected[0],
            at,
            attempt: (request?.attempt ?? 0) + 1,
          })
        }}
      >
        <div className='min-w-0 flex-1 space-y-1'>
          <Label htmlFor={inputId}>{t('Evaluate at (optional)')}</Label>
          <Input
            id={inputId}
            type='datetime-local'
            value={at}
            onChange={(event) => {
              setAt(event.target.value)
              setRequest(null)
            }}
          />
        </div>
        <Button type='submit' disabled={!selected[0] || simulation.isFetching}>
          {t('Simulate')}
        </Button>
      </form>
      {request && simulation.isFetching && (
        <LoadingState className='min-h-0 py-6' />
      )}
      {request && simulation.isError && (
        <div role='alert'>
          <ErrorState
            className='min-h-0 py-4'
            title={t('Simulation failed')}
            description={getServerErrorMessage(simulation.error)}
            onRetry={() => void simulation.refetch()}
          />
        </div>
      )}
      {result && (
        <section
          aria-label={t('Simulation result')}
          className='min-w-0 space-y-3 rounded-lg border p-3 [overflow-wrap:anywhere] break-words'
        >
          <div className='flex flex-wrap gap-2 text-sm'>
            <Badge variant={result.has_policy ? 'default' : 'outline'}>
              {result.has_policy ? t('Policy active') : t('No policy')}
            </Badge>
            <Badge variant='outline'>{result.timezone}</Badge>
            <span className='text-muted-foreground'>
              {new Date(result.resolved_at).toLocaleString(locale)}
            </span>
          </div>
          <p className='text-sm'>
            {t('User')} #{result.user_id}
          </p>
          <p className='text-sm'>
            {t('Sources')}:{' '}
            {(result.sources ?? []).map(sourceLabel).join(', ') || t('None')}
          </p>
          <p className='text-sm'>
            {t('Schedule hits')}:{' '}
            {(result.schedule_hits ?? [])
              .map((hit) =>
                hit === 'rule' ? t('Time window') : formatPoolDecision(hit, t)
              )
              .join(', ') || t('None')}
          </p>
          <LimitsSummary value={result.limits} />
          <div className='text-muted-foreground space-y-1 text-xs'>
            {(result.details ?? []).map((detail) => (
              <p
                key={`${detail.source}-${detail.source_id}-${detail.policy_id}-${detail.pool_id ?? 0}`}
              >
                {sourceLabel(detail.source)} #{detail.source_id} ·{' '}
                {detail.policy_name} · {t('Priority')} {detail.priority} ·{' '}
                {detail.active ? t('Active') : t('Inactive')}
              </p>
            ))}
          </div>
        </section>
      )}
    </div>
  )
}
