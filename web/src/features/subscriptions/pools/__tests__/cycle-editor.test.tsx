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
import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { useState } from 'react'
import { I18nextProvider } from 'react-i18next'
import { expect, test, vi } from 'vitest'

import { useSystemConfigStore } from '@/stores/system-config-store'

import { CycleEditor } from '../cycle-editor'
import { QuotaAmountInput } from '../quota-amount-input'
import { newCycle } from '../schema'

function Editor(props: { pooled?: boolean; disabled?: boolean }) {
  const [cycles, setCycles] = useState([
    newCycle('Short'),
    { ...newCycle('Long'), seconds: 604800 },
  ])
  return (
    <CycleEditor
      {...props}
      value={cycles}
      onChange={setCycles}
      lockedKeys={[cycles[0].key]}
    />
  )
}

test('sharing is opt-in per cycle and does not change other cycles', async () => {
  render(<Editor pooled />)
  const short = within(screen.getByRole('region', { name: 'Short' }))
  const long = within(screen.getByRole('region', { name: 'Long' }))
  expect(
    short.getByRole('switch', { name: 'Shared by all pool members' })
  ).not.toBeChecked()
  await userEvent.click(
    short.getByRole('switch', { name: 'Shared by all pool members' })
  )
  expect(
    short.getByRole('switch', { name: 'Shared by all pool members' })
  ).toBeChecked()
  expect(
    long.getByRole('switch', { name: 'Shared by all pool members' })
  ).not.toBeChecked()
  expect(
    short.queryByRole('switch', { name: 'Enforce upstream total quota' })
  ).not.toBeInTheDocument()
  expect(
    long.getByRole('switch', { name: 'Enforce upstream total quota' })
  ).toBeInTheDocument()
})

test('existing cycle deletion is disabled while a new cycle can be removed', async () => {
  render(<Editor pooled />)
  expect(
    within(screen.getByRole('region', { name: 'Short' })).getByRole('button', {
      name: 'Remove',
    })
  ).toBeDisabled()
  await userEvent.click(
    within(screen.getByRole('region', { name: 'Long' })).getByRole('button', {
      name: 'Remove',
    })
  )
  expect(screen.queryByRole('region', { name: 'Long' })).not.toBeInTheDocument()
})

test('independent subscription editors never offer a pool sharing switch', () => {
  render(<Editor />)
  expect(
    screen.queryByRole('switch', { name: 'Shared by all pool members' })
  ).not.toBeInTheDocument()
})

test('date clear controls follow the selected interface language', async () => {
  const i18n = createInstance()
  await i18n.init({
    lng: 'zh',
    fallbackLng: 'en',
    resources: {
      zh: { translation: { Clear: '清除' } },
      en: { translation: {} },
    },
  })
  render(
    <I18nextProvider i18n={i18n}>
      <Editor />
    </I18nextProvider>
  )
  expect(screen.getAllByRole('button', { name: '清除' })).toHaveLength(2)
  await act(async () => {
    await i18n.changeLanguage('en')
  })
  expect(screen.getAllByRole('button', { name: 'Clear' })).toHaveLength(2)
})

test('an imported unlimited cycle accepts a local monetary limit without an upstream capacity', async () => {
  const change = vi.fn()
  const rule = { ...newCycle('Imported'), limit: 0, unlimited: true }
  render(<CycleEditor value={[rule]} onChange={change} pooled />)
  const amount = screen.getByLabelText('Quota units')
  expect(amount).toBeEnabled()
  await userEvent.click(
    screen.getByRole('combobox', { name: 'Quota units · Input unit' })
  )
  await userEvent.click(await screen.findByRole('option', { name: 'USD ($)' }))
  await userEvent.clear(amount)
  await userEvent.type(amount, '2.50')
  expect(change.mock.lastCall?.[0][0]).toMatchObject({
    limit: 1250000,
    unlimited: false,
  })
})

test('changing the input currency preserves stored quota and CNY uses the configured exchange rate', async () => {
  const original = useSystemConfigStore.getState().config.currency
  useSystemConfigStore.getState().setConfig({
    currency: { ...original, quotaPerUnit: 500000, usdExchangeRate: 7 },
  })
  try {
    const change = vi.fn()
    render(
      <CycleEditor
        value={[{ ...newCycle('Local'), limit: 1250000 }]}
        onChange={change}
      />
    )
    const amount = screen.getByLabelText('Quota units')
    await userEvent.click(
      screen.getByRole('combobox', { name: 'Quota units · Input unit' })
    )
    await userEvent.click(
      await screen.findByRole('option', { name: 'CNY (￥)' })
    )
    expect(amount).toHaveValue(17.5)
    expect(change).not.toHaveBeenCalled()
    await userEvent.clear(amount)
    await userEvent.type(amount, '21')
    expect(change.mock.lastCall?.[0][0].limit).toBe(1500000)
  } finally {
    useSystemConfigStore.getState().setConfig({ currency: original })
  }
})

test('invalid or unsafe quota amounts cannot be turned into valid saved integers', async () => {
  const change = vi.fn()
  render(
    <QuotaAmountInput
      id='test-amount'
      label='Capacity'
      value={0}
      onChange={change}
    />
  )
  const input = screen.getByLabelText('Capacity')
  await userEvent.clear(input)
  await userEvent.type(input, '9007199254740992')
  expect(change.mock.lastCall?.[0]).toBeNaN()
  await userEvent.click(
    screen.getByRole('combobox', { name: 'Capacity · Input unit' })
  )
  await userEvent.click(
    await screen.findByRole('option', { name: 'Quota units' })
  )
  await userEvent.clear(input)
  await userEvent.type(input, '1.5')
  expect(change.mock.lastCall?.[0]).toBeNaN()
})
