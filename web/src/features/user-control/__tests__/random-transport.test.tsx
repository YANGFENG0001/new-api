import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { expect, test } from 'vitest'

import { ActionsEditor } from '../actions-editor'
import { ScheduleEditor } from '../schedule-editor'
import type { ControlActions, Schedule } from '../types'

function ActionsFixture() {
  const [value, setValue] = useState<ControlActions>({})
  return <ActionsEditor value={value} onChange={setValue} />
}

function ScheduleFixture() {
  const [value, setValue] = useState<Schedule | null>({
    timezone: 'UTC',
    rules: [{ id: 'window', limits: {} }],
  })
  return <ScheduleEditor value={value} onChange={setValue} />
}

test('transport controls are opt-in and disabling them removes the editable range', async () => {
  render(<ActionsFixture />)
  expect(screen.queryByLabelText('Minimum delay (ms)')).not.toBeInTheDocument()
  await userEvent.click(
    screen.getByRole('switch', { name: 'Random upstream delay' })
  )
  expect(screen.getByLabelText('Minimum delay (ms)')).toHaveValue(1000)
  fireEvent.change(screen.getByLabelText('Maximum delay (ms)'), {
    target: { value: '500' },
  })
  expect(screen.getByLabelText('Maximum delay (ms)')).toHaveAttribute(
    'aria-invalid',
    'true'
  )
  await userEvent.click(
    screen.getByRole('switch', { name: 'Random upstream delay' })
  )
  expect(screen.queryByLabelText('Minimum delay (ms)')).not.toBeInTheDocument()
  await userEvent.click(
    screen.getByRole('switch', { name: 'Buffered character delivery' })
  )
  expect(screen.getByLabelText('Minimum characters per second')).toHaveValue(10)
  expect(screen.getByLabelText('Maximum characters per second')).toHaveValue(30)
  await userEvent.click(
    screen.getByRole('switch', { name: 'Buffered character delivery' })
  )
  expect(
    screen.queryByLabelText('Minimum characters per second')
  ).not.toBeInTheDocument()
})

test('continuous random windows expose separate active and rest ranges and retain settings on toggle', async () => {
  render(<ScheduleFixture />)
  await userEvent.click(
    screen.getByRole('switch', { name: 'Random continuous activation' })
  )
  fireEvent.change(screen.getByLabelText('Minimum active duration (minutes)'), {
    target: { value: '8' },
  })
  expect(screen.getByLabelText('Minimum rest duration (minutes)')).toHaveValue(
    5
  )
  await userEvent.click(
    screen.getByRole('switch', { name: 'Random continuous activation' })
  )
  expect(
    screen.queryByLabelText('Minimum active duration (minutes)')
  ).not.toBeInTheDocument()
  await userEvent.click(
    screen.getByRole('switch', { name: 'Random continuous activation' })
  )
  expect(
    screen.getByLabelText('Minimum active duration (minutes)')
  ).toHaveValue(8)
})
