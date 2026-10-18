import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { ActionsEditor } from '../actions-editor'
import { LimitsEditor } from '../limits-editor'
import { LimitsSummary } from '../limits-summary'
import { PolicyEditorDialog } from '../policy-editor'
import { RuntimeAuditDetails } from '../runtime-audit-details'
import type { ControlActions, Limits } from '../types'

function RulesFixture() {
  const [limits, setLimits] = useState<Limits>({})
  return <LimitsEditor value={limits} onChange={setLimits} />
}

test('adding a scoped rule preserves comma-separated filters and offers limits without recursive rules', async () => {
  render(<RulesFixture />)
  await userEvent.click(screen.getByRole('button', { name: 'Add scoped rule' }))
  const rule = screen.getByRole('region', { name: 'Scoped rule 1' })
  await userEvent.type(
    within(rule).getByLabelText('Match original models'),
    'gpt,deepseek'
  )
  await userEvent.type(within(rule).getByLabelText('Match channel IDs'), '7,9')
  expect(within(rule).getByLabelText('Match original models')).toHaveValue(
    'gpt, deepseek'
  )
  expect(within(rule).getByLabelText('Match channel IDs')).toHaveValue('7,9')
  fireEvent.change(within(rule).getByLabelText('Match channel IDs'), {
    target: { value: 'invalid-channel' },
  })
  expect(within(rule).getByLabelText('Match channel IDs')).toHaveAttribute(
    'aria-invalid',
    'true'
  )
  expect(
    within(rule).getByText('scoped channel IDs must be positive')
  ).toBeVisible()
  fireEvent.change(within(rule).getByLabelText('Match channel IDs'), {
    target: { value: '9' },
  })
  expect(within(rule).getByLabelText('Match channel IDs')).not.toHaveAttribute(
    'aria-invalid'
  )
  expect(within(rule).getByLabelText('Requests per minute')).toBeVisible()
  expect(
    within(rule).queryByRole('button', { name: 'Add scoped rule' })
  ).not.toBeInTheDocument()
  expect(
    within(rule).queryByLabelText('Burst capacity')
  ).not.toBeInTheDocument()
  await userEvent.click(within(rule).getByRole('switch', { name: 'Enabled' }))
  expect(
    within(rule).getByRole('switch', { name: 'Enabled' })
  ).not.toBeChecked()
  await userEvent.click(
    within(rule).getByRole('button', { name: 'Remove scoped rule' })
  )
  expect(
    screen.queryByRole('region', { name: 'Scoped rule 1' })
  ).not.toBeInTheDocument()
})

test('saving a scheduled scoped rule sends only that window actions and a JSON template', async () => {
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
  await userEvent.type(screen.getByLabelText('Name'), 'Scoped schedule')
  await userEvent.click(screen.getByRole('button', { name: 'Add time window' }))
  const addButtons = screen.getAllByRole('button', { name: 'Add scoped rule' })
  await userEvent.click(addButtons[1])
  const rule = screen.getByRole('region', { name: 'Scoped rule 1' })
  await userEvent.type(
    within(rule).getByLabelText('Match original models'),
    'gpt'
  )
  await userEvent.type(within(rule).getByLabelText('Requests per minute'), '3')
  const actions = within(rule).getByRole('button', {
    name: 'Request and response actions',
  })
  await userEvent.click(actions)
  expect(actions).toHaveAttribute('aria-expanded', 'true')
  await userEvent.type(
    within(rule).getByLabelText('Route to model'),
    'target-model'
  )
  await userEvent.click(
    within(rule).getByRole('switch', { name: 'Request body rewrite' })
  )
  const editor = within(rule).getByRole('textbox', {
    name: 'Request JSON template',
  })
  expect(editor).toHaveValue('{\n  "temperature": 0.7\n}')
  fireEvent.input(editor, { target: { value: '{"temperature":0}' } })
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/user-control/policies',
      expect.objectContaining({
        limits: {},
        schedule: expect.objectContaining({
          timezone: 'UTC',
          rules: [
            expect.objectContaining({
              limits: {
                scoped_rules: [
                  expect.objectContaining({
                    models: ['gpt'],
                    limits: { rate: { rpm: 3 } },
                    actions: expect.objectContaining({
                      route_model: 'target-model',
                      request_mode: 'merge',
                      request_body: '{"temperature":0}',
                    }),
                  }),
                ],
              },
            }),
          ],
        }),
      })
    )
  )
})

function ActionsFixture() {
  const [actions, setActions] = useState<ControlActions>({})
  return <ActionsEditor value={actions} onChange={setActions} />
}

test('choosing a simulated error exposes real status and content and can be turned off', async () => {
  render(<ActionsFixture />)
  await userEvent.click(
    screen.getByRole('combobox', { name: 'Response replacement (simulation)' })
  )
  await userEvent.click(
    await screen.findByRole('option', { name: 'Simulated error response' })
  )
  expect(screen.getByLabelText('Error HTTP status')).toHaveValue(429)
  expect(screen.getByLabelText('Error HTTP status')).toHaveAttribute(
    'min',
    '400'
  )
  expect(screen.getByLabelText('Error HTTP status')).toHaveAttribute(
    'max',
    '599'
  )
  await userEvent.type(
    screen.getByLabelText('Replacement content'),
    'maintenance'
  )
  expect(screen.getByLabelText('Replacement content')).toHaveValue(
    'maintenance'
  )
  expect(
    screen.getByText(
      'Simulated responses do not call upstream or charge usage. They are marked in the response and admin audit. JSON mode requires a non-streaming request.'
    )
  ).toBeVisible()
  await userEvent.click(
    screen.getByRole('combobox', { name: 'Response replacement (simulation)' })
  )
  await userEvent.click(await screen.findByRole('option', { name: 'Disabled' }))
  expect(screen.queryByLabelText('Replacement content')).not.toBeInTheDocument()
})

test('settlement audit shows actual usage and incomplete delivery separately from request success', () => {
  render(
    <RuntimeAuditDetails
      item={{
        action: 'user_control.runtime.delivery.settled',
        other: {
          audit_info: {
            params: {
              actual_model: 'real-model',
              quota: '57',
              prompt_tokens: '11',
              completion_tokens: '100',
              delivery_complete: 'false',
            },
          },
        },
      }}
    />
  )
  expect(screen.getByText('real-model')).toBeVisible()
  expect(screen.getByText('57')).toBeVisible()
  expect(screen.getByText('11')).toBeVisible()
  expect(screen.getByText('100')).toBeVisible()
  expect(screen.getByText('No')).toBeVisible()
})

test('summaries show scoped filters and admin audit translates action names without raw request content', () => {
  render(
    <>
      <LimitsSummary
        value={{
          scoped_rules: [
            {
              id: 'scope',
              name: 'Model quota',
              models: ['client-model'],
              channel_ids: [7],
              limits: { rate: { rpm: 3 } },
            },
          ],
        }}
      />
      <RuntimeAuditDetails
        item={{
          action: 'user_control.runtime.request',
          other: {
            audit_info: {
              params: {
                original_model: 'client-model',
                actual_model: 'target-model',
                simulated: 'true',
                actions: '["route_model","response"]',
                rules: '["scope"]',
              },
            },
          },
        }}
      />
    </>
  )
  expect(screen.getByText(/Model quota/)).toBeVisible()
  expect(screen.queryByText('[object Object]')).not.toBeInTheDocument()
  expect(screen.getByText('target-model')).toBeVisible()
  expect(
    screen.getByText('Route to model, Response replacement (simulation)')
  ).toBeVisible()
  expect(screen.getByText('Yes')).toBeVisible()
})
