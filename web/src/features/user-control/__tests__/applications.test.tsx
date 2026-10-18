import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { ApplicationEditor } from '../application-editor'
import { parseTargets } from '../application-targets'
import { equalProbabilities, validPoolConfig } from '../pool-validation'
import { ResourceCatalogContext } from '../resource-catalog'
import { ResourceSelector } from '../resource-selector'
import { ScheduledPoolEditor } from '../scheduled-pool-editor'
import type { PolicyView, PoolView } from '../types'

const policy: PolicyView = {
  policy: {
    id: 7,
    name: 'Base policy',
    description: '',
    enabled: true,
    priority: 0,
    limits: '{}',
    schedule: '',
    version: 1,
    created_by: 1,
    updated_by: 1,
    created_at: 0,
    updated_at: 0,
  },
  limits: {},
}
const pool: PoolView = {
  pool: {
    id: 3,
    name: 'Evening pool',
    description: '',
    enabled: true,
    priority: 0,
    version: 1,
  },
  config: {
    probability: 6000,
    hold_min: 5,
    hold_max: 10,
    members: [{ policy_id: 7, probability: 10000 }],
  },
}

test('application editor distinguishes all-day policies from scheduled pools and saves typed targets', async () => {
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  render(
    <ApplicationEditor
      owner={{ kind: 'list', id: 4, name: 'Trial users' }}
      policies={[policy]}
      pools={[pool]}
      canRelax={false}
      onClose={vi.fn()}
      onSaved={vi.fn()}
    />
  )
  await userEvent.click(screen.getByLabelText('Applied policies and pools'))
  await userEvent.click(
    await screen.findByRole('option', { name: 'Policy: Base policy' })
  )
  await userEvent.click(
    await screen.findByRole('option', { name: 'Strategy pool: Evening pool' })
  )
  await userEvent.keyboard('{Escape}')
  expect(screen.queryByLabelText('Requests per minute')).not.toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: 'Add time window' })
  ).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith(
      '/api/user-control/applications/list/4',
      expect.objectContaining({
        version: 0,
        targets: [
          { kind: 'policy', id: 7 },
          { kind: 'pool', id: 3 },
        ],
        mode: 'merge',
      })
    )
  )
})

test('legacy migration requires an explicit acknowledgement before changing bindings', async () => {
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  render(
    <ApplicationEditor
      owner={{ kind: 'user', id: 7, name: 'Alice' }}
      legacy='old limits'
      policies={[]}
      pools={[]}
      canRelax={false}
      onClose={vi.fn()}
      onSaved={vi.fn()}
    />
  )
  expect(
    screen.getByRole('switch', {
      name: 'Replace inherited user-control settings',
    })
  ).toHaveAttribute('aria-disabled', 'true')
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  expect(await screen.findByRole('alert')).toHaveTextContent(
    'Review and confirm replacement of the legacy configuration'
  )
  expect(put).not.toHaveBeenCalled()
  await userEvent.click(
    screen.getByRole('switch', {
      name: 'I reviewed the legacy configuration and confirm the switch',
    })
  )
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith(
      '/api/user-control/applications/user/7',
      expect.objectContaining({ confirm_legacy: true })
    )
  )
})

test('batch append shows per-user failures and retry does not repeat successful writes', async () => {
  let failed = true
  const put = vi.spyOn(api, 'put').mockImplementation(async (url) => ({
    data:
      url.endsWith('/8') && failed
        ? { success: false, message: 'temporary failure' }
        : { success: true },
  }))
  const close = vi.fn()
  render(
    <ApplicationEditor
      owner={{ kind: 'user', id: 7, name: 'Selected users' }}
      bulkUsers={[7, 8]}
      applications={[]}
      policies={[policy]}
      pools={[]}
      canRelax={false}
      onClose={close}
      onSaved={vi.fn()}
    />
  )
  expect(
    screen.getByRole('combobox', { name: 'Batch binding operation' })
  ).toHaveTextContent('Append bindings')
  await userEvent.click(screen.getByLabelText('Applied policies and pools'))
  await userEvent.click(
    await screen.findByRole('option', { name: 'Policy: Base policy' })
  )
  await userEvent.keyboard('{Escape}')
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  expect(await screen.findByRole('alert')).toHaveTextContent(
    'temporary failure'
  )
  expect(close).not.toHaveBeenCalled()
  failed = false
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(close).toHaveBeenCalled())
  expect(put.mock.calls.filter(([url]) => url.endsWith('/7'))).toHaveLength(1)
  expect(put.mock.calls.filter(([url]) => url.endsWith('/8'))).toHaveLength(2)
  expect(put).toHaveBeenCalledWith(
    '/api/user-control/applications/user/7',
    expect.objectContaining({ operation: 'append' })
  )
})

test('invalid saved targets are rejected rather than silently converted to unrestricted access', () => {
  expect(() => parseTargets('{bad-json')).toThrow()
  expect(() => parseTargets('[{"kind":"channel","id":1}]')).toThrow()
  expect(parseTargets('[]')).toEqual([])
})

test('channel resource selection preserves missing references and does not allow invented IDs', async () => {
  const change = vi.fn()
  render(
    <ResourceCatalogContext.Provider
      value={{
        models: ['model-a'],
        channels: [
          {
            id: 12,
            name: 'Primary',
            type: 1,
            status: 1,
            models: 'model-a',
            group: 'default',
          },
        ],
      }}
    >
      <label htmlFor='channels'>Channels</label>
      <ResourceSelector
        id='channels'
        kind='channel'
        value={['99']}
        onChange={change}
      />
    </ResourceCatalogContext.Provider>
  )
  expect(screen.getByText('99')).toBeVisible()
  await userEvent.click(screen.getByLabelText('Channels'))
  await userEvent.click(
    await screen.findByRole('option', { name: 'Primary #12' })
  )
  expect(change).toHaveBeenCalledWith(['99', '12'])
  change.mockClear()
  await userEvent.type(
    screen.getByLabelText('Channels'),
    'not-a-channel{Enter}'
  )
  expect(change).not.toHaveBeenCalled()
})

test('equal distribution preserves exactly 100 percent and probability endpoints remain valid', () => {
  const members = equalProbabilities([1, 2, 3])
  expect(members.map((m) => m.probability)).toEqual([3334, 3333, 3333])
  const config = { members, probability: 0, hold_min: 1, hold_max: 10 }
  expect(validPoolConfig(config)).toBe(true)
  expect(validPoolConfig({ ...config, probability: 10000 })).toBe(true)
  expect(validPoolConfig({ ...config, probability: 10001 })).toBe(false)
  expect(
    validPoolConfig({
      ...config,
      members: members.map((m) => ({ ...m, probability: 0 })),
    })
  ).toBe(false)
})

test('pool editor saves its own timing and references without copying policy behavior', async () => {
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  render(
    <ScheduledPoolEditor
      open
      pool={pool}
      policies={[policy]}
      defaultTimezone='UTC'
      onOpenChange={vi.fn()}
      onSaved={vi.fn()}
    />
  )
  expect(screen.queryByLabelText('Requests per minute')).not.toBeInTheDocument()
  const probability = screen.getByRole('spinbutton', {
    name: 'Pool activation probability (%)',
  })
  await userEvent.clear(probability)
  await userEvent.type(probability, '0')
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith(
      '/api/user-control/pools/3',
      expect.objectContaining({
        config: expect.objectContaining({
          probability: 0,
          members: [{ policy_id: 7, probability: 10000 }],
        }),
      })
    )
  )
})
