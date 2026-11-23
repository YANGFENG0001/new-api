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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import type { ReactNode } from 'react'
import { afterEach, expect, test, vi } from 'vitest'

import zh from '@/i18n/locales/zh.json'
import { api } from '@/lib/api'

import type { Capabilities, Pool } from '../api'
import {
  emptySyncSource,
  syncBindingSchema,
  type SyncMapping,
} from '../sync-api'
import { SyncAuthenticationActions } from '../sync-authentication'
import { PoolSyncBinding } from '../sync-binding'
import { SyncObservations } from '../sync-observations'
import { SyncSourceEditor } from '../sync-source-editor'
import { SyncSourcesPanel } from '../sync-sources-panel'

const mapping: SyncMapping = {
  success_path: 'ok',
  accounts_path: 'data.accounts',
  id_path: 'account_id',
  name_path: 'vm_id',
  sampled_at_path: 'last_probe',
  source_path: 'probe_source',
  expiry_path: '',
  time_format: 'rfc3339',
  cycles: [
    {
      key: 'short',
      name: 'Short',
      seconds: 3600,
      utilization_path: 'utilization',
      scale: 'ratio',
      reset_path: 'reset',
      sampled_at_path: '',
    },
  ],
}
const source = {
  ...emptySyncSource(mapping),
  id: 4,
  name: 'Panel',
  url: 'https://example.com/api/panel/usage',
  version: 1,
  has_credentials: true,
}
const caps: Capabilities = {
  read: true,
  manage: true,
  assign: true,
  calibrate: true,
  shared_reset: true,
  personal_reset: true,
  batch_migrate: true,
  audit_read: true,
  sync_read: true,
  sync_credentials: true,
  sync_run: true,
  sync_binding: true,
  sync_apply: true,
}
const clients: QueryClient[] = []
function mount(children: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  return render(
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
}
const envelope = (data: unknown) => ({ data: { success: true, data } })
function mockAuthenticationVerification() {
  vi.spyOn(api, 'get').mockResolvedValue(
    envelope({
      scope: 'subscription.sync.credentials',
      methods: [{ method: '2fa', available: true }],
      oauth_providers: [],
      password_encryption_enabled: false,
    })
  )
  vi.spyOn(api, 'post').mockResolvedValue(
    envelope({
      proof_token: 'auth-proof',
      scope: 'subscription.sync.credentials',
      method: '2fa',
      expires_at: Math.floor(Date.now() / 1000) + 60,
    })
  )
}
async function completeAuthenticationVerification() {
  await userEvent.type(
    await screen.findByLabelText('Authenticator code or backup code'),
    '123456'
  )
  await userEvent.click(screen.getByRole('button', { name: 'Verify' }))
}
afterEach(() => clients.splice(0).forEach((c) => c.clear()))

test('saving an existing source preserves write-only credentials unless replacement is explicitly selected', async () => {
  mockAuthenticationVerification()
  const request = vi.spyOn(api, 'request').mockResolvedValue(envelope(source))
  mount(
    <SyncSourceEditor
      source={source}
      template={mapping}
      privileged
      canEdit
      onClose={() => undefined}
    />
  )
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  expect(request).not.toHaveBeenCalled()
  await completeAuthenticationVerification()
  await waitFor(() => expect(request).toHaveBeenCalledTimes(1))
  const payload = request.mock.calls[0][0].data as Record<string, unknown>
  expect(payload.request).toBeUndefined()
  expect(payload.token).toBeUndefined()
  expect(payload.password).toBeUndefined()
  expect(payload.mapping).toMatchObject({ accounts_path: 'data.accounts' })
})

test('test read requires a separate confirmation before applying synchronization', async () => {
  const request = vi
    .spyOn(api, 'request')
    .mockImplementation(async (config) => {
      if (config.url?.endsWith('/sync-sources')) {
        return envelope({
          sources: [source],
          template: mapping,
          privileged: true,
        })
      }
      if (config.url?.endsWith('/state')) {
        return envelope({ accounts: [], events: [] })
      }
      return envelope({
        preview: (config.data as { preview: boolean }).preview,
        accounts: [],
        changes: [],
      })
    })
  mount(<SyncSourcesPanel caps={caps} />)
  await userEvent.click(await screen.findByRole('button', { name: 'Panel' }))
  expect(screen.getByRole('button', { name: 'Synchronize now' })).toBeDisabled()
  await userEvent.click(
    screen.getByRole('button', { name: 'Test read and preview' })
  )
  await waitFor(() =>
    expect(
      screen.getByRole('button', { name: 'Synchronize now' })
    ).toBeEnabled()
  )
  expect(
    request.mock.calls
      .filter(([c]) => c.url?.endsWith('/run'))
      .map(([c]) => c.data)
  ).toEqual([{ preview: true }])
  await userEvent.click(screen.getByRole('button', { name: 'Synchronize now' }))
  const dialog = await screen.findByRole('alertdialog')
  expect(
    request.mock.calls.filter(([c]) => c.url?.endsWith('/run'))
  ).toHaveLength(1)
  await userEvent.click(
    within(dialog).getByRole('button', { name: 'Continue' })
  )
  await waitFor(() =>
    expect(
      request.mock.calls.filter(([c]) => c.url?.endsWith('/run'))
    ).toHaveLength(2)
  )
  expect(
    request.mock.calls.filter(([c]) => c.url?.endsWith('/run'))[1][0].data
  ).toEqual({ preview: false })
})

test('a later scheduled snapshot replaces a manual preview without reverting it to an older saved snapshot', async () => {
  const now = Math.floor(Date.now() / 1000)
  let lastFetch = now - 60
  let utilization = 0.5
  const account = (ratio: number) => ({
    id: 'account-1',
    name: 'Account',
    expiry: 0,
    cycles: [
      {
        key: 'short',
        name: 'Short',
        seconds: 3600,
        utilization: ratio,
        sampled_at: now,
        window_start: now - 100,
        window_end: now + 3500,
        error: '',
      },
    ],
  })
  vi.spyOn(api, 'request').mockImplementation(async (config) => {
    if (config.url?.endsWith('/sync-sources')) {
      return envelope({
        sources: [{ ...source, last_fetch: lastFetch }],
        template: mapping,
        privileged: true,
      })
    }
    if (config.url?.endsWith('/state')) {
      return envelope({ accounts: [account(utilization)], events: [] })
    }
    return envelope({ preview: true, accounts: [account(0.6)], changes: [] })
  })
  mount(<SyncSourcesPanel caps={caps} />)
  await userEvent.click(await screen.findByRole('button', { name: 'Panel' }))
  expect(await screen.findByText('50%')).toBeVisible()
  await userEvent.click(
    screen.getByRole('button', { name: 'Test read and preview' })
  )
  expect(await screen.findByText('60%')).toBeVisible()
  await act(async () => {
    await clients[0].invalidateQueries({ queryKey: ['subscription-sync'] })
  })
  expect(screen.getByText('60%')).toBeVisible()
  utilization = 0.7
  lastFetch = Math.ceil(Date.now() / 1000) + 1
  await act(async () => {
    await clients[0].invalidateQueries({ queryKey: ['subscription-sync'] })
  })
  expect(await screen.findByText('70%')).toBeVisible()
  expect(screen.queryByText('60%')).not.toBeInTheDocument()
})

test('pool binding selects a discovered stable account and saves only after confirmation', async () => {
  const pool = {
    id: 8,
    name: 'Pool',
    version: 1,
    cycles: [{ key: 'local', name: 'Personal', seconds: 3600 }],
  } as Pool
  const request = vi
    .spyOn(api, 'request')
    .mockImplementation(async (config) => {
      if (config.method === 'put') return envelope(null)
      if (config.url?.endsWith('/sync-sources')) {
        return envelope({
          sources: [source],
          template: mapping,
          privileged: true,
        })
      }
      if (config.url?.endsWith('/state')) {
        return envelope({
          accounts: [
            {
              id: 'stable-id',
              name: 'vm-01',
              expiry: 0,
              cycles: [
                {
                  key: 'short',
                  name: 'Short',
                  seconds: 3600,
                  utilization: 0.2,
                  sampled_at: Math.floor(Date.now() / 1000),
                  window_start: 1,
                  window_end: 253402300799,
                  error: '',
                },
              ],
            },
          ],
          events: [],
        })
      }
      return envelope({
        binding: {
          pool_id: 8,
          source_id: 4,
          account_id: '',
          enabled: false,
          sync_expiry: false,
          stale_policy: 'stop',
          grace_seconds: 0,
          version: 0,
          cycles: [],
        },
        observations: [],
      })
    })
  mount(<PoolSyncBinding pool={pool} caps={caps} />)
  const account = await screen.findByRole('combobox', {
    name: 'Upstream account',
  })
  await userEvent.click(account)
  await userEvent.click(
    await screen.findByRole('option', { name: 'vm-01 · stable-id' })
  )
  await userEvent.click(
    screen.getByRole('combobox', { name: 'Upstream cycle' })
  )
  await userEvent.click(await screen.findByRole('option', { name: 'Short' }))
  expect(
    screen.queryByRole('spinbutton', {
      name: 'Quota units per full upstream window (optional)',
    })
  ).not.toBeInTheDocument()
  await userEvent.click(
    screen.getByRole('button', { name: 'Advanced synchronization settings' })
  )
  expect(
    await screen.findByRole('spinbutton', {
      name: 'Quota units per full upstream window (optional)',
    })
  ).toBeVisible()
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  const dialog = await screen.findByRole('alertdialog')
  expect(request.mock.calls.some(([c]) => c.method === 'put')).toBe(false)
  await userEvent.click(
    within(dialog).getByRole('button', { name: 'Continue' })
  )
  await waitFor(() =>
    expect(request.mock.calls.some(([c]) => c.method === 'put')).toBe(true)
  )
  const payload = request.mock.calls.find(([c]) => c.method === 'put')?.[0]
    .data as Record<string, unknown>
  expect(payload.account_id).toBe('stable-id')
  expect(payload.cycles).toEqual([
    expect.objectContaining({ local_key: 'local', upstream_key: 'short' }),
  ])
  expect(payload.sync_expiry).toBe(false)
})

test('stale upstream observations stay visibly stale even after a recent fetch', () => {
  const now = Math.floor(Date.now() / 1000)
  mount(
    <SyncObservations
      observations={[
        {
          key: 'x',
          name: 'Short',
          utilization: 0.62,
          sampled_at: now - 600,
          fetched_at: now,
          window_end: now + 3600,
          stale: true,
          pending: '',
        },
      ]}
    />
  )
  expect(screen.getByText('62%')).toBeInTheDocument()
  expect(screen.getByRole('status')).toHaveTextContent(
    'Upstream observation is stale'
  )
  expect(screen.getByRole('progressbar', { name: 'Short' })).toHaveAttribute(
    'aria-valuenow',
    '62'
  )
})

test('binding validation rejects a missing account, negative grace and invalid utilization threshold', () => {
  const base = {
    pool_id: 8,
    source_id: 4,
    account_id: 'stable',
    enabled: true,
    sync_expiry: false,
    stale_policy: 'stop',
    grace_seconds: 0,
    version: 0,
    cycles: [],
  }
  expect(syncBindingSchema.safeParse(base).success).toBe(true)
  expect(syncBindingSchema.safeParse({ ...base, account_id: '' }).success).toBe(
    false
  )
  expect(
    syncBindingSchema.safeParse({ ...base, grace_seconds: -1 }).success
  ).toBe(false)
  expect(
    syncBindingSchema.safeParse({
      ...base,
      cycles: [
        {
          local_key: 'a',
          upstream_key: 'b',
          align_reset: false,
          enforce: true,
          threshold: 1.01,
          max_shift_seconds: 300,
        },
      ],
    }).success
  ).toBe(false)
})

test('Chinese observations translate status text and retain the original utilization', async () => {
  i18next.addResourceBundle('zh', 'translation', zh.translation, true, true)
  await i18next.changeLanguage('zh')
  try {
    const now = Math.floor(Date.now() / 1000)
    mount(
      <SyncObservations
        observations={[
          {
            key: 'x',
            name: '自定义周期',
            utilization: 0.62,
            sampled_at: now - 600,
            fetched_at: now,
            window_end: now + 3600,
            stale: true,
            pending: '',
          },
        ]}
      />
    )
    expect(screen.getByText('62%')).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('上游观测数据已过期')
    expect(
      screen.queryByText('Upstream observation is stale')
    ).not.toBeInTheDocument()
  } finally {
    await i18next.changeLanguage('en')
  }
})

test('read-only source access disables saving and keeps secrets hidden', () => {
  mount(
    <SyncSourceEditor
      source={source}
      template={mapping}
      privileged={false}
      canEdit={false}
      onClose={() => undefined}
    />
  )
  expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
  expect(screen.getByRole('textbox', { name: 'API URL' })).toBeDisabled()
  expect(screen.queryByLabelText('API key')).not.toBeInTheDocument()
})

test('removing and adding mapping cycles keeps unique keys', async () => {
  mount(
    <SyncSourceEditor
      source={source}
      template={mapping}
      privileged
      canEdit
      onClose={() => undefined}
    />
  )
  await userEvent.click(screen.getByRole('button', { name: 'Add cycle' }))
  await userEvent.click(screen.getByRole('button', { name: 'Add cycle' }))
  await userEvent.click(
    within(screen.getAllByRole('group', { name: 'Cycles' })[0]).getByRole(
      'button',
      { name: 'Remove' }
    )
  )
  await userEvent.click(screen.getByRole('button', { name: 'Add cycle' }))
  const inputs = screen.getAllByRole<HTMLInputElement>('textbox', {
    name: 'Cycle key',
  })
  expect(new Set(inputs.map((input) => input.value)).size).toBe(inputs.length)
})

test('editing a mapping key keeps focus throughout keyboard input', async () => {
  mount(
    <SyncSourceEditor
      source={source}
      template={mapping}
      privileged
      canEdit
      onClose={() => undefined}
    />
  )
  const inputs = screen.getAllByRole<HTMLInputElement>('textbox', {
    name: 'Cycle key',
  })
  await userEvent.clear(inputs[0])
  await userEvent.type(inputs[0], 'custom-key')
  expect(
    screen.getAllByRole<HTMLInputElement>('textbox', { name: 'Cycle key' })[0]
  ).toHaveValue('custom-key')
  expect(inputs[0]).toHaveFocus()
})

test('opening a history event displays its recorded utilization changes', async () => {
  vi.spyOn(api, 'request').mockImplementation(async (config) => {
    if (config.url?.endsWith('/sync-sources')) {
      return envelope({
        sources: [source],
        template: mapping,
        privileged: true,
      })
    }
    return envelope({
      accounts: [],
      events: [
        {
          id: 11,
          pool_id: 8,
          actor_id: 1,
          action: 'pool_sync',
          message: 'Upstream synchronization evaluated',
          created_at: 1795344000,
          changes: [
            {
              pool_id: 8,
              key: 'short',
              before: 0,
              after: 0,
              previous_utilization: 0.1,
              utilization: 0.62,
              message: 'Upstream observation accepted',
            },
          ],
        },
      ],
    })
  })
  mount(<SyncSourcesPanel caps={caps} />)
  await userEvent.click(await screen.findByRole('button', { name: 'Panel' }))
  await userEvent.click(
    screen.getByText('Synchronization history', { selector: 'summary' })
  )
  await userEvent.click(await screen.findByRole('button', { name: 'View' }))
  expect(screen.getByText('Upstream observation accepted')).toBeInTheDocument()
  expect(screen.getByText(/10%.*62%/)).toBeInTheDocument()
})

test('token replacement waits for confirmation and step-up, sends a fenced request and never displays the saved token', async () => {
  mockAuthenticationVerification()
  const request = vi.spyOn(api, 'request').mockResolvedValue(
    envelope({
      ...source,
      credential_revision: 2,
      last_auth_success: 1795344000,
    })
  )
  const changed = vi.fn()
  mount(
    <SyncAuthenticationActions
      source={source}
      disabled={false}
      onChanged={changed}
    />
  )
  expect(screen.getByText('Managed upstream')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Renew token now' })).toBeDisabled()
  await userEvent.click(screen.getByRole('button', { name: 'Replace token' }))
  const dialog = await screen.findByRole('alertdialog')
  expect(
    within(dialog).getByRole('button', { name: 'Continue' })
  ).toBeDisabled()
  await userEvent.type(
    within(dialog).getByLabelText('New token'),
    'replacement-secret'
  )
  expect(within(dialog).getByLabelText('New token')).toHaveAttribute(
    'type',
    'password'
  )
  await userEvent.click(
    within(dialog).getByRole('button', { name: 'Continue' })
  )
  expect(request).not.toHaveBeenCalled()
  await completeAuthenticationVerification()
  await waitFor(() => expect(changed).toHaveBeenCalledTimes(1))
  expect(request.mock.calls[0][0]).toMatchObject({
    url: '/api/subscription/admin/sync-sources/4/auth',
    data: {
      action: 'replace',
      token: 'replacement-secret',
      version: 1,
      credential_revision: 0,
      confirm: true,
    },
    headers: { 'X-Security-Proof': 'auth-proof' },
  })
  expect(screen.queryByLabelText('New token')).not.toBeInTheDocument()
  expect(screen.queryByText('replacement-secret')).not.toBeInTheDocument()
})

test('cancelled token verification performs no upstream authentication operation', async () => {
  mockAuthenticationVerification()
  const request = vi.spyOn(api, 'request').mockResolvedValue(envelope(source))
  mount(
    <SyncAuthenticationActions
      source={source}
      disabled={false}
      onChanged={() => undefined}
    />
  )
  await userEvent.click(
    screen.getByRole('button', { name: 'Check authentication' })
  )
  await userEvent.click(
    within(await screen.findByRole('alertdialog')).getByRole('button', {
      name: 'Continue',
    })
  )
  await screen.findByLabelText('Authenticator code or backup code')
  await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  await waitFor(() =>
    expect(
      screen.queryByLabelText('Authenticator code or backup code')
    ).not.toBeInTheDocument()
  )
  expect(request).not.toHaveBeenCalled()
})

test('pending edits disable authentication actions and consumed refresh tokens disable renewal', () => {
  mount(
    <SyncAuthenticationActions
      source={{ ...source, refresh_mode: 'oauth', refresh_blocked: true }}
      disabled
      onChanged={() => undefined}
    />
  )
  for (const name of [
    'Check authentication',
    'Renew token now',
    'Replace token',
  ]) {
    expect(screen.getByRole('button', { name })).toBeDisabled()
  }
  expect(screen.getByRole('alert')).toHaveTextContent(
    'Refresh token may have been consumed'
  )
})

test('selecting account renewal exposes login credentials and defaults automatic renewal to off', async () => {
  mount(
    <SyncSourceEditor
      source={source}
      template={mapping}
      privileged
      canEdit
      onClose={() => undefined}
    />
  )
  await userEvent.click(
    screen.getByRole('combobox', { name: 'Token renewal mode' })
  )
  await userEvent.click(
    await screen.findByRole('option', { name: 'Account login renewal' })
  )
  expect(screen.getByLabelText('Password')).toHaveAttribute('type', 'password')
  expect(
    screen.getByRole('switch', { name: 'Automatic token renewal' })
  ).not.toBeChecked()
  expect(screen.getByLabelText('Renew before expiry (seconds)')).toHaveValue(
    86400
  )
  expect(
    screen.getByRole('button', { name: 'Check authentication' })
  ).toBeDisabled()
})

test('failed token validation displays the error without reporting success or changing the saved source', async () => {
  mockAuthenticationVerification()
  vi.spyOn(api, 'request').mockResolvedValue({
    data: { success: false, message: 'Synchronization authentication failed' },
  })
  const changed = vi.fn()
  mount(
    <SyncAuthenticationActions
      source={source}
      disabled={false}
      onChanged={changed}
    />
  )
  await userEvent.click(screen.getByRole('button', { name: 'Replace token' }))
  const dialog = await screen.findByRole('alertdialog')
  await userEvent.type(
    within(dialog).getByLabelText('New token'),
    'invalid-token'
  )
  await userEvent.click(
    within(dialog).getByRole('button', { name: 'Continue' })
  )
  await completeAuthenticationVerification()
  expect(
    await screen.findByText('Synchronization authentication failed')
  ).toBeInTheDocument()
  expect(changed).not.toHaveBeenCalled()
  expect(screen.getByText('Managed upstream')).toBeInTheDocument()
})

test('source deletion requires confirmation and bound verification before removing the source', async () => {
  mockAuthenticationVerification()
  let removed = false
  const request = vi
    .spyOn(api, 'request')
    .mockImplementation(async (config) => {
      if (config.method === 'delete') {
        removed = true
        return envelope(null)
      }
      return envelope({
        sources: removed ? [] : [source],
        template: mapping,
        privileged: true,
      })
    })
  mount(<SyncSourcesPanel caps={caps} />)
  await userEvent.click(
    await screen.findByRole('button', { name: 'Delete Panel' })
  )
  expect(request.mock.calls.some(([c]) => c.method === 'delete')).toBe(false)
  const dialog = await screen.findByRole('alertdialog')
  await userEvent.click(within(dialog).getByRole('button', { name: 'Delete' }))
  expect(request.mock.calls.some(([c]) => c.method === 'delete')).toBe(false)
  await completeAuthenticationVerification()
  await waitFor(() =>
    expect(request.mock.calls.some(([c]) => c.method === 'delete')).toBe(true)
  )
  expect(
    request.mock.calls.find(([c]) => c.method === 'delete')?.[0]
  ).toMatchObject({
    data: { version: 1, credential_revision: 0, confirm: true },
  })
  await waitFor(() =>
    expect(
      screen.queryByRole('button', { name: 'Delete Panel' })
    ).not.toBeInTheDocument()
  )
})
