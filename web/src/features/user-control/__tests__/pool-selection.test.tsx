import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { PolicyEditorDialog } from '../policy-editor'
import type { PolicyView } from '../types'

const policies: PolicyView[] = [
  {
    policy: {
      id: 7,
      name: 'Text policy',
      enabled: true,
      description: '',
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
  },
  {
    policy: {
      id: 9,
      name: 'Existing pool',
      enabled: true,
      description: '',
      priority: 0,
      limits: '{}',
      schedule: '',
      version: 1,
      created_by: 1,
      updated_by: 1,
      created_at: 0,
      updated_at: 0,
    },
    limits: { random_pool: { policy_ids: [7], hold_min: 5, hold_max: 10 } },
  },
]

test('pool creation selects existing complete policies and saves a reusable pool payload', async () => {
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true } })
  render(
    <PolicyEditorDialog
      open
      createPool
      policy={null}
      policies={policies}
      defaultTimezone='UTC'
      onSaved={vi.fn()}
      onOpenChange={vi.fn()}
    />
  )
  expect(
    screen.queryByRole('checkbox', { name: 'Existing pool' })
  ).not.toBeInTheDocument()
  await userEvent.type(screen.getByLabelText('Name'), 'Reusable')
  await userEvent.click(screen.getByRole('checkbox', { name: 'Text policy' }))
  fireEvent.change(
    screen.getByLabelText('Minimum policy holding duration (minutes)'),
    { target: { value: '10' } }
  )
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/user-control/policies',
      expect.objectContaining({
        limits: {
          random_pool: { policy_ids: [7], hold_min: 10, hold_max: 15 },
        },
      })
    )
  )
})

test('an empty pool is not saved and its ordinary policy candidates remain selectable', async () => {
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true } })
  render(
    <PolicyEditorDialog
      open
      createPool
      policy={null}
      policies={policies}
      defaultTimezone='UTC'
      onSaved={vi.fn()}
      onOpenChange={vi.fn()}
    />
  )
  await userEvent.type(screen.getByLabelText('Name'), 'Empty pool')
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  expect(post).not.toHaveBeenCalled()
  expect(
    screen.getByRole('checkbox', { name: 'Text policy' })
  ).not.toBeChecked()
})
