import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, test, vi } from 'vitest'

import { OverrideDialog } from '../override-dialog'
import { PolicyEditorDialog } from '../policy-editor'

test('opening a policy keeps the body scrollable and its actions outside the scroll area', () => {
  render(
    <PolicyEditorDialog
      open
      policy={null}
      defaultTimezone='UTC'
      onOpenChange={vi.fn()}
      onSaved={vi.fn()}
    />
  )
  const dialog = screen.getByRole('dialog', { name: 'New control policy' })
  expect(dialog).toHaveClass(
    'max-h-[var(--dialog-available-height,calc(100dvh-2rem))]'
  )
  const name = screen.getByRole('textbox', { name: 'Name' })
  const body = name.closest('form')?.parentElement?.parentElement
  expect(body).toHaveClass('min-h-0', 'overflow-y-auto')
  expect(body?.contains(screen.getByRole('button', { name: 'Save' }))).toBe(
    false
  )
  expect(
    screen.getByRole('spinbutton', { name: 'Requests per minute' })
  ).toHaveAttribute('placeholder', '0 = unlimited')
})

test('choosing a policy displays its name and keeps replace disabled without relax permission', async () => {
  const policy = {
    id: 42,
    name: 'Trial limits',
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
  }
  render(
    <OverrideDialog
      userId={7}
      existing={null}
      policies={[{ policy, limits: {} }]}
      defaultTimezone='UTC'
      canRelax={false}
      onClose={vi.fn()}
      onSaved={vi.fn()}
    />
  )
  const template = screen.getByRole('combobox', { name: 'Policy template' })
  await userEvent.click(template)
  await userEvent.click(
    await screen.findByRole('option', { name: 'Trial limits' })
  )
  expect(template).toHaveTextContent('Trial limits')
  expect(template).not.toHaveTextContent('42')
  await userEvent.click(screen.getByRole('combobox', { name: 'Mode' }))
  expect(
    await screen.findByRole('option', {
      name: 'Replace (relax inherited limits)',
    })
  ).toHaveAttribute('aria-disabled', 'true')
})

test('adding a window uses the configured timezone and exposes accessible weekday selection', async () => {
  render(
    <PolicyEditorDialog
      open
      policy={null}
      defaultTimezone='UTC'
      onOpenChange={vi.fn()}
      onSaved={vi.fn()}
    />
  )
  await userEvent.click(screen.getByRole('button', { name: 'Add time window' }))
  expect(
    screen.getByRole('textbox', { name: 'Schedule timezone' })
  ).toHaveValue('UTC')
  const monday = screen.getByRole('button', { name: 'Mon' })
  expect(monday).toHaveAttribute('aria-pressed', 'false')
  await userEvent.click(monday)
  expect(monday).toHaveAttribute('aria-pressed', 'true')
  expect(screen.getByLabelText('Start date')).toHaveAttribute('type', 'date')
})

test('opening an override uses the shared responsive dialog and named project select controls', () => {
  render(
    <OverrideDialog
      userId={7}
      existing={null}
      policies={[]}
      defaultTimezone='UTC'
      canRelax={false}
      onClose={vi.fn()}
      onSaved={vi.fn()}
    />
  )
  const dialog = screen.getByRole('dialog', { name: 'User override #7' })
  expect(dialog).toHaveClass(
    'max-h-[var(--dialog-available-height,calc(100dvh-2rem))]'
  )
  expect(
    within(dialog).getByRole('combobox', { name: 'Policy template' })
  ).toHaveAttribute('data-slot', 'select-trigger')
  expect(within(dialog).getByRole('combobox', { name: 'Mode' })).toBeEnabled()
  expect(screen.getByLabelText('Expires at (optional)')).toHaveAttribute(
    'type',
    'datetime-local'
  )
})
