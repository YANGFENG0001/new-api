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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { DateTimePicker } from '@/components/datetime-picker'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { MultiSelect } from '@/components/multi-select'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import {
  poolAPI,
  type Allocation,
  type Assignment,
  type AssignmentPreview,
  type AssignmentResult,
  type CycleOverride,
  type Usage,
} from './api'
import { CycleProgress } from './cycle-progress'
import { PoolQuotaAllocationFields } from './pool-quota-allocation'
import { formatMultiplier } from './quota-share'
import { SyncTimestamp } from './sync-usage'

export function InstanceAllocations(props: {
  userId: number
  subscriptions: {
    id: number
    plan_id: number
    status: string
    plan_title?: string
  }[]
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const client = useQueryClient()
  const caps = useQuery({
    queryKey: ['subscription-pools', 'capabilities'],
    queryFn: poolAPI.capabilities,
  })
  const allocations = useQuery({
    queryKey: ['subscription-pools', 'allocations', props.userId],
    queryFn: () => poolAPI.allocations(props.userId),
    enabled: !!caps.data?.read,
  })
  const pools = useQuery({
    queryKey: ['subscription-pools', 'list'],
    queryFn: poolAPI.list,
    enabled: !!caps.data?.read,
  })
  const [selected, setSelected] = useState<string[]>([])
  const [poolID, setPoolID] = useState('')
  const [end, setEnd] = useState<Date>()
  const [customExpiry, setCustomExpiry] = useState(false)
  const [customQuota, setCustomQuota] = useState(false)
  const [quotaMultiplier, setQuotaMultiplier] = useState(1)
  const [cycleOverrides, setCycleOverrides] = useState<CycleOverride[]>([])
  const [previews, setPreviews] = useState<AssignmentPreview[] | null>(null)
  const [results, setResults] = useState<AssignmentResult[]>([])
  const [reset, setReset] = useState<{ id: number; key: string } | null>(null)
  const batchAllowed = !!caps.data?.batch_migrate

  function requestedAssignments(confirm: boolean): Assignment[] {
    return selected.map((id) => ({
      subscription_id: Number(id),
      pool_id: Number(poolID),
      version:
        allocations.data?.allocations.find(
          (a) => a.subscription_id === Number(id)
        )?.version ?? 0,
      pool_version:
        pools.data?.find((p) => p.id === Number(poolID))?.version ?? 0,
      end_time: customExpiry && end ? Math.floor(end.getTime() / 1000) : 0,
      confirm,
      ...(customQuota
        ? {
            quota_multiplier: quotaMultiplier,
            cycle_overrides: cycleOverrides,
          }
        : {}),
    }))
  }
  const preview = useMutation({
    mutationFn: async () => {
      const items = requestedAssignments(false)
      if (items.length === 1) {
        const result = await poolAPI.assign(items[0])
        if (!result) throw new Error('Invalid parameters')
        return [
          {
            subscription_id: items[0].subscription_id,
            success: true,
            message: '',
            preview: result,
          },
        ]
      }
      return poolAPI.batchAssign(items)
    },
    onSuccess: (rows) => {
      setResults(rows.filter((r) => !r.success))
      setPreviews(
        rows.flatMap((r) => (r.success && r.preview ? [r.preview] : []))
      )
    },
  })
  const assignment = useMutation({
    mutationFn: async () => {
      const items = (previews ?? []).map((p) => ({
        subscription_id: p.subscription_id,
        pool_id: p.target_pool_id,
        version: p.current.version || 0,
        pool_version: p.pool_version,
        end_time: customExpiry ? p.end_time : 0,
        confirm: true,
        usage_digest: p.legacy_carry?.digest,
        ...(customQuota
          ? {
              quota_multiplier: p.quota_multiplier ?? quotaMultiplier,
              cycle_overrides: p.cycle_overrides ?? cycleOverrides,
            }
          : {}),
      }))
      if (items.length === 1) {
        await poolAPI.assign(items[0])
        return [
          {
            subscription_id: items[0].subscription_id,
            success: true,
            message: '',
          },
        ]
      }
      return poolAPI.batchAssign(items)
    },
    onSuccess: (rows) => {
      const successes = new Set(
        rows.filter((r) => r.success).map((r) => String(r.subscription_id))
      )
      setResults((previous) => [
        ...previous.filter(
          (r) => !rows.some((n) => n.subscription_id === r.subscription_id)
        ),
        ...rows,
      ])
      setSelected((previous) => previous.filter((id) => !successes.has(id)))
      setPreviews(null)
      void client.invalidateQueries({ queryKey: ['subscription-pools'] })
    },
  })
  const resetMutation = useMutation({
    mutationFn: () => poolAPI.resetPersonal(reset?.id ?? 0, reset?.key ?? ''),
    onSuccess: () => {
      setReset(null)
      void client.invalidateQueries({ queryKey: ['subscription-pools'] })
    },
  })
  if (!caps.data?.read) return null
  if (allocations.isLoading || pools.isLoading) return <LoadingState />
  if (allocations.isError || pools.isError) {
    return (
      <ErrorState
        onRetry={() => {
          void allocations.refetch()
          void pools.refetch()
        }}
      />
    )
  }
  const selectedPool = pools.data?.find((p) => p.id === Number(poolID))
  return (
    <section className='space-y-3 rounded-lg border p-4'>
      <h3 className='font-semibold'>{t('Actual pool allocation')}</h3>
      {(allocations.data?.subscriptions ?? []).some(
        (u) => !u.managed && u.status === 'active'
      ) && (
        <p role='status' className='text-warning text-sm'>
          {t(
            'Active legacy subscriptions are not linked to a pool. Select the instances below to migrate them; changing plan defaults does not migrate existing subscriptions.'
          )}
        </p>
      )}
      {(allocations.data?.subscriptions ?? [])
        .filter((u) => u.managed)
        .map((u) => {
          const alloc = allocations.data?.allocations.find(
            (a) => a.subscription_id === u.subscription_id
          )
          return (
            <InstanceUsage
              key={u.subscription_id}
              usage={u}
              allocation={alloc}
              poolName={pools.data?.find((p) => p.id === alloc?.pool_id)?.name}
              onReset={
                caps.data.personal_reset && u.status === 'active'
                  ? (key) => setReset({ id: u.subscription_id, key })
                  : undefined
              }
            />
          )
        })}
      {caps.data.assign && (
        <div className='space-y-3'>
          <div className='space-y-1.5'>
            <Label htmlFor='subscription-selection'>
              {t('Subscription instances')}
            </Label>
            <MultiSelect
              id='subscription-selection'
              placeholder={t('Subscription instances')}
              allowCreate={false}
              options={props.subscriptions
                .filter((s) => s.status === 'active')
                .map((s) => ({
                  value: String(s.id),
                  label: `#${s.id} · ${s.plan_title?.trim() || `${t('Plan')} #${s.plan_id}`}`,
                }))}
              selected={selected}
              onChange={(v) => setSelected(batchAllowed ? v : v.slice(-1))}
            />
          </div>
          <div className='space-y-1.5'>
            <Label htmlFor='target-upstream-pool'>
              {t('Target upstream pool')}
            </Label>
            <Combobox
              id='target-upstream-pool'
              aria-label={t('Target upstream pool')}
              value={poolID}
              options={(pools.data ?? [])
                .filter((p) => p.enabled && p.end_time > Date.now() / 1000)
                .map((p) => ({ value: String(p.id), label: p.name }))}
              onValueChange={(v) => {
                const nextPoolID = v ?? ''
                setPoolID(nextPoolID)
                const nextPool = pools.data?.find(
                  (p) => p.id === Number(nextPoolID)
                )
                const validKeys = new Set(
                  (nextPool?.cycles ?? [])
                    .filter((c) => !c.shared)
                    .map((c) => c.key)
                )
                setCycleOverrides((prev) =>
                  prev.filter((ov) => validKeys.has(ov.key))
                )
              }}
            />
          </div>
          <Label className='flex items-center gap-2'>
            <Switch checked={customExpiry} onCheckedChange={setCustomExpiry} />
            {t('Shorten member expiry')}
          </Label>
          {customExpiry && <DateTimePicker value={end} onChange={setEnd} />}
          {selectedPool && (
            <div className='space-y-2'>
              <p className='text-muted-foreground text-sm'>
                {t('Target cycle scopes')}:{' '}
                {selectedPool.cycles
                  .map(
                    (c) =>
                      `${c.name} · ${c.shared ? t('Shared') : t('Independent')}`
                  )
                  .join(' / ')}
              </p>
              <Label className='flex items-center gap-2'>
                <Switch
                  checked={customQuota}
                  onCheckedChange={(checked) => {
                    setCustomQuota(checked)
                    if (checked && selected.length > 0) {
                      const first = allocations.data?.allocations.find(
                        (a) => a.subscription_id === Number(selected[0])
                      )
                      if (first) {
                        setQuotaMultiplier(first.quota_multiplier || 1)
                        const validKeys = new Set(
                          selectedPool.cycles
                            .filter((c) => !c.shared)
                            .map((c) => c.key)
                        )
                        setCycleOverrides(
                          (first.cycle_overrides ?? []).filter((ov) =>
                            validKeys.has(ov.key)
                          )
                        )
                      }
                    }
                  }}
                />
                {t('Customize member quota share')}
              </Label>
              {customQuota && (
                <PoolQuotaAllocationFields
                  idPrefix='member-pool-quota'
                  multiplier={quotaMultiplier}
                  overrides={cycleOverrides}
                  poolCycles={selectedPool.cycles}
                  onChange={(next) => {
                    setQuotaMultiplier(next.quota_multiplier)
                    setCycleOverrides(next.cycle_overrides)
                  }}
                />
              )}
            </div>
          )}
          <p className='text-muted-foreground text-sm'>
            {t(
              'Legacy subscriptions retain settled usage in exactly matching cycles. Unmatched usage requires reconciliation before migration. Legacy inflight requests remain on their original ledger.'
            )}
          </p>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Moving members keeps old pool usage and target pool usage unchanged. No quota is transferred or cleared. Inflight requests remain with their original pool.'
            )}
          </p>
          <Button
            disabled={
              !selected.length ||
              !poolID ||
              preview.isPending ||
              assignment.isPending ||
              (customExpiry && !end)
            }
            onClick={() => preview.mutate()}
          >
            {t('Review and assign')}
          </Button>
          {preview.isError && <p role='alert'>{t(preview.error.message)}</p>}
          {results.length > 0 && (
            <ul className='space-y-1 text-sm'>
              {results.map((r) => (
                <li key={r.subscription_id}>
                  #{r.subscription_id} ·{' '}
                  {r.success ? t('Success') : t(r.message)}
                </li>
              ))}
            </ul>
          )}
          <ConfirmDialog
            open={!!previews?.length}
            onOpenChange={(v) => {
              if (!v) setPreviews(null)
            }}
            title={t('Confirm pool assignment')}
            desc={t(
              'Moving members keeps old pool usage and target pool usage unchanged. No quota is transferred or cleared. Inflight requests remain with their original pool.'
            )}
            handleConfirm={() => assignment.mutate()}
            isLoading={assignment.isPending}
          >
            <div className='max-h-72 space-y-3 overflow-y-auto'>
              {previews?.map((p) => (
                <div key={p.subscription_id} className='space-y-2'>
                  <p>
                    #{p.subscription_id} · {t('Expiry time')}:{' '}
                    <SyncTimestamp
                      value={p.end_time === 253402300799 ? 0 : p.end_time}
                      fallback='Never'
                    />
                    {p.quota_multiplier && p.quota_multiplier !== 1
                      ? ` · ${formatMultiplier(p.quota_multiplier)}`
                      : ''}
                  </p>
                  {!!p.legacy_carry?.amount && (
                    <p className='text-sm'>
                      {t(
                        'Settled usage to retain: {{amount}} quota units; excluded inflight reserves: {{reserved}}; target cycles: {{cycles}}.',
                        {
                          amount: formatNumber(p.legacy_carry.amount, locale),
                          reserved: formatNumber(
                            p.legacy_carry.excluded_reserved,
                            locale
                          ),
                          cycles: p.legacy_carry.keys.join(' / '),
                        }
                      )}
                    </p>
                  )}
                  <CycleProgress cycles={p.target_cycles} />
                </div>
              ))}
              {assignment.isError && (
                <p role='alert'>{t(assignment.error.message)}</p>
              )}
            </div>
          </ConfirmDialog>
        </div>
      )}
      <ConfirmDialog
        open={reset !== null}
        onOpenChange={(v) => {
          if (!v) setReset(null)
        }}
        title={t('Reset cycle')}
        desc={t(
          'Only this independent subscription cycle is reset. Inflight requests retain their original accounting window.'
        )}
        isLoading={resetMutation.isPending}
        handleConfirm={() => resetMutation.mutate()}
      >
        {resetMutation.isError && (
          <p role='alert'>{t(resetMutation.error.message)}</p>
        )}
      </ConfirmDialog>
    </section>
  )
}

function InstanceUsage(props: {
  usage: Usage
  allocation?: Allocation
  poolName?: string
  onReset?: (key: string) => void
}) {
  const { t } = useTranslation()
  const multiplier = props.allocation?.quota_multiplier
  const overrideCount = props.allocation?.cycle_overrides?.length ?? 0
  return (
    <div className='space-y-2'>
      <p className='text-sm font-medium'>
        #{props.usage.subscription_id} · {props.poolName ?? t('Independent')}
        {props.allocation?.pool_id && multiplier && multiplier !== 1
          ? ` · ${formatMultiplier(multiplier)}`
          : ''}
        {overrideCount > 0
          ? ` · ${t('Custom override')} (${overrideCount})`
          : ''}
        {props.usage.pending && ` · ${t('Pending assignment')}`}
      </p>
      <CycleProgress cycles={props.usage.cycles} />
      {props.onReset && (
        <div className='flex flex-wrap gap-2'>
          {props.usage.cycles
            .filter((c) => !c.shared)
            .map((c) => (
              <Button
                key={c.key}
                size='sm'
                variant='outline'
                onClick={() => props.onReset?.(c.key)}
              >
                {t('Reset')} · {c.name}
              </Button>
            ))}
        </div>
      )}
    </div>
  )
}
