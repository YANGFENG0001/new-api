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
import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { SubscriptionsMutateDrawer } from '../../components/subscriptions-mutate-drawer'
import { subscriptionPlanSchema } from '../../types'
import type { Assignment } from '../api'
import { InstanceAllocations } from '../instance-allocation'
import { PoolEditor } from '../pool-editor'
import { PoolPanel } from '../pool-panel'
import { PoolSyncImport } from '../pool-sync-import'
import { emptyPool, newCycle } from '../schema'

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
}
const pool = {
  id: 7,
  name: 'Pool A',
  enabled: true,
  version: 3,
  channels: [],
  note: '',
  end_time: Math.floor(Date.now() / 1000) + 86400,
  cycles: [newCycle('Custom')],
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
})

test('refreshing imported upstream data requires confirmation before calibrating linked pools', async () => {
  const request = vi
    .spyOn(api, 'request')
    .mockImplementation(async (config) => {
      if (config.url?.endsWith('/sync-sources')) {
        return envelope({
          sources: [
            {
              id: 3,
              name: 'Source',
              enabled: true,
              interval_seconds: 60,
              timeout_seconds: 10,
              stale_seconds: 300,
            },
          ],
          template: {},
          privileged: true,
        })
      }
      if (config.url?.endsWith('/run')) {
        return envelope({ accounts: [], changes: [] })
      }
      return envelope({ accounts: [], events: [] })
    })
  mount(<PoolSyncImport canRun onImport={vi.fn()} onClear={vi.fn()} />)
  await userEvent.click(
    await screen.findByRole('combobox', {
      name: 'Upstream synchronization source',
    })
  )
  await userEvent.click(await screen.findByRole('option', { name: 'Source' }))
  await userEvent.click(
    screen.getByRole('button', { name: 'Refresh saved upstream data' })
  )
  let dialog = await screen.findByRole('alertdialog')
  expect(request.mock.calls.some(([c]) => c.url?.endsWith('/run'))).toBe(false)
  await userEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
  expect(request.mock.calls.some(([c]) => c.url?.endsWith('/run'))).toBe(false)
  await userEvent.click(
    screen.getByRole('button', { name: 'Refresh saved upstream data' })
  )
  dialog = await screen.findByRole('alertdialog')
  await userEvent.click(
    within(dialog).getByRole('button', { name: 'Continue' })
  )
  await waitFor(() =>
    expect(
      request.mock.calls.filter(([c]) => c.url?.endsWith('/run'))
    ).toHaveLength(1)
  )
  expect(
    request.mock.calls.find(([c]) => c.url?.endsWith('/run'))?.[0].data
  ).toEqual({ preview: false })
})

test('unified pool settings preserve drafts through cache refresh and submit binding with one pool save', async () => {
  const cycle = { ...newCycle('Short'), key: 'short', seconds: 3600 }
  const fullPool = { ...emptyPool(), ...pool, cycles: [cycle] }
  const binding = {
    pool_id: 7,
    source_id: 3,
    account_id: 'stable',
    version: 1,
    enabled: true,
    sync_expiry: true,
    stale_policy: 'stop',
    grace_seconds: 0,
    cycles: [
      {
        local_key: 'short',
        upstream_key: 'short',
        align_reset: true,
        enforce: true,
        threshold: 1,
        max_shift_seconds: 300,
      },
    ],
  }
  const request = vi
    .spyOn(api, 'request')
    .mockImplementation(async (config) => {
      if (config.method === 'put') {
        return envelope({ ...fullPool, ...(config.data as object), version: 4 })
      }
      if (config.url?.endsWith('/sync-sources')) {
        return envelope({
          sources: [
            {
              id: 3,
              name: 'Source',
              enabled: true,
              mapping: {
                cycles: [{ key: 'short', name: 'Short', seconds: 3600 }],
              },
            },
          ],
          template: {},
          privileged: true,
        })
      }
      if (config.url?.endsWith('/sync')) {
        return envelope({ binding, observations: [] })
      }
      if (config.url?.endsWith('/state')) {
        return envelope({
          accounts: [{ id: 'stable', name: 'Account', cycles: [] }],
          events: [],
        })
      }
      return envelope([])
    })
  const saved = vi.fn()
  mount(
    <PoolEditor
      pool={fullPool}
      embedded
      canManage
      caps={{
        ...caps,
        calibrate: false,
        shared_reset: false,
        audit_read: true,
        sync_read: true,
        sync_binding: true,
        sync_apply: true,
      }}
      onClose={vi.fn()}
      onSaved={saved}
    />
  )
  expect(
    await screen.findByRole('switch', { name: 'Align reset time' })
  ).toBeChecked()
  expect(
    screen.getByRole('spinbutton', { name: 'Cycle duration' })
  ).toBeDisabled()
  await userEvent.type(
    screen.getByRole('textbox', { name: 'Administrator note' }),
    'Keep my draft'
  )
  await act(async () => {
    const client = clients.at(-1)
    if (!client) throw new Error('Missing query client fixture')
    await client.invalidateQueries({
      queryKey: ['subscription-sync'],
    })
  })
  expect(
    screen.getByRole('textbox', { name: 'Administrator note' })
  ).toHaveValue('Keep my draft')
  await userEvent.click(
    screen.getByRole('switch', { name: 'Align reset time' })
  )
  expect(
    screen.getByRole('spinbutton', { name: 'Cycle duration' })
  ).toBeEnabled()
  await userEvent.click(screen.getByRole('button', { name: 'Add cycle' }))
  const removes = screen.getAllByRole('button', { name: 'Remove' })
  expect(removes[0]).toBeDisabled()
  await userEvent.click(removes[1])
  expect(screen.getAllByRole('button', { name: 'Remove' })).toHaveLength(1)
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(saved).toHaveBeenCalledTimes(1))
  const writes = request.mock.calls.filter(([c]) => c.method === 'put')
  expect(writes).toHaveLength(1)
  expect(writes[0][0].url).toMatch(/\/pools\/7$/)
  expect(writes[0][0].data).toMatchObject({
    note: 'Keep my draft',
    sync_binding: { version: 1, cycles: [{ align_reset: false }] },
  })
})

test('a new multi-cycle plan submits its terms and allocation once and never saves allocation separately', async () => {
  vi.spyOn(api, 'get').mockResolvedValue(envelope([]))
  vi.spyOn(api, 'request').mockResolvedValue(envelope(caps))
  const create = vi.spyOn(api, 'post').mockResolvedValue(envelope({ id: 11 }))
  const update = vi.spyOn(api, 'put').mockResolvedValue(envelope(null))
  const close = vi.fn()
  mount(<SubscriptionsMutateDrawer open onOpenChange={close} />)
  expect(document.querySelectorAll('form')).toHaveLength(1)
  await userEvent.type(
    await screen.findByRole('textbox', { name: 'Plan Title' }),
    'Atomic terms'
  )
  await userEvent.click(
    await screen.findByRole('combobox', { name: 'Allocation mode' })
  )
  await userEvent.click(
    await screen.findByRole('option', {
      name: 'Independent multi-cycle subscription',
    })
  )
  expect(screen.queryByText('Total Quota')).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Save changes' }))
  expect(create).not.toHaveBeenCalled()
  await userEvent.click(screen.getByRole('button', { name: 'Add cycle' }))
  await userEvent.click(screen.getByRole('button', { name: 'Save changes' }))
  await waitFor(() => expect(create).toHaveBeenCalledTimes(1))
  expect(create.mock.calls[0][1]).toMatchObject({
    plan: { title: 'Atomic terms' },
    allocation: {
      mode: 'independent',
      version: 0,
      cycles: [{ shared: false, seconds: 18000 }],
    },
  })
  expect(update).not.toHaveBeenCalled()
  await waitFor(() => expect(close).toHaveBeenCalledWith(false))
})

test('a saved pool synchronization shortcut opens unified settings with exactly four main tabs', async () => {
  vi.spyOn(api, 'request').mockImplementation(async (config) => {
    if (config.url?.endsWith('/pools')) return envelope([pool])
    if (config.url?.endsWith('/pool-catalog')) return envelope([])
    if (config.url?.endsWith('/sync-sources')) {
      return envelope({ sources: [], template: {}, privileged: true })
    }
    if (config.url?.endsWith('/sync')) {
      return envelope({
        binding: { pool_id: 7, version: 0, cycles: [] },
        observations: [],
      })
    }
    return envelope({
      cycles: [],
      members: [
        {
          subscription_id: 12,
          user_id: 1,
          username: 'Member',
          end_time: 253402300799,
          status: 'active',
        },
      ],
      events: [],
    })
  })
  mount(
    <PoolPanel
      caps={
        { ...caps, sync_read: true } as Parameters<typeof PoolPanel>[0]['caps']
      }
    />
  )
  await userEvent.click(
    await screen.findByRole('button', { name: 'Automatic synchronization' })
  )
  expect(screen.getByRole('tab', { name: 'Settings' })).toHaveAttribute(
    'aria-selected',
    'true'
  )
  expect(screen.getByRole('tab', { name: 'Settings' })).toBeVisible()
  expect(screen.getAllByRole('tab')).toHaveLength(4)
  expect(
    screen.getByLabelText('Name', { selector: '#pool-name' })
  ).toBeVisible()
  await userEvent.click(screen.getByRole('tab', { name: 'Members' }))
  expect(await screen.findByText('Never')).toBeVisible()
  expect(screen.queryByText(/9999/)).not.toBeInTheDocument()
})

test('creating a pool explains synchronization and opens its saved overview without a second save', async () => {
  vi.spyOn(api, 'request').mockImplementation(async (config) => {
    if (config.url?.endsWith('/pools') && config.method === 'post') {
      return envelope({
        name: 'New pool',
        enabled: true,
        end_time: pool.end_time,
        id: 7,
        version: 1,
      })
    }
    if (config.url?.endsWith('/pools')) return envelope([])
    if (config.url?.endsWith('/pool-catalog')) return envelope([])
    if (config.url?.endsWith('/sync-sources')) {
      return envelope({ sources: [], template: {}, privileged: true })
    }
    if (config.url?.endsWith('/sync')) {
      return envelope({
        binding: { pool_id: 7, version: 0, cycles: [] },
        observations: [],
      })
    }
    return envelope({ cycles: [], members: [], events: [] })
  })
  mount(
    <PoolPanel
      caps={
        { ...caps, sync_read: true } as Parameters<typeof PoolPanel>[0]['caps']
      }
    />
  )
  await userEvent.click(
    await screen.findByRole('button', { name: 'Create upstream pool' })
  )
  expect(screen.getByRole('alert')).toHaveTextContent('Create from upstream')
  await userEvent.type(
    screen.getByRole('textbox', { name: 'Name' }),
    'New pool'
  )
  await userEvent.click(screen.getByRole('button', { name: 'Add cycle' }))
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  expect(await screen.findByRole('tab', { name: 'Overview' })).toHaveAttribute(
    'aria-selected',
    'true'
  )
})

test('a user without synchronization permission does not receive a synchronization shortcut', async () => {
  vi.spyOn(api, 'request').mockResolvedValue(envelope([pool]))
  mount(<PoolPanel caps={caps as Parameters<typeof PoolPanel>[0]['caps']} />)
  await screen.findByRole('button', { name: 'Pool A' })
  expect(
    screen.queryByRole('button', { name: 'Automatic synchronization' })
  ).not.toBeInTheDocument()
})

test('when allocation loading fails, editing a managed plan keeps legacy fields hidden and save disabled', async () => {
  vi.spyOn(api, 'get').mockResolvedValue(envelope([]))
  vi.spyOn(api, 'request').mockImplementation(async (config) => {
    if (config.url?.endsWith('/pool-capabilities')) return envelope(caps)
    if (config.url?.endsWith('/allocation')) throw new Error('offline')
    return envelope([])
  })
  const update = vi.spyOn(api, 'put').mockResolvedValue(envelope(null))
  mount(
    <SubscriptionsMutateDrawer
      open
      onOpenChange={vi.fn()}
      currentRow={{
        plan: subscriptionPlanSchema.parse({
          id: 1,
          title: 'Managed',
          price_amount: 1,
          duration_unit: 'month',
          duration_value: 1,
          quota_reset_period: 'never',
          enabled: true,
          sort_order: 0,
          max_purchase_per_user: 0,
          total_amount: 0,
        }),
        allocation: {
          mode: 'fixed',
          available: true,
          fixed_end_time: pool.end_time,
          cycles: [],
        },
      }}
    />
  )
  await screen.findByRole('button', { name: 'Retry' })
  expect(screen.getByRole('button', { name: 'Save changes' })).toBeDisabled()
  expect(screen.queryByText('Total Quota')).not.toBeInTheDocument()
  expect(screen.queryByText('Quota Reset')).not.toBeInTheDocument()
  expect(update).not.toHaveBeenCalled()
})

test('single assignment requires a server preview and confirms the versions actually reviewed', async () => {
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
          subscription_id: 12,
          target_pool_id: 7,
          pool_version: 4,
          end_time: 253402300799,
          current: { version: 2 },
          target_cycles: [],
          usage_transfer: false,
        })
      }
      return envelope([pool])
    })
  mount(
    <InstanceAllocations
      userId={1}
      subscriptions={[{ id: 12, plan_id: 1, status: 'active' }]}
    />
  )
  const selection = await screen.findByRole('combobox', {
    name: 'Subscription instances',
  })
  await userEvent.click(selection)
  await userEvent.click(
    await screen.findByRole('option', { name: '#12 · Plan #1' })
  )
  await userEvent.keyboard('{Escape}')
  await userEvent.click(
    screen.getByRole('combobox', { name: 'Target upstream pool' })
  )
  await userEvent.click(await screen.findByRole('option', { name: 'Pool A' }))
  await userEvent.click(
    screen.getByRole('button', { name: 'Review and assign' })
  )
  const dialog = await screen.findByRole('alertdialog', {
    name: 'Confirm pool assignment',
  })
  expect(within(dialog).getByText('Never')).toBeVisible()
  expect(
    request.mock.calls.filter(
      ([c]) => (c.data as Assignment | undefined)?.confirm === true
    )
  ).toHaveLength(0)
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
  expect(confirmed.version).toBe(2)
  expect(confirmed.pool_version).toBe(4)
  expect(confirmed.end_time).toBe(0)
})

test('allocation load failures do not expose enabled batch controls', async () => {
  vi.spyOn(api, 'request').mockImplementation(async (config) => {
    if (config.url?.endsWith('/pool-capabilities')) return envelope(caps)
    throw new Error('offline')
  })
  mount(<InstanceAllocations userId={1} subscriptions={[]} />)
  expect(
    await screen.findByRole('button', { name: 'Retry' })
  ).toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: 'Review and assign' })
  ).not.toBeInTheDocument()
})

test('creating from upstream fills reset windows and keeps imported policy editable before one atomic save', async () => {
  const now = Math.floor(Date.now() / 1000)
  const source = {
    id: 3,
    name: 'Upstream panel',
    version: 2,
    enabled: true,
    interval_seconds: 60,
    timeout_seconds: 10,
    stale_seconds: 60,
    mapping: { cycles: [] },
  }
  const request = vi
    .spyOn(api, 'request')
    .mockImplementation(async (config) => {
      if (config.url?.endsWith('/sync-sources')) {
        return envelope({ sources: [source], template: {}, privileged: true })
      }
      if (config.url?.endsWith('/state')) {
        return envelope({
          accounts: [
            {
              id: 'account-1',
              name: 'vm-01',
              expiry: 0,
              sampled_at: now,
              cycles: [
                {
                  key: 'short',
                  name: '5h',
                  seconds: 18000,
                  window_start: now - 100,
                  window_end: now + 17900,
                  utilization: 0.4,
                  sampled_at: now,
                  error: '',
                },
                {
                  key: 'missing',
                  name: 'Sonnet',
                  seconds: 604800,
                  window_end: 0,
                  utilization: null,
                  sampled_at: 0,
                  error: 'Not reported',
                },
              ],
            },
          ],
          events: [],
        })
      }
      if (config.url?.endsWith('/pools') && config.method === 'post') {
        return envelope({ ...(config.data as object), id: 9, version: 1 })
      }
      if (config.url?.endsWith('/sync')) {
        return envelope({
          binding: { pool_id: 9, cycles: [], version: 0 },
          observations: [],
        })
      }
      if (
        config.url?.endsWith('/pools') ||
        config.url?.endsWith('/pool-catalog')
      ) {
        return envelope([])
      }
      return envelope({ cycles: [], members: [], events: [] })
    })
  mount(
    <PoolPanel
      caps={
        {
          ...caps,
          sync_read: true,
          sync_binding: true,
          sync_apply: true,
          sync_run: true,
        } as Parameters<typeof PoolPanel>[0]['caps']
      }
    />
  )
  await userEvent.click(
    await screen.findByRole('button', { name: 'Create upstream pool' })
  )
  await userEvent.click(
    screen.getByRole('switch', { name: 'Create from upstream' })
  )
  await userEvent.click(
    await screen.findByRole('combobox', {
      name: 'Upstream synchronization source',
    })
  )
  await userEvent.click(
    await screen.findByRole('option', { name: 'Upstream panel' })
  )
  await userEvent.click(
    await screen.findByRole('combobox', { name: 'Upstream account' })
  )
  await userEvent.click(
    await screen.findByRole('option', { name: 'vm-01 · account-1' })
  )
  const cycle = screen.getByRole('region', { name: '5h' })
  expect(within(cycle).getByRole('switch', { name: 'Unlimited' })).toBeChecked()
  expect(
    screen.queryByRole('region', { name: 'Sonnet' })
  ).not.toBeInTheDocument()
  await userEvent.clear(within(cycle).getByRole('textbox', { name: 'Name' }))
  await userEvent.type(
    within(cycle).getByRole('textbox', { name: 'Name' }),
    'Custom cycle'
  )
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  await screen.findByRole('tab', { name: 'Overview' })
  const payload = request.mock.calls.find(
    ([c]) => c.method === 'post' && c.url?.endsWith('/pools')
  )?.[0].data as Record<string, unknown>
  expect(payload).toMatchObject({
    end_time: 253402300799,
    sync: {
      source_id: 3,
      source_version: 2,
      account_id: 'account-1',
      sync_expiry: true,
    },
    cycles: [
      {
        key: 'short',
        name: 'Custom cycle',
        next_reset: now + 17900,
        seconds: 18000,
        unlimited: true,
      },
    ],
  })
  expect(request.mock.calls.some(([c]) => c.url?.endsWith('/run'))).toBe(false)
})
