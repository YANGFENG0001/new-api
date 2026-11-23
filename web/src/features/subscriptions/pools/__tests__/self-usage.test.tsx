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
import { act, render, screen, within } from '@testing-library/react'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import type { Usage } from '../api'
import { CycleProgress } from '../cycle-progress'
import { SelfAlignedUsage } from '../self-usage'

const clients: QueryClient[] = []

beforeEach(() => {
  useSystemConfigStore.getState().setConfig({
    currency: { ...DEFAULT_CURRENCY_CONFIG, quotaDisplayType: 'TOKENS' },
  })
})
test('a local current-window estimate remains separate from an expired upstream snapshot', () => {
  const end = Math.floor(Date.now() / 1000) + 3500
  render(
    <CycleProgress
      preferObservations
      cycles={[
        {
          key: 'c',
          name: 'Current local',
          seconds: 3600,
          shared: true,
          unlimited: true,
          limit: 0,
          used: 10,
          adjustment: 0,
          reserved: 0,
          start_time: end - 3600,
          next_reset: end,
          pending: false,
        },
      ]}
      observations={[
        {
          key: 'c',
          name: 'Old upstream',
          utilization: 0.9,
          sampled_at: end - 3700,
          fetched_at: end - 3500,
          window_end: end - 3600,
          stale: true,
          pending: '',
          local_window_end: end,
          local_estimated_utilization: 0.1,
        },
      ]}
    />
  )
  const current = screen.getByRole('region', { name: 'Current local' })
  expect(within(current).getByText('10%')).toBeVisible()
  expect(
    within(current).getByText('Local estimate for current cycle')
  ).toBeVisible()
  expect(screen.getByText('Historical upstream snapshot')).toBeVisible()
  expect(screen.getByText('90%')).toBeVisible()
})

test.each(['expired', 'stale', 'pending'])(
  'a %s upstream snapshot cannot replace the current local cycle',
  (reason) => {
    const end = Math.floor(Date.now() / 1000) + 3600
    render(
      <CycleProgress
        preferObservations
        cycles={[
          {
            key: 'c',
            name: 'Current cycle',
            seconds: 3600,
            shared: true,
            unlimited: false,
            limit: 100,
            used: 7,
            adjustment: 0,
            reserved: 0,
            start_time: end - 3600,
            next_reset: end,
            pending: false,
          },
        ]}
        observations={[
          {
            key: 'c',
            name: 'Upstream cycle',
            utilization: 0.9,
            sampled_at: end - 3500,
            fetched_at: end - 3490,
            window_end: reason === 'expired' ? end - 3600 : end,
            stale: reason === 'stale',
            pending:
              reason === 'pending'
                ? 'Upstream reset shift requires confirmation'
                : '',
          },
        ]}
      />
    )
    const local = screen.getByRole('region', { name: 'Current cycle' })
    expect(within(local).getByText(/7 \/ 100/)).toBeVisible()
    expect(within(local).getByText('Local usage statistics')).toBeVisible()
  }
)
afterEach(() => {
  clients.splice(0).forEach((client) => client.clear())
  useSystemConfigStore.getState().setConfig({
    currency: { ...DEFAULT_CURRENCY_CONFIG },
  })
})

function mount(subscription?: Usage) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  return render(
    <QueryClientProvider client={client}>
      <SelfAlignedUsage subscription={subscription} />
    </QueryClientProvider>
  )
}

test('legacy-only accounts do not display an upstream carpool section', async () => {
  vi.spyOn(api, 'request').mockImplementation(async (config) => ({
    data: {
      success: true,
      data: config.url?.endsWith('/usage')
        ? { server_time: 100, subscriptions: [] }
        : [],
    },
  }))
  const view = mount()
  await act(async () => {
    await clients[0].refetchQueries()
  })
  expect(view.container).not.toHaveTextContent('Upstream carpool subscriptions')
})

test('a subscription only exposes its own pool and does not fetch duplicate personal usage', async () => {
  const request = vi.spyOn(api, 'request').mockResolvedValue({
    data: {
      success: true,
      data: [
        {
          id: 1,
          name: 'My pool',
          subscription_ids: [9],
          end_time: 1890000000,
          cycles: [],
          observations: [],
        },
        {
          id: 2,
          name: 'Different subscription pool',
          subscription_ids: [10],
          end_time: 1890000000,
          cycles: [],
          observations: [],
        },
      ],
    },
  })
  mount({
    subscription_id: 9,
    plan_id: 2,
    status: 'active',
    managed: true,
    pending: false,
    end_time: 1890000000,
    cycles: [],
  })
  await screen.findByText('My pool')
  expect(
    screen.queryByRole('button', { name: 'Upstream carpool subscriptions' })
  ).not.toBeInTheDocument()
  expect(screen.getByText('My pool')).toBeVisible()
  expect(
    screen.queryByText('Different subscription pool')
  ).not.toBeInTheDocument()
  expect(request.mock.calls.some(([c]) => c.url?.endsWith('/usage'))).toBe(
    false
  )
})

test('a pooled subscription shows the shared cycle once and retains its independent member quota', async () => {
  const cycle = {
    key: 'short',
    name: '7h',
    seconds: 25200,
    shared: true,
    unlimited: true,
    limit: 0,
    used: 12,
    reserved: 0,
    adjustment: 0,
    start_time: 1,
    next_reset: 1890000000,
    pending: false,
  }
  vi.spyOn(api, 'request').mockResolvedValue({
    data: {
      success: true,
      data: [
        {
          id: 1,
          name: 'Shared upstream',
          subscription_ids: [9],
          end_time: 1890000000,
          cycles: [cycle],
          observations: [],
          sync_observations: [
            {
              key: 'short',
              name: '7h',
              utilization: 0.3,
              sampled_at: 1890000000 - 100,
              fetched_at: 1890000000 - 100,
              window_end: 1890000000,
              stale: false,
              pending: '',
            },
          ],
        },
      ],
    },
  })
  mount({
    subscription_id: 9,
    plan_id: 2,
    status: 'active',
    managed: true,
    pending: false,
    end_time: 1890000000,
    cycles: [
      cycle,
      {
        ...cycle,
        key: 'week',
        name: 'Personal week',
        shared: false,
        unlimited: false,
        limit: 100,
        used: 7,
      },
    ],
  })
  await screen.findByText('Shared upstream')
  expect(screen.getAllByRole('progressbar', { name: '7h' })).toHaveLength(1)
  expect(screen.getAllByText('7h')).toHaveLength(1)
  expect(screen.getByText(/7 \/ 100/)).toBeVisible()
  expect(screen.queryByText(/12 \/ Unlimited/)).not.toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: 'Upstream carpool subscriptions' })
  ).not.toBeInTheDocument()
})

test('multiple subscription instances in one pool share one upstream summary while keeping each personal limit', async () => {
  const cycle = {
    key: 'week',
    name: 'Member week',
    seconds: 604800,
    shared: false,
    unlimited: false,
    limit: 100,
    used: 7,
    reserved: 0,
    adjustment: 0,
    start_time: 1,
    next_reset: 1890000000,
    pending: false,
  }
  vi.spyOn(api, 'request').mockResolvedValue({
    data: {
      success: true,
      data: [
        {
          id: 1,
          name: 'Only one pool summary',
          subscription_ids: [9, 10],
          end_time: 1890000000,
          cycles: [],
          observations: [],
        },
      ],
    },
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  const subscription = {
    subscription_id: 9,
    plan_id: 2,
    status: 'active',
    managed: true,
    pending: false,
    end_time: 1890000000,
    cycles: [cycle],
  }
  render(
    <QueryClientProvider client={client}>
      <SelfAlignedUsage
        subscriptions={[subscription, { ...subscription, subscription_id: 10 }]}
        planTitles={new Map([[2, 'Claude Pro']])}
      />
    </QueryClientProvider>
  )
  await screen.findByText('Only one pool summary')
  expect(screen.getAllByText('Only one pool summary')).toHaveLength(1)
  expect(screen.getAllByText(/7 \/ 100/)).toHaveLength(2)
  expect(screen.getByText('Claude Pro · Subscription #9')).toBeVisible()
  expect(screen.getByText('Claude Pro · Subscription #10')).toBeVisible()
})

test('pending assignment shows unavailable without a fake unlimited quota or upstream section', async () => {
  vi.spyOn(api, 'request').mockResolvedValue({
    data: { success: true, data: [] },
  })
  mount({
    subscription_id: 9,
    plan_id: 2,
    status: 'active',
    managed: true,
    pending: true,
    end_time: 1890000000,
    cycles: [],
  })
  expect(await screen.findByText('Pending assignment')).toBeVisible()
  expect(
    screen.getByText(
      'This subscription is unavailable until its pool assignment or availability is resolved.'
    )
  ).toBeVisible()
  expect(screen.queryByText('Unlimited')).not.toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: 'Upstream carpool subscriptions' })
  ).not.toBeInTheDocument()
})

test('mixed shared and personal cycles show their actual scopes and distinct usage', async () => {
  vi.spyOn(api, 'request').mockResolvedValue({
    data: { success: true, data: [] },
  })
  const base = {
    seconds: 137,
    unlimited: false,
    limit: 100,
    reserved: 0,
    adjustment: 0,
    start_time: 1,
    next_reset: 1890000000,
    pending: false,
  }
  mount({
    subscription_id: 9,
    plan_id: 2,
    status: 'active',
    managed: true,
    pending: false,
    end_time: 1890000000,
    cycles: [
      { ...base, key: 'shared', name: 'Shared short', shared: true, used: 30 },
      {
        ...base,
        key: 'personal',
        name: 'Personal long',
        shared: false,
        used: 7,
      },
    ],
  })
  const shared = screen.getByRole('region', { name: 'Shared short' })
  const personal = screen.getByRole('region', { name: 'Personal long' })
  expect(within(shared).getByText('Shared')).toBeVisible()
  expect(within(shared).getByText(/30 \/ 100/)).toBeVisible()
  expect(within(personal).getByText('Independent')).toBeVisible()
  expect(within(personal).getByText(/7 \/ 100/)).toBeVisible()
})

test('cancelled multi-cycle subscriptions remain visible and do not masquerade as traditional unlimited subscriptions', async () => {
  vi.spyOn(api, 'request').mockImplementation(async (config) => ({
    data: {
      success: true,
      data: config.url?.endsWith('/usage')
        ? {
            server_time: Date.now() / 1000,
            subscriptions: [
              {
                subscription_id: 9,
                plan_id: 2,
                status: 'cancelled',
                managed: true,
                pending: false,
                end_time: 1890000000,
                cycles: [],
              },
            ],
          }
        : [],
    },
  }))
  mount()
  expect(await screen.findByText(/#9/)).toBeVisible()
  expect(screen.getByText('Cancelled')).toBeVisible()
  expect(screen.queryByText('Unlimited')).not.toBeInTheDocument()
})

test('upstream overview errors are visible and offer retry instead of silently hiding the pool', async () => {
  vi.spyOn(api, 'request').mockImplementation(async (config) => {
    if (config.url?.endsWith('/pools')) throw new Error('offline')
    return {
      data: { success: true, data: { server_time: 100, subscriptions: [] } },
    }
  })
  mount()
  expect(
    await screen.findByRole('button', { name: 'Retry' })
  ).toBeInTheDocument()
})

test('usage formatting responds to both Chinese interface locale aliases', async () => {
  const i18n = createInstance()
  await i18n.init({
    lng: 'zhCN',
    fallbackLng: 'en',
    resources: {
      en: { translation: {} },
      zhCN: { translation: {} },
      zhTW: { translation: {} },
    },
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  vi.spyOn(api, 'request').mockImplementation(async (config) => ({
    data: {
      success: true,
      data: config.url?.endsWith('/pools')
        ? []
        : {
            server_time: 100,
            subscriptions: [
              {
                subscription_id: 1,
                managed: true,
                status: 'active',
                pending: false,
                end_time: 1890000000,
                cycles: [
                  {
                    key: 'c',
                    name: 'Personal',
                    used: 1234,
                    adjustment: 0,
                    reserved: 0,
                    limit: 5000,
                    unlimited: false,
                    shared: false,
                    next_reset: 1890000000,
                  },
                ],
              },
            ],
          },
    },
  }))
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <SelfAlignedUsage />
      </QueryClientProvider>
    </I18nextProvider>
  )
  expect(await screen.findByText(/1,234/)).toBeInTheDocument()
  await act(async () => {
    await i18n.changeLanguage('zhTW')
  })
  expect(screen.getByText(/1,234/)).toBeInTheDocument()
})

test('unknown upstream capacity is not presented as unlimited or as a percentage', () => {
  render(
    <CycleProgress
      cycles={[
        {
          key: 'c',
          name: 'Upstream',
          seconds: 137,
          shared: false,
          unlimited: true,
          limit_unknown: true,
          limit: 0,
          used: 12,
          reserved: 0,
          adjustment: 0,
          start_time: 1,
          next_reset: 2,
          pending: false,
        },
      ]}
    />
  )
  expect(screen.getByText(/Unknown/)).toBeInTheDocument()
  expect(screen.queryByText(/Unlimited/)).not.toBeInTheDocument()
  expect(screen.queryByRole('progressbar')).not.toBeInTheDocument()
})

test('an upstream pool without a reported expiry displays never instead of the storage sentinel date', async () => {
  vi.spyOn(api, 'request').mockImplementation(async (config) => ({
    data: {
      success: true,
      data: config.url?.endsWith('/usage')
        ? {
            server_time: 100,
            subscriptions: [
              {
                subscription_id: 1,
                managed: true,
                status: 'active',
                end_time: 253402300799,
                cycles: [],
              },
            ],
          }
        : [
            {
              id: 1,
              name: 'No expiry pool',
              subscription_ids: [1],
              end_time: 253402300799,
              cycles: [],
              observations: [],
            },
          ],
    },
  }))
  mount()
  await screen.findByText('No expiry pool')
  expect(screen.getAllByText('Never')).toHaveLength(2)
  expect(screen.queryByText(/9999/)).not.toBeInTheDocument()
})
