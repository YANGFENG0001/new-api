import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { PolicyEditorDialog } from '../policy-editor'
import { ScheduleEditor } from '../schedule-editor'
import type { Schedule } from '../types'

test('a behavior-only policy saves model choices without any schedule fields', async () => {
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true } })
  render(
    <PolicyEditorDialog
      open
      policy={null}
      defaultTimezone='UTC'
      onOpenChange={vi.fn()}
      onSaved={vi.fn()}
    />
  )
  await userEvent.type(screen.getByLabelText('Name'), 'Test')
  await userEvent.type(
    screen.getByLabelText('Allowed models (comma separated)'),
    'gpt-4{Enter}'
  )
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/user-control/policies',
      expect.objectContaining({
        schema_version: 2,
        schedule: null,
        limits: expect.objectContaining({ models: { allow: ['gpt-4'] } }),
      })
    )
  )
  expect(
    screen.queryByRole('button', { name: 'Add time window' })
  ).not.toBeInTheDocument()
})

function ScheduleFixture() {
  const [schedule, setSchedule] = useState<Schedule | null>(null)
  return (
    <ScheduleEditor
      value={schedule}
      onChange={setSchedule}
      defaultTimezone='UTC'
    />
  )
}

test('editing the timezone before adding the first window retains the typed timezone', async () => {
  render(<ScheduleFixture />)
  const timezone = screen.getByRole('textbox', { name: 'Schedule timezone' })
  await userEvent.clear(timezone)
  await userEvent.type(timezone, 'Asia/Tokyo')
  await userEvent.click(screen.getByRole('button', { name: 'Add time window' }))
  expect(timezone).toHaveValue('Asia/Tokyo')
})
