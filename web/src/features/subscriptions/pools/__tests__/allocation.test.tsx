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
import type { ReactNode } from 'react'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import type { Assignment } from '../api'
import { InstanceAllocations } from '../instance-allocation'
import { PlanAllocationSettings } from '../plan-allocation'
import { newCycle } from '../schema'

const clients: QueryClient[] = []
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

test('saving allocation from the plan dialog does not submit its parent plan form', async () => {
  const parentSubmit = vi.fn((event) => event.preventDefault())
  const request = vi
    .spyOn(api, 'request')
    .mockImplementation(async (config) => {
      if (config.method === 'put') return envelope(null)
      if (config.url?.endsWith('/pool-capabilities')) return envelope(caps)
      if (config.url?.endsWith('/allocation')) {
        return envelope({
          plan_id: 1,
          mode: 'independent',
          pool_id: 0,
          fixed_end_time: 0,
          version: 1,
          cycles: [newCycle('Personal')],
        })
      }
      return envelope([pool])
    })
  mount(
    <form onSubmit={parentSubmit}>
      <PlanAllocationSettings planId={1} />
    </form>
  )
  await userEvent.click(
    await screen.findByRole('button', { name: 'Configure allocation' })
  )
  const dialog = await screen.findByRole('dialog', {
    name: 'Allocation and aligned cycles',
  })
  await userEvent.click(
    await within(dialog).findByRole('button', { name: 'Save' })
  )
  await waitFor(() =>
    expect(request.mock.calls.some(([config]) => config.method === 'put')).toBe(
      true
    )
  )
  expect(parentSubmit).not.toHaveBeenCalled()
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
          end_time: pool.end_time,
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
