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
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import type { Capabilities } from '../api'
import { emptySyncSource, type SyncMapping } from '../sync-api'
import { SyncObservations } from '../sync-observations'
import { SyncSourcesPanel } from '../sync-sources-panel'
import { SyncUsageCard, SyncSourceStatus, SyncAccounts } from '../sync-usage'
import { formatQuota } from '@/lib/format'

const now = Math.floor(Date.now() / 1000)
const mapping = { cycles: [] } as unknown as SyncMapping
const source = { ...emptySyncSource(mapping), id: 1, name: 'Panel' }
const clients: QueryClient[] = []
afterEach(() => clients.splice(0).forEach((client) => client.clear()))

test('placeholder accounts with null quotas or zero defaults and no observation metadata are not shown', () => {
  const base = {
    key: 'c',
    name: 'Empty cycle',
    seconds: 3600,
    window_start: 0,
    window_end: 0,
    sampled_at: 0,
    source: '',
    error: 'Missing or inconsistent upstream cycle data',
  }
  render(
    <SyncAccounts
      staleSeconds={300}
      accounts={[
        {
          id: 'empty-null',
          name: 'vm-null',
          expiry: 0,
          sampled_at: 0,
          cycles: [{ ...base, utilization: null }],
        },
        {
          id: 'empty-zero',
          name: 'vm-shell',
          expiry: 0,
          sampled_at: 0,
          cycles: [{ ...base, utilization: 0 }],
        },
        {
          id: 'real-zero',
          name: 'vm-live',
          expiry: 0,
          sampled_at: now,
          cycles: [
            {
              ...base,
              name: 'Real zero',
              utilization: 0,
              sampled_at: now,
              window_end: now + 1000,
              error: '',
            },
          ],
        },
      ]}
    />
  )
  expect(screen.queryByText('vm-null')).not.toBeInTheDocument()
  expect(screen.queryByText('vm-shell')).not.toBeInTheDocument()
  expect(screen.getByText('vm-live')).toBeVisible()
  expect(
    screen.getByRole('progressbar', { name: 'Real zero' })
  ).toHaveAttribute('aria-valuenow', '0')
})

async function openAccounts() {
  vi.spyOn(api, 'request').mockImplementation(async (config) => ({
    data: {
      success: true,
      data: config.url?.endsWith('/sync-sources')
        ? { sources: [source], template: mapping, privileged: true }
        : {
            accounts: [
              {
                id: 'account-1',
                name: 'vm-01',
                sampled_at: now,
                expiry: 0,
                cycles: [
                  {
                    key: 'short',
                    name: '5h',
                    seconds: 18000,
                    utilization: 0,
                    window_start: now - 10,
                    window_end: now + 17990,
                    sampled_at: now,
                    source: 'headers',
                    error: '',
                  },
                  {
                    key: 'week',
                    name: '7d',
                    seconds: 604800,
                    utilization: 0.26,
                    window_start: now - 10,
                    window_end: now + 604790,
                    sampled_at: now,
                    source: 'headers',
                    error: '',
                  },
                  {
                    key: 'missing',
                    name: 'Sonnet',
                    seconds: 604800,
                    utilization: null,
                    window_start: 0,
                    window_end: 0,
                    sampled_at: 0,
                    source: '',
                    error: 'Upstream cycle data is missing or inconsistent',
                  },
                ],
              },
            ],
            events: [],
          },
    },
  }))
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <SyncSourcesPanel caps={{ sync_read: true } as Capabilities} />
    </QueryClientProvider>
  )
  await userEvent.click(await screen.findByRole('button', { name: 'Panel' }))
}

test('account quotas show separate progress bars and reset times while preserving an explicit zero', async () => {
  await openAccounts()
  expect(
    await screen.findByRole('progressbar', { name: '5h' })
  ).toHaveAttribute('aria-valuenow', '0')
  expect(screen.getByRole('progressbar', { name: '7d' })).toHaveAttribute(
    'aria-valuenow',
    '26'
  )
  expect(screen.getAllByText('Next reset time')).toHaveLength(2)
  expect(screen.getByText('26%')).toBeVisible()
  expect(screen.getAllByText('0%')).toHaveLength(1)
})

test('unreported optional cycles are hidden rather than displayed as empty subscriptions', async () => {
  await openAccounts()
  expect(await screen.findByRole('progressbar', { name: '5h' })).toBeVisible()
  expect(
    screen.queryByRole('region', { name: 'Sonnet' })
  ).not.toBeInTheDocument()
})

test('an observation without timestamps never renders the Unix epoch or an invented reset countdown', () => {
  render(
    <SyncObservations
      observations={[
        {
          key: 'x',
          name: 'Missing',
          utilization: null,
          sampled_at: 0,
          fetched_at: 0,
          window_end: 0,
          stale: true,
          pending: '',
        },
      ]}
    />
  )
  expect(screen.queryByText(/1970/)).not.toBeInTheDocument()
  expect(screen.queryByText('Reset time unavailable')).not.toBeInTheDocument()
  expect(screen.queryByRole('progressbar')).not.toBeInTheDocument()
})

test('over-limit usage retains its actual percentage while the progress bar stops at full capacity', () => {
  render(
    <SyncUsageCard
      name='Over limit'
      utilization={1.25}
      windowEnd={now + 3600}
      sampledAt={now}
      stale={false}
    />
  )
  expect(screen.getByText('125%')).toBeVisible()
  expect(screen.getByRole('progressbar')).toHaveAttribute(
    'aria-valuenow',
    '100'
  )
})

test.each([
  ['zhCN', 'zh-CN'],
  ['zhTW', 'zh-TW'],
  ['en', 'en'],
  ['fr', 'fr'],
  ['ru', 'ru'],
  ['ja', 'ja'],
  ['vi', 'vi'],
  ['invalid_locale!', undefined],
])(
  'timestamps respond to switching the interface to %s without changing the observation',
  async (language, locale) => {
    const view = render(
      <SyncUsageCard
        name='Window'
        utilization={0.625}
        windowEnd={now + 3600}
        sampledAt={now}
        stale={false}
      />
    )
    try {
      await act(async () => {
        await i18next.changeLanguage(language)
      })
      const expected = new Date((now + 3600) * 1000).toLocaleString(locale)
      expect(
        await screen.findByText(expected.replaceAll(/\s+/g, ' '))
      ).toBeVisible()
      expect(screen.getByRole('progressbar')).toHaveAttribute(
        'aria-valuenow',
        '62.5'
      )
    } finally {
      view.unmount()
      await i18next.changeLanguage('en')
    }
  }
)

test('a recent successful poll and an old upstream snapshot have distinct status indicators', () => {
  render(
    <SyncSourceStatus
      source={{
        ...source,
        last_fetch: now,
        fetch_status: 'Synchronization is on schedule',
        observation_status: 'Upstream observation is stale',
      }}
    />
  )
  expect(screen.getByText('Synchronization is on schedule')).toBeVisible()
  expect(screen.getByText('Upstream observation is stale')).toBeVisible()
  expect(
    screen.getByText('Poll every 60s; observations expire after 300s.')
  ).toBeVisible()
})

test('a hinted sample can show a valid percentage without presenting an exact latest observation', () => {
  render(
    <SyncUsageCard
      name='Hinted window'
      utilization={0.26}
      windowEnd={now + 3600}
      sampledAt={0}
      sampleTimeQuality='hint'
      issueCode='sample_time_incomplete'
      stale={false}
    />
  )

  expect(screen.getByText('26%')).toBeVisible()
  expect(screen.getByText('Sampling time incomplete')).toBeVisible()
  expect(screen.queryByText('Up to date')).not.toBeInTheDocument()
  expect(screen.queryByText(/1970/)).not.toBeInTheDocument()
})

test('source status reports an incomplete sample separately from a successful poll', () => {
  render(
    <SyncSourceStatus
      source={{
        ...source,
        last_fetch: now,
        fetch_status: 'Synchronization is on schedule',
        observation_status: 'Upstream sample time is incomplete',
      }}
    />
  )

  expect(screen.getByText('Synchronization is on schedule')).toBeVisible()
  expect(screen.getByText('Upstream sample time is incomplete')).toBeVisible()
  expect(screen.queryByText('Snapshot within freshness window')).not.toBeInTheDocument()
})

test('a calibrated progress card separates upstream quality from local quota metering', () => {
  render(
    <SyncObservations
      observations={[
        {
          key: 'short',
          name: '5h',
          utilization: 0.4,
          estimated_utilization: 0.45,
          local_delta: 50,
          sampled_at: now,
          fetched_at: now,
          window_end: now + 18000,
          stale: false,
          pending: '',
        },
      ]}
    />
  )
  expect(screen.getByRole('progressbar', { name: '5h' })).toHaveAttribute(
    'aria-valuenow',
    '40'
  )
  expect(screen.getByText('Upstream utilization')).toBeVisible()
  expect(screen.getByText('Local metering since calibration')).toBeVisible()
  expect(screen.getByText(formatQuota(50))).toBeVisible()
  expect(screen.getByText('Estimated utilization after local metering')).toBeVisible()
  expect(screen.getByText('45%')).toBeVisible()
})
