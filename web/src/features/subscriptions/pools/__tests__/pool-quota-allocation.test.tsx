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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState, type ReactNode } from 'react'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { SubscriptionsMutateDrawer } from '../../components/subscriptions-mutate-drawer'
import type { Assignment, CycleOverride, Pool } from '../api'
import { InstanceAllocations } from '../instance-allocation'
import { PoolDetail } from '../pool-detail'
import { PoolQuotaAllocationFields } from '../pool-quota-allocation'
import { scaleCycleQuota } from '../quota-share'
import { allocationSchema, newCycle } from '../schema'

const clients: QueryClient[] = []
vi.mock('../../components/subscriptions-provider', () => ({
  useSubscriptions: () => ({ triggerRefresh: vi.fn() }),
}))

const caps = {
  read: true,
  manage: true,
  assign: true,
  personal_reset: true,
  batch_migrate: true,
  calibrate: false,
  shared_reset: false,
  audit_read: false,
  sync_read: false,
  sync_binding: false,
  sync_apply: false,
  sync_run: false,
}

const carpoolPool: Pool = {
  id: 21,
  name: 'Shared 5h + Weekly Pool',
  enabled: true,
  version: 5,
  channels: [1],
  external_source: '',
  external_id: '',
  updated_at: Math.floor(Date.now() / 1000),
  note: '',
  end_time: Math.floor(Date.now() / 1000) + 86400 * 30,
  cycles: [
    {
      ...newCycle('5h'),
      key: '5h',
      name: '5h',
      seconds: 18000,
      shared: true,
      unlimited: false,
      limit: 500000,
    },
    {
      ...newCycle('7d'),
      key: '7d',
      name: '7d',
      seconds: 604800,
      shared: false,
      unlimited: false,
      limit: 5000000,
    },
  ],
}

const envelope = (data: unknown) => ({ data: { success: true, data } })

function mount(children: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  return render(
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
}

afterEach(() => {
  clients.splice(0).forEach((client) => client.clear())
  vi.restoreAllMocks()
})

test('allocation schema validates quota_multiplier and cycle_overrides', () => {
  const base = {
    plan_id: 1,
    mode: 'fixed',
    pool_id: 21,
    version: 0,
    fixed_end_time: 0,
    cycles: [],
  }
  expect(
    allocationSchema.safeParse({
      ...base,
      quota_multiplier: 2,
      cycle_overrides: [{ key: '7d', limit: 12000000, unlimited: false }],
    }).success
  ).toBe(true)
  expect(
    allocationSchema.safeParse({ ...base, quota_multiplier: 0 }).success
  ).toBe(false)
  expect(
    allocationSchema.safeParse({ ...base, quota_multiplier: 101 }).success
  ).toBe(false)
  expect(
    allocationSchema.safeParse({
      ...base,
      cycle_overrides: [
        { key: '7d', limit: 1000, unlimited: false },
        { key: '7d', limit: 2000, unlimited: false },
      ],
    }).success
  ).toBe(false)
  expect(
    allocationSchema.safeParse({
      ...base,
      cycle_overrides: [{ key: '7d', limit: 0, unlimited: false }],
    }).success
  ).toBe(false)
  expect(
    allocationSchema.safeParse({
      ...base,
      cycle_overrides: [{ key: '7d', limit: 0, unlimited: true }],
    }).success
  ).toBe(true)
})

test('PoolQuotaAllocationFields keeps shared cycles unoverridable while scaling and overriding independent cycles', async () => {
  const prevConfig = useSystemConfigStore.getState().config
  useSystemConfigStore.setState({
    config: {
      ...prevConfig,
      currency: {
        ...prevConfig.currency,
        quotaDisplayType: 'USD',
        quotaPerUnit: 500000,
        usdExchangeRate: 1,
      },
    },
  })
  expect(scaleCycleQuota(5000000, 2)).toBe(10000000)

  function Harness() {
    const [multiplier, setMultiplier] = useState(1)
    const [overrides, setOverrides] = useState<CycleOverride[]>([])
    return (
      <PoolQuotaAllocationFields
        idPrefix='test-quota'
        multiplier={multiplier}
        overrides={overrides}
        poolCycles={carpoolPool.cycles}
        onChange={(next) => {
          setMultiplier(next.quota_multiplier)
          setOverrides(next.cycle_overrides)
        }}
      />
    )
  }

  mount(<Harness />)

  expect(screen.getByText(/Shared across the upstream pool/)).toBeVisible()
  // Only the independent 7d cycle has an "Override cycle quota" switch.
  expect(
    screen.getAllByRole('switch', { name: 'Override cycle quota' })
  ).toHaveLength(1)

  // Click 2x preset: 5,000,000 ($10) scales to 10,000,000 ($20).
  await userEvent.click(screen.getByRole('button', { name: '2×' }))
  expect(screen.getByText('$20')).toBeVisible()
  expect(screen.getByText('(2× × $10)')).toBeVisible()

  // Enable per-cycle override and toggle Unlimited on, then disable override.
  const overrideSwitch = screen.getByRole('switch', {
    name: 'Override cycle quota',
  })
  await userEvent.click(overrideSwitch)
  expect(screen.getByText('Custom override')).toBeVisible()
  await userEvent.click(screen.getByRole('switch', { name: 'Unlimited' }))
  expect(screen.getAllByText('Unlimited').length).toBeGreaterThanOrEqual(2)
  await userEvent.click(overrideSwitch)
  expect(screen.queryByText('Custom override')).not.toBeInTheDocument()
})

test('creating a pooled plan with 2x multiplier submits quota_multiplier and cycle_overrides atomically', async () => {
  vi.spyOn(api, 'get').mockResolvedValue(envelope([]))
  vi.spyOn(api, 'request').mockImplementation(async (config) => {
    if (config.url?.endsWith('/pool-capabilities')) return envelope(caps)
    if (config.url?.endsWith('/pools')) return envelope([carpoolPool])
    return envelope([])
  })
  const create = vi.spyOn(api, 'post').mockResolvedValue(envelope({ id: 55 }))
  const close = vi.fn()

  mount(<SubscriptionsMutateDrawer open onOpenChange={close} />)

  await userEvent.type(
    await screen.findByRole('textbox', { name: 'Plan Title' }),
    '三人拼*2'
  )
  await userEvent.click(
    await screen.findByRole('combobox', { name: 'Allocation mode' })
  )
  await userEvent.click(
    await screen.findByRole('option', { name: 'Default upstream pool' })
  )
  await userEvent.click(
    await screen.findByRole('combobox', { name: 'Default upstream pool' })
  )
  await userEvent.click(
    await screen.findByRole('option', { name: 'Shared 5h + Weekly Pool' })
  )
  await userEvent.click(await screen.findByRole('button', { name: '2×' }))
  await userEvent.click(screen.getByRole('button', { name: 'Save changes' }))

  await waitFor(() => expect(create).toHaveBeenCalledTimes(1))
  expect(create.mock.calls[0][1]).toMatchObject({
    plan: { title: '三人拼*2' },
    allocation: {
      mode: 'fixed',
      pool_id: 21,
      quota_multiplier: 2,
      cycle_overrides: [],
    },
  })
  await waitFor(() => expect(close).toHaveBeenCalledWith(false))
})

test('InstanceAllocations sends custom member quota_multiplier on preview and confirm', async () => {
  const request = vi
    .spyOn(api, 'request')
    .mockImplementation(async (config) => {
      if (config.url?.endsWith('/pool-capabilities')) return envelope(caps)
      if (config.url?.endsWith('/allocations')) {
        return envelope({ allocations: [], subscriptions: [] })
      }
      if (config.url?.endsWith('/pool-assignment')) {
        const input = config.data as Assignment
        if (input.confirm) return envelope(null)
        return envelope({
          subscription_id: 42,
          target_pool_id: 21,
          pool_version: 5,
          end_time: 253402300799,
          quota_multiplier: input.quota_multiplier ?? 1,
          cycle_overrides: input.cycle_overrides ?? [],
          current: { version: 0 },
          target_cycles: [],
          usage_transfer: false,
        })
      }
      return envelope([carpoolPool])
    })

  mount(
    <InstanceAllocations
      userId={7}
      subscriptions={[
        { id: 42, plan_id: 1, status: 'active', plan_title: '三人拼*2' },
      ]}
    />
  )

  await userEvent.click(
    await screen.findByRole('combobox', { name: 'Subscription instances' })
  )
  await userEvent.click(
    await screen.findByRole('option', { name: '#42 · 三人拼*2' })
  )
  await userEvent.keyboard('{Escape}')
  await userEvent.click(
    screen.getByRole('combobox', { name: 'Target upstream pool' })
  )
  await userEvent.click(
    await screen.findByRole('option', { name: 'Shared 5h + Weekly Pool' })
  )
  await userEvent.click(
    screen.getByRole('switch', { name: 'Customize member quota share' })
  )
  await userEvent.click(screen.getByRole('button', { name: '2×' }))
  await userEvent.click(
    screen.getByRole('button', { name: 'Review and assign' })
  )

  const dialog = await screen.findByRole('alertdialog', {
    name: 'Confirm pool assignment',
  })
  expect(within(dialog).getByText(/2×/)).toBeVisible()
  await userEvent.click(
    within(dialog).getByRole('button', { name: 'Continue' })
  )

  await waitFor(() =>
    expect(
      request.mock.calls.some(
        ([c]) => (c.data as Assignment | undefined)?.confirm === true
      )
    ).toBe(true)
  )
  const confirmed = request.mock.calls.find(
    ([c]) => (c.data as Assignment | undefined)?.confirm === true
  )?.[0].data as Assignment
  expect(confirmed.quota_multiplier).toBe(2)
  expect(confirmed.cycle_overrides).toEqual([])
})

test('PoolDetail members table renders quota share multipliers and custom overrides', async () => {
  vi.spyOn(api, 'request').mockResolvedValue(
    envelope({
      cycles: [],
      members: [
        {
          subscription_id: 101,
          user_id: 1,
          username: 'alice',
          end_time: 253402300799,
          status: 'active',
          quota_multiplier: 1,
          cycle_overrides: [],
        },
        {
          subscription_id: 102,
          user_id: 2,
          username: 'bob',
          end_time: 253402300799,
          status: 'active',
          quota_multiplier: 2,
          cycle_overrides: [{ key: '7d', limit: 15000000, unlimited: false }],
        },
      ],
      events: [],
    })
  )

  mount(<PoolDetail pool={carpoolPool} caps={caps} tab='members' />)

  expect(await screen.findByText('Quota share')).toBeVisible()
  expect(await screen.findByText('1×')).toBeVisible()
  expect(screen.getByText('2× · Custom override (1)')).toBeVisible()
})
