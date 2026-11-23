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
import { AlertCircle, CheckCircle2, Clock3, Info, Server } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { StatusBadge } from '@/components/status-badge'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
  PopoverTitle,
  PopoverDescription,
} from '@/components/ui/popover'
import { Progress } from '@/components/ui/progress'
import { toIntlLocale } from '@/i18n/languages'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatNumber, formatTimestampRelative } from '@/lib/format'
import { useSystemConfigStore } from '@/stores/system-config-store'

import {
  syncStaleSeconds,
  reportedSyncAccounts,
  syncCycleName,
  type SyncAccount,
  type SyncSource,
} from './sync-api'

export function SyncSourceStatus(props: { source: SyncSource }) {
  const { t } = useTranslation()
  let fetchStatus = props.source.fetch_status ?? 'Awaiting observation'
  if (!props.source.fetch_status && props.source.last_fetch) {
    fetchStatus =
      Date.now() / 1000 - props.source.last_fetch >
      syncStaleSeconds(props.source)
        ? 'Synchronization is delayed'
        : 'Synchronization is on schedule'
  }
  return (
    <div className='space-y-2'>
      <div className='flex flex-wrap items-center gap-2'>
        <SyncHealth
          healthy={
            !props.source.last_error &&
            fetchStatus === 'Synchronization is on schedule'
          }
          label={props.source.last_error ? undefined : fetchStatus}
          message={
            props.source.last_error ||
            (fetchStatus === 'Synchronization is delayed'
              ? fetchStatus
              : undefined)
          }
        />
        <SyncHealth
          healthy={
            props.source.observation_status ===
            'Upstream observations are current'
          }
          label={
            props.source.observation_status ===
            'Upstream observations are current'
              ? 'Snapshot within freshness window'
              : props.source.observation_status
          }
          message={props.source.observation_status}
        />
      </div>
      {props.source.reported_cycles != null &&
        props.source.reported_cycles > 0 && (
          <p className='text-muted-foreground text-xs'>
            {t('{{fresh}} / {{total}} current windows', {
              fresh: props.source.fresh_cycles ?? 0,
              total: props.source.reported_cycles,
            })}
          </p>
        )}
      {!!props.source.cycle_statuses?.length && (
        <div
          aria-label={t('Upstream cycle status')}
          className='flex flex-col gap-1 text-xs'
        >
          {props.source.cycle_statuses.map((status) => (
            <div key={status.key} className='flex flex-wrap items-center gap-2'>
              <span className='text-muted-foreground'>
                {status.name || status.key}:{' '}
                {t('{{fresh}} / {{total}} current windows', {
                  fresh: status.fresh_cycles,
                  total: status.reported_cycles,
                })}
              </span>
              {!!status.hint_samples && (
                <Badge variant='outline'>{t('Sampling time incomplete')}</Badge>
              )}
            </div>
          ))}
        </div>
      )}
      {!!props.source.stale_accounts?.length && (
        <p className='text-warning text-xs'>
          {t('Some upstream cycles need attention for: {{accounts}}', {
            accounts: props.source.stale_accounts.join(', '),
          })}
        </p>
      )}
      <p className='text-muted-foreground text-xs'>
        {t(
          'Poll every {{interval}}s; observations expire after {{threshold}}s.',
          {
            interval: props.source.interval_seconds,
            threshold: syncStaleSeconds(props.source),
          }
        )}
      </p>
    </div>
  )
}

export function SyncTimestamp(props: {
  value?: number
  fallback?: string
  relative?: boolean
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const date = new Date((props.value ?? 0) * 1000)
  if (!props.value || props.value < 0 || !Number.isFinite(date.getTime())) {
    return (
      <span className='text-muted-foreground'>
        {t(props.fallback ?? 'Unknown')}
      </span>
    )
  }
  return (
    <span className='inline-flex min-w-0 flex-col gap-0.5'>
      <time
        dateTime={date.toISOString()}
        className='text-foreground tabular-nums'
      >
        {date.toLocaleString(locale)}
      </time>
      {props.relative && (
        <span className='text-muted-foreground text-xs'>
          {formatTimestampRelative(props.value, 'seconds', locale)}
        </span>
      )}
    </span>
  )
}

export function SyncHealth(props: {
  message?: string
  healthy?: boolean
  label?: string
}) {
  const { t } = useTranslation()
  let label = props.label ?? 'Needs attention'
  let variant: 'warning' | 'success' | 'neutral' = 'warning'
  let icon = AlertCircle
  if (props.healthy) {
    label = props.label ?? 'Up to date'
    variant = 'success'
    icon = CheckCircle2
  } else if (!props.message || props.message === 'No upstream observation') {
    label = props.label ?? 'Awaiting observation'
    variant = 'neutral'
    icon = Clock3
  } else if (props.message.toLowerCase().includes('stale')) {
    label = props.label ?? 'Stale data'
    icon = Clock3
  }
  return (
    <span className='inline-flex max-w-full items-center gap-1'>
      <StatusBadge
        label={t(label)}
        variant={variant}
        icon={icon}
        copyable={false}
      />
      {props.message && !props.healthy && (
        <Popover>
          <PopoverTrigger
            render={
              <Button
                type='button'
                variant='ghost'
                size='icon'
                className='size-6 shrink-0'
                aria-label={t('View status details')}
              />
            }
          >
            <Info className='size-3.5' aria-hidden='true' />
          </PopoverTrigger>
          <PopoverContent className='max-w-[calc(100vw-2rem)] break-words'>
            <PopoverTitle>{t('Status')}</PopoverTitle>
            <PopoverDescription>{t(props.message)}</PopoverDescription>
          </PopoverContent>
        </Popover>
      )}
    </span>
  )
}

export function SyncUsageCard(props: {
  name: string
  utilization: number | null
  windowEnd: number
  sampledAt: number
  sampleTimeQuality?: 'exact' | 'hint'
  issueCode?: string
  fetchedAt?: number
  stale: boolean
  issue?: string
  estimatedUtilization?: number | null
  localDelta?: number
  scope?: 'Shared' | 'Independent'
  historical?: boolean
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const currencyDisplayType = useSystemConfigStore(
    (state) => state.config.currency.quotaDisplayType
  )
  const sampleTimeQuality = props.sampleTimeQuality ?? 'exact'
  const reported =
    props.utilization != null &&
    Number.isFinite(props.utilization) &&
    props.utilization >= 0 &&
    props.issueCode !== 'invalid_sample' &&
    props.issueCode !== 'invalid_utilization'
  const localMetered =
    props.localDelta != null && Number.isFinite(props.localDelta)
  const estimated =
    !props.historical &&
    props.estimatedUtilization != null &&
    Number.isFinite(props.estimatedUtilization) &&
    props.estimatedUtilization >= 0
  const utilization =
    reported && props.utilization != null ? props.utilization * 100 : null
  const estimatedUtilization =
    estimated && props.estimatedUtilization != null
      ? props.estimatedUtilization * 100
      : null
  const resetPassed =
    props.windowEnd > 0 && props.windowEnd <= Date.now() / 1000
  const expired = resetPassed || props.issueCode === 'expired_reset'
  const issueMessages: Record<string, string> = {
    missing_reset: 'Upstream reset time is missing',
    expired_reset: 'Upstream reset time has expired',
    invalid_sample: 'Upstream sample is invalid',
    sample_time_incomplete: 'Upstream sample time is incomplete',
  }
  const hasHardIssue =
    !expired &&
    ((!!props.issueCode && props.issueCode !== 'sample_time_incomplete') ||
      (!!props.issue &&
        props.issue !== 'Upstream sample time is incomplete' &&
        props.issue !== 'Upstream observation is stale'))
  const stale = !expired && props.stale
  const sampleIncomplete =
    !expired &&
    !hasHardIssue &&
    !stale &&
    (sampleTimeQuality === 'hint' ||
      props.issueCode === 'sample_time_incomplete')

  let issue: string | undefined
  if (expired) {
    issue = 'Upstream reset time has expired'
  } else if (hasHardIssue) {
    issue =
      (props.issueCode ? issueMessages[props.issueCode] : undefined) ||
      props.issue
  } else if (stale) {
    issue = 'Upstream observation is stale'
  } else if (sampleIncomplete) {
    issue = 'Upstream sample time is incomplete'
  }
  if (!reported && !localMetered) return null

  let healthLabel: string | undefined
  if (!reported) healthLabel = 'Not reported'
  else if (expired) healthLabel = 'Expired'
  else if (hasHardIssue) healthLabel = undefined
  else if (stale) healthLabel = 'Stale data'
  else if (sampleIncomplete) healthLabel = 'Sampling time incomplete'

  let statusWarning: string | undefined
  if (expired) statusWarning = 'Upstream reset time has expired'
  else if (stale) statusWarning = 'Upstream observation is stale'

  return (
    <section
      aria-label={props.name}
      className='bg-card flex h-full min-w-0 flex-col gap-3 rounded-xl border p-4'
    >
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div className='flex min-w-0 flex-wrap items-center gap-2'>
          <h4 className='min-w-0 font-medium break-words'>{props.name}</h4>
          {props.historical && (
            <Badge variant='outline'>{t('Historical upstream snapshot')}</Badge>
          )}
          {props.scope && <Badge variant='secondary'>{t(props.scope)}</Badge>}
        </div>
        <SyncHealth
          healthy={
            reported && !expired && !hasHardIssue && !stale && !sampleIncomplete
          }
          message={issue}
          label={healthLabel}
        />
      </div>
      <div className='flex items-baseline gap-2'>
        <strong className='text-2xl font-semibold tabular-nums'>
          {utilization == null ? '—' : `${formatNumber(utilization, locale)}%`}
        </strong>
        <span className='text-muted-foreground text-xs'>
          {t('Upstream utilization')}
        </span>
      </div>
      {(localMetered || estimated) && (
        <dl className='bg-muted/30 grid gap-2 rounded-lg p-3 text-xs sm:grid-cols-2'>
          {localMetered && (
            <div className='flex flex-col gap-1'>
              <dt className='text-muted-foreground'>
                {t('Local metering since calibration')}
              </dt>
              <dd className='font-medium tabular-nums'>
                {currencyDisplayType === 'TOKENS'
                  ? formatNumber(props.localDelta ?? 0, locale)
                  : formatQuotaWithCurrency(props.localDelta ?? 0, {
                      locale,
                      digitsLarge: 2,
                      digitsSmall: 4,
                      abbreviate: false,
                    })}
              </dd>
            </div>
          )}
          {estimatedUtilization != null && (
            <div className='flex flex-col gap-1'>
              <dt className='text-muted-foreground'>
                {t('Estimated utilization after local metering')}
              </dt>
              <dd className='font-medium tabular-nums'>
                {formatNumber(estimatedUtilization, locale)}%
              </dd>
            </div>
          )}
        </dl>
      )}
      {utilization == null ? (
        <div aria-hidden='true' className='bg-muted h-1 rounded-full' />
      ) : (
        <Progress
          aria-label={props.name}
          value={Math.min(100, utilization)}
          className={expired || stale || hasHardIssue ? 'opacity-50' : ''}
        />
      )}
      <dl className='space-y-2.5 text-xs'>
        <div className='flex flex-wrap items-start justify-between gap-2'>
          <dt className='text-muted-foreground flex items-center gap-1.5'>
            <Clock3 className='size-3.5' aria-hidden='true' />
            {t('Next reset time')}
          </dt>
          <dd className='text-end'>
            <SyncTimestamp
              value={props.windowEnd}
              fallback='Reset time unavailable'
              relative={props.windowEnd > 0}
            />
          </dd>
        </div>
        <div className='flex flex-wrap items-start justify-between gap-2'>
          <dt className='text-muted-foreground'>{t('Observation time')}</dt>
          <dd className='text-end'>
            <SyncTimestamp value={props.sampledAt} fallback='Not reported' />
          </dd>
        </div>
        {props.fetchedAt != null && (
          <div className='flex flex-wrap items-start justify-between gap-2'>
            <dt className='text-muted-foreground'>{t('Last fetch')}</dt>
            <dd className='text-end'>
              <SyncTimestamp value={props.fetchedAt} fallback='Never' />
            </dd>
          </div>
        )}
      </dl>
      {statusWarning && (
        <p
          role='status'
          className='text-warning mt-auto flex items-center gap-1.5 pt-1 text-xs'
        >
          <Clock3 className='size-3.5 shrink-0' aria-hidden='true' />
          {t(statusWarning)}
        </p>
      )}
    </section>
  )
}

export function SyncAccounts(props: {
  accounts: SyncAccount[]
  staleSeconds: number
  fetchedAt?: number
}) {
  const { t } = useTranslation()
  const now = Math.floor(Date.now() / 1000)
  const accounts = reportedSyncAccounts(props.accounts)
  if (!accounts.length) {
    return (
      <EmptyState
        className='min-h-40'
        icon={Server}
        title={t('No upstream observation')}
        description={t(
          'Read the source to inspect account usage and reset times.'
        )}
      />
    )
  }
  return (
    <div className='space-y-4'>
      {accounts.map((account) => (
        <section
          key={account.id}
          aria-label={account.name || account.id}
          className='min-w-0 space-y-3 rounded-xl border p-4'
        >
          <div className='flex flex-wrap items-start justify-between gap-3'>
            <div className='flex min-w-0 items-start gap-2'>
              <Server
                className='text-muted-foreground mt-0.5 size-4 shrink-0'
                aria-hidden='true'
              />
              <div className='min-w-0'>
                <h3 className='font-semibold break-words'>
                  {account.name || account.id}
                </h3>
                <p className='text-muted-foreground font-mono text-xs break-all'>
                  {account.id}
                </p>
              </div>
            </div>
            {account.expiry > 0 && (
              <div className='text-xs'>
                <span className='text-muted-foreground'>
                  {t('Expiry time')} ·{' '}
                </span>
                <SyncTimestamp value={account.expiry} />
              </div>
            )}
          </div>
          {props.fetchedAt != null && (
            <p className='text-muted-foreground text-xs'>
              {t('Last fetch')}:{' '}
              <SyncTimestamp value={props.fetchedAt} fallback='Never' />
            </p>
          )}
          {account.cycles.some(
            (cycle) =>
              cycle.sampled_at > 0 &&
              now - cycle.sampled_at > props.staleSeconds
          ) && (
            <p role='status' className='text-warning text-xs'>
              {t(
                'Source polling only reads the upstream cache. This account has an old observation; another poll does not make it fresh. Check the upstream cache updater or per-cycle observation time mapping.'
              )}
            </p>
          )}
          <div className='grid min-w-0 gap-3 md:grid-cols-2 2xl:grid-cols-3'>
            {account.cycles.map((cycle) => {
              const cycleStale =
                cycle.sampled_at > 0
                  ? now - cycle.sampled_at > props.staleSeconds
                  : props.fetchedAt != null &&
                    props.fetchedAt > 0 &&
                    now - props.fetchedAt > props.staleSeconds
              return (
                <SyncUsageCard
                  key={cycle.key}
                  name={syncCycleName(cycle.name || cycle.key, cycle.seconds)}
                  utilization={cycle.utilization}
                  windowEnd={cycle.window_end}
                  sampledAt={cycle.sampled_at}
                  issue={cycle.error}
                  sampleTimeQuality={cycle.sample_time_quality}
                  issueCode={cycle.issue_code}
                  stale={cycleStale}
                />
              )
            })}
          </div>
          {!account.cycles.length && (
            <p className='text-muted-foreground text-sm'>
              {t('No upstream observation')}
            </p>
          )}
        </section>
      ))}
    </div>
  )
}
