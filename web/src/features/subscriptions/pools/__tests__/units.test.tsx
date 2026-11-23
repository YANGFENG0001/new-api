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
import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { CycleProgress } from '../cycle-progress'
import { QuotaAmountInput } from '../quota-amount-input'

const UNIT_STORAGE_KEY =
  'new-api:subscription-quota-input-unit:v1:7:period-quota'

function configureCurrency(
  overrides: Partial<typeof DEFAULT_CURRENCY_CONFIG>
): void {
  useSystemConfigStore.getState().setConfig({
    currency: { ...DEFAULT_CURRENCY_CONFIG, ...overrides },
  })
}

function signInAdmin(id = 7): void {
  useAuthStore.getState().auth.setUser({
    id,
    username: `admin-${id}`,
    role: 100,
  })
}

beforeEach(() => {
  window.localStorage.clear()
  configureCurrency({})
  useAuthStore.getState().auth.reset('complete')
})

afterEach(() => {
  useAuthStore.getState().auth.reset('complete')
  window.localStorage.clear()
  configureCurrency({})
})

test('a new input defaults to the site USD unit while retaining canonical quota', async () => {
  signInAdmin()
  const change = vi.fn()
  render(
    <QuotaAmountInput
      id='period-quota'
      label='Capacity'
      value={1250000}
      onChange={change}
    />
  )
  const input = screen.getByLabelText('Capacity')
  expect(input).toHaveValue(2.5)
  await userEvent.clear(input)
  await userEvent.type(input, '3')
  expect(change).toHaveBeenLastCalledWith(1500000)
})

test('site CNY and TOKENS defaults use CNY and raw quota respectively', () => {
  signInAdmin()
  configureCurrency({ quotaDisplayType: 'CNY', usdExchangeRate: 7 })
  const view = render(
    <QuotaAmountInput
      id='period-quota'
      label='Capacity'
      value={1250000}
      onChange={vi.fn()}
    />
  )
  expect(screen.getByLabelText('Capacity')).toHaveValue(17.5)
  view.unmount()

  configureCurrency({ quotaDisplayType: 'TOKENS' })
  render(
    <QuotaAmountInput
      id='period-quota'
      label='Capacity'
      value={1250000}
      onChange={vi.fn()}
    />
  )
  expect(screen.getByLabelText('Capacity')).toHaveValue(1250000)
})

test('a selected unit persists across remounts for the same admin and field', async () => {
  signInAdmin()
  configureCurrency({ usdExchangeRate: 7 })
  const first = render(
    <QuotaAmountInput
      id='period-quota'
      label='Capacity'
      value={1250000}
      onChange={vi.fn()}
    />
  )
  await userEvent.click(
    screen.getByRole('combobox', { name: 'Capacity · Input unit' })
  )
  await userEvent.click(await screen.findByRole('option', { name: 'CNY (￥)' }))
  expect(window.localStorage.getItem(UNIT_STORAGE_KEY)).toBe('CNY')
  await waitFor(() =>
    expect(screen.getByLabelText('Capacity')).toHaveValue(17.5)
  )

  first.unmount()
  render(
    <QuotaAmountInput
      id='period-quota'
      label='Capacity'
      value={1250000}
      onChange={vi.fn()}
    />
  )
  expect(screen.getByLabelText('Capacity')).toHaveValue(17.5)
})

test('unit preferences remain scoped to the current admin and stable field id', async () => {
  signInAdmin()
  window.localStorage.setItem(UNIT_STORAGE_KEY, 'CNY')
  const view = render(
    <QuotaAmountInput
      id='other-quota'
      label='Other capacity'
      value={1250000}
      onChange={vi.fn()}
    />
  )
  expect(screen.getByLabelText('Other capacity')).toHaveValue(2.5)
  view.unmount()

  signInAdmin(8)
  render(
    <QuotaAmountInput
      id='period-quota'
      label='Capacity'
      value={1250000}
      onChange={vi.fn()}
    />
  )
  expect(screen.getByLabelText('Capacity')).toHaveValue(2.5)
})

test('changing units never emits amount changes or drifts canonical quota', async () => {
  signInAdmin()
  configureCurrency({ usdExchangeRate: 7 })
  const change = vi.fn()
  render(
    <QuotaAmountInput
      id='period-quota'
      label='Capacity'
      value={1250000}
      onChange={change}
    />
  )
  const input = screen.getByLabelText('Capacity')
  const selector = screen.getByRole('combobox', {
    name: 'Capacity · Input unit',
  })

  expect(input).toHaveValue(2.5)
  await userEvent.click(selector)
  await userEvent.click(await screen.findByRole('option', { name: 'CNY (￥)' }))
  await waitFor(() => expect(input).toHaveValue(17.5))

  await userEvent.click(selector)
  await userEvent.click(
    await screen.findByRole('option', { name: 'Quota units' })
  )
  expect(input).toHaveValue(1250000)

  await userEvent.click(selector)
  await userEvent.click(await screen.findByRole('option', { name: 'USD ($)' }))
  expect(input).toHaveValue(2.5)
  expect(change).not.toHaveBeenCalled()
})

test('invalid stored preferences fall back to the site unit', () => {
  signInAdmin()
  window.localStorage.setItem(UNIT_STORAGE_KEY, 'EUR')
  configureCurrency({ quotaDisplayType: 'CNY', usdExchangeRate: 7 })
  render(
    <QuotaAmountInput
      id='period-quota'
      label='Capacity'
      value={1250000}
      onChange={vi.fn()}
    />
  )
  expect(screen.getByLabelText('Capacity')).toHaveValue(17.5)
})

test('blocked storage leaves unit selection usable with the site default', async () => {
  signInAdmin()
  configureCurrency({ usdExchangeRate: 7 })
  vi.spyOn(window.localStorage, 'getItem').mockImplementation(() => {
    throw new Error('storage blocked')
  })
  vi.spyOn(window.localStorage, 'setItem').mockImplementation(() => {
    throw new Error('storage full')
  })
  render(
    <QuotaAmountInput
      id='period-quota'
      label='Capacity'
      value={1250000}
      onChange={vi.fn()}
    />
  )
  const input = screen.getByLabelText('Capacity')
  expect(input).toHaveValue(2.5)
  await userEvent.click(
    screen.getByRole('combobox', { name: 'Capacity · Input unit' })
  )
  await userEvent.click(await screen.findByRole('option', { name: 'CNY (￥)' }))
  await waitFor(() => expect(input).toHaveValue(17.5))
})

test('cycle progress formats used, total, and reserved in the live site currency', () => {
  configureCurrency({
    quotaDisplayType: 'USD',
    quotaPerUnit: 500000,
    usdExchangeRate: 7,
  })
  render(
    <CycleProgress
      cycles={[
        {
          key: 'c',
          name: 'Current cycle',
          seconds: 3600,
          shared: false,
          unlimited: false,
          limit: 1000000,
          used: 250000,
          adjustment: 0,
          reserved: 100000,
          start_time: 1,
          next_reset: 2,
          pending: false,
        },
      ]}
    />
  )
  expect(screen.getByText(/Used: \$0\.5 \/ \$2/)).toBeVisible()
  expect(screen.getByText(/Reserved: \$0\.2/)).toBeVisible()
  expect(screen.queryByText(/Quota units/)).not.toBeInTheDocument()

  act(() => {
    configureCurrency({
      quotaDisplayType: 'CNY',
      quotaPerUnit: 500000,
      usdExchangeRate: 7,
    })
  })
  expect(screen.getByText(/Used: ¥3\.5 \/ ¥14/)).toBeVisible()
  expect(screen.getByText(/Reserved: ¥1\.4/)).toBeVisible()
})

test('a local estimate keeps its percentage and formats local delta as money', () => {
  configureCurrency({
    quotaDisplayType: 'USD',
    quotaPerUnit: 500000,
    usdExchangeRate: 7,
  })
  const end = Math.floor(Date.now() / 1000) + 3600
  render(
    <CycleProgress
      cycles={[
        {
          key: 'c',
          name: 'Current cycle',
          seconds: 3600,
          shared: false,
          unlimited: false,
          limit: 1000000,
          used: 250000,
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
          name: 'Current cycle',
          utilization: 0.2,
          sampled_at: end - 3000,
          fetched_at: end - 2900,
          window_end: end,
          stale: false,
          pending: '',
          local_window_end: end,
          local_estimated_utilization: 0.25,
          local_delta: 125000,
        },
      ]}
    />
  )
  expect(screen.getByText('25%')).toBeVisible()
  expect(screen.getByText(/Local delta: \$0\.25/)).toBeVisible()
})
test('pending cycle terms show active and next-cycle amounts without relabeling active usage', () => {
  configureCurrency({
    quotaDisplayType: 'USD',
    quotaPerUnit: 500000,
    usdExchangeRate: 7,
  })
  render(
    <CycleProgress
      cycles={[
        {
          key: 'c',
          name: 'Current cycle',
          seconds: 3600,
          shared: false,
          unlimited: false,
          limit: 1000000,
          used: 250000,
          adjustment: 0,
          reserved: 0,
          start_time: 1,
          next_reset: 2,
          pending: true,
          pending_rule: {
            name: 'Current cycle',
            seconds: 7200,
            shared: true,
            unlimited: false,
            limit: 1500000,
          },
        },
      ]}
    />
  )
  const cycle = screen.getByRole('region', { name: 'Current cycle' })
  expect(within(cycle).getByText(/Used: \$0\.5 \/ \$2/)).toBeVisible()
  expect(
    within(cycle).getByText('Active terms: 1 hour · $2 · Independent')
  ).toBeVisible()
  expect(
    within(cycle).getByText('Next cycle: 2 hours · $3 · Shared')
  ).toBeVisible()
})

test('cycles without pending changes do not show future-term noise', () => {
  render(
    <CycleProgress
      cycles={[
        {
          key: 'c',
          name: 'Current cycle',
          seconds: 3600,
          shared: false,
          unlimited: false,
          limit: 1000000,
          used: 250000,
          adjustment: 0,
          reserved: 0,
          start_time: 1,
          next_reset: 2,
          pending: false,
        },
      ]}
    />
  )
  expect(screen.queryByText(/Active terms:/)).not.toBeInTheDocument()
  expect(screen.queryByText(/Next cycle:/)).not.toBeInTheDocument()
})

test('a stable name keeps the selected unit when the rendered id changes after remount', async () => {
  signInAdmin()
  configureCurrency({ usdExchangeRate: 7 })
  const first = render(
    <QuotaAmountInput
      id='field-1'
      name='cycle-limit'
      label='Capacity'
      value={1250000}
      onChange={vi.fn()}
    />
  )
  await userEvent.click(
    screen.getByRole('combobox', { name: 'Capacity · Input unit' })
  )
  await userEvent.click(await screen.findByRole('option', { name: 'CNY (￥)' }))
  await waitFor(() =>
    expect(screen.getByLabelText('Capacity')).toHaveValue(17.5)
  )
  first.unmount()

  render(
    <QuotaAmountInput
      id='field-2'
      name='cycle-limit'
      label='Capacity'
      value={1250000}
      onChange={vi.fn()}
    />
  )
  expect(screen.getByLabelText('Capacity')).toHaveValue(17.5)
})

test('an explicit preference key survives changes to both form id and name', async () => {
  signInAdmin()
  configureCurrency({ usdExchangeRate: 7 })
  const first = render(
    <QuotaAmountInput
      id='field-a'
      preferenceKey='stable-cycle-limit'
      label='Capacity'
      value={1250000}
      onChange={vi.fn()}
    />
  )
  expect(screen.getByLabelText('Capacity')).toHaveAttribute(
    'name',
    'stable-cycle-limit'
  )
  await userEvent.click(
    screen.getByRole('combobox', { name: 'Capacity · Input unit' })
  )
  await userEvent.click(await screen.findByRole('option', { name: 'CNY (￥)' }))
  await waitFor(() =>
    expect(screen.getByLabelText('Capacity')).toHaveValue(17.5)
  )
  first.unmount()

  render(
    <QuotaAmountInput
      id='field-b'
      preferenceKey='stable-cycle-limit'
      label='Capacity'
      value={1250000}
      onChange={vi.fn()}
    />
  )
  expect(screen.getByLabelText('Capacity')).toHaveValue(17.5)
})
