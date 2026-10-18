import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { PolicyEditorDialog } from '../policy-editor'
import { ScheduleEditor } from '../schedule-editor'
import type { Schedule } from '../types'

test('typing comma separated model, group, channel and date values preserves separators and saves all entries with the footer action', async () => {
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
  await userEvent.type(screen.getByRole('textbox', { name: 'Name' }), 'Test')
  const allowed = screen.getByRole('textbox', {
    name: 'Allowed models (comma separated)',
  })
  await userEvent.type(allowed, 'gpt-4,')
  expect(allowed).toHaveValue('gpt-4,')
  await userEvent.type(allowed, ' claude')
  await userEvent.type(
    screen.getByRole('textbox', { name: 'Allowed groups' }),
    'default, premium'
  )
  await userEvent.type(
    screen.getByRole('textbox', { name: 'Allowed channel IDs' }),
    '12, 34'
  )
  await userEvent.click(screen.getByRole('button', { name: 'Add time window' }))
  await userEvent.type(
    screen.getByRole('textbox', {
      name: 'Specific dates (YYYY-MM-DD, comma separated)',
    }),
    '2026-10-18, 2026-10-19'
  )
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/user-control/policies',
      expect.objectContaining({
        limits: expect.objectContaining({
          models: { allow: ['gpt-4', 'claude'] },
          channels: {
            allow_groups: ['default', 'premium'],
            allow_ids: [12, 34],
          },
        }),
        schedule: expect.objectContaining({
          timezone: 'UTC',
          rules: [
            expect.objectContaining({ dates: ['2026-10-18', '2026-10-19'] }),
          ],
        }),
      })
    )
  )
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
