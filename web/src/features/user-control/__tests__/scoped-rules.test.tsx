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

test('a scoped rule offers selectable model filters and no recursive rules', async () => {
  render(<RulesFixture />)
  await userEvent.click(screen.getByRole('button', { name: 'Add scoped rule' }))
  const rule = screen.getByRole('region', { name: 'Scoped rule 1' })
  await userEvent.type(
    within(rule).getByLabelText('Match original models'),
    'gpt{Enter}'
  )
  expect(within(rule).getByText('gpt')).toBeVisible()
  expect(
    within(rule).queryByRole('button', { name: 'Add scoped rule' })
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

test('a behavior policy saves scoped actions without embedding scheduling', async () => {
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
  await userEvent.type(screen.getByLabelText('Name'), 'Scoped behavior')
  await userEvent.click(screen.getByRole('button', { name: 'Add scoped rule' }))
  const rule = screen.getByRole('region', { name: 'Scoped rule 1' })
  await userEvent.type(
    within(rule).getByLabelText('Match original models'),
    'gpt{Enter}'
  )
  await userEvent.type(within(rule).getByLabelText('Requests per minute'), '3')
  await userEvent.click(
    within(rule).getByRole('button', { name: 'Request and response actions' })
  )
  await userEvent.type(
    within(rule).getByLabelText('Route to model'),
    'target-model{Enter}'
  )
  await userEvent.click(
    within(rule).getByRole('switch', { name: 'Request body rewrite' })
  )
  fireEvent.input(
    within(rule).getByRole('textbox', { name: 'Request JSON template' }),
    { target: { value: '{"temperature":0}' } }
  )
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/user-control/policies',
      expect.objectContaining({
        schedule: null,
        limits: expect.objectContaining({
          scoped_rules: [
            expect.objectContaining({
              models: ['gpt'],
              limits: { rate: { rpm: 3 } },
              actions: expect.objectContaining({
                route_model: 'target-model',
                request_body: '{"temperature":0}',
              }),
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
