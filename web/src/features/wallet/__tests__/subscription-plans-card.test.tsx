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
import { render, screen } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'

import type { Usage } from '@/features/subscriptions/pools/api'
import { api } from '@/lib/api'

import { SubscriptionPlansCard } from '../components/subscription-plans-card'

const clients: QueryClient[] = []
afterEach(() => clients.splice(0).forEach((client) => client.clear()))

test.each(['legacy', 'independent', 'pooled', 'mixed'] as const)(
  '%s subscriptions keep their own relevant usage without a duplicate upstream accordion',
  async (mode) => {
    const now = Math.floor(Date.now() / 1000)
    const cycle = {
      key: 'short',
      name: '7h',
      seconds: 25200,
      shared: true,
      unlimited: true,
      limit: 0,
      used: 0,
      reserved: 0,
      adjustment: 0,
      start_time: now - 1000,
      next_reset: now + 1000,
      pending: false,
    }
    const managed = mode !== 'legacy'
    const pooled = mode === 'pooled' || mode === 'mixed'
    const usage: Usage = {
      subscription_id: 9,
      plan_id: 2,
      status: 'active',
      managed,
      pending: false,
      end_time: now + 86400,
      cycles: managed ? [cycle] : [],
    }
    if (mode === 'independent') {
      usage.cycles = [
        {
          ...cycle,
          shared: false,
          name: 'Personal custom cycle',
          unlimited: false,
          limit: 100,
          used: 7,
        },
      ]
    }
    const subscriptions = [
      {
        subscription: {
          id: 9,
          plan_id: 2,
          status: 'active',
          end_time: now + 86400,
          amount_total: 1000000,
          amount_used: 1000,
        },
      },
    ]
    const usages = [usage]
    if (mode === 'mixed') {
      subscriptions.push({
        subscription: { ...subscriptions[0].subscription, id: 10, plan_id: 3 },
      })
      usages.push({
        ...usage,
        subscription_id: 10,
        plan_id: 3,
        managed: false,
        cycles: [],
      })
    }
    vi.spyOn(api, 'get').mockImplementation(async (url) => ({
      data: {
        success: true,
        data: url.endsWith('/plans')
          ? []
          : {
              billing_preference: 'subscription_first',
              subscriptions,
              all_subscriptions: subscriptions,
            },
      },
    }))
    const overview = pooled
      ? [
          {
            id: 1,
            name: 'Carpool summary',
            subscription_ids: [9],
            end_time: now + 86400,
            cycles: [cycle],
            observations: [],
            sync_observations: [
              {
                key: 'short',
                name: '7h',
                seconds: 25200,
                utilization: 0.31,
                window_end: now + 1000,
                sampled_at: now,
                fetched_at: now,
                stale: false,
                pending: '',
              },
            ],
          },
        ]
      : []
    vi.spyOn(api, 'request').mockImplementation(async (config) => ({
      data: {
        success: true,
        data: config.url?.endsWith('/usage')
          ? { server_time: now, subscriptions: usages }
          : overview,
      },
    }))
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    clients.push(client)
    render(
      <QueryClientProvider client={client}>
        <SubscriptionPlansCard topupInfo={null} />
      </QueryClientProvider>
    )
    await screen.findByText('My Subscriptions')
    if (pooled) {
      await screen.findByText('Carpool summary')
      expect(screen.getAllByText('7h')).toHaveLength(1)
      expect(screen.getByText('31%')).toBeVisible()
      expect(screen.queryByText(/0 \/ Unlimited/)).not.toBeInTheDocument()
    }
    if (mode === 'independent') {
      expect(await screen.findByText(/7 \/ 100/)).toBeVisible()
    }
    if (mode === 'legacy' || mode === 'mixed') {
      expect(
        await screen.findByText('Total Quota', { exact: false })
      ).toBeVisible()
    } else {
      expect(
        screen.queryByText('Total Quota', { exact: false })
      ).not.toBeInTheDocument()
    }
    expect(
      screen.queryByRole('button', { name: 'Upstream carpool subscriptions' })
    ).not.toBeInTheDocument()
  }
)
