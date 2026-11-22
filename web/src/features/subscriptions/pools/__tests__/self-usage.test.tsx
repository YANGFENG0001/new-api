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
import { act, render, screen } from '@testing-library/react'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { CycleProgress } from '../cycle-progress'
import { SelfAlignedUsage } from '../self-usage'

const clients: QueryClient[] = []
afterEach(() => {
  clients.splice(0).forEach((client) => client.clear())
})

function mount() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  return render(
    <QueryClientProvider client={client}>
      <SelfAlignedUsage />
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
