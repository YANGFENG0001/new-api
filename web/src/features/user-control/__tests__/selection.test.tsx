import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { useState } from 'react'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { UserControlSection } from '@/features/system-settings/security/user-control-section'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { MembersDialog } from '../members-dialog'
import { Simulator } from '../simulator'
import { UserPicker } from '../user-picker'

const clients: QueryClient[] = []
const list = {
  id: 4,
  name: 'Trial',
  description: '',
  policy_id: 0,
  enabled: true,
  members: 21,
  created_by: 1,
  updated_by: 1,
  created_at: 0,
  updated_at: 0,
}
const alice = {
  id: 7,
  username: 'alice',
  display_name: 'Alice',
  email: 'alice@example.com',
  group: 'default',
}
const bob = { id: 8, username: 'bob', display_name: 'Bob', group: 'default' }
const carol = {
  id: 9,
  username: 'carol',
  display_name: 'Carol',
  group: 'default',
}
const envelope = (data: unknown) => ({ data: { success: true, data } })

function mount(children: React.ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  return render(
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
}

function Picker(props: { multiple?: boolean; excludedIds?: number[] }) {
  const [selected, setSelected] = useState<number[]>([])
  return <UserPicker {...props} selected={selected} onChange={setSelected} />
}

beforeEach(() => {
  useAuthStore.getState().auth.setUser({ id: 1, username: 'root', role: 100 })
})

afterEach(() => {
  clients.splice(0).forEach((client) => client.clear())
  useAuthStore.getState().auth.reset()
})

test('searching from page two resets pagination and retains selections across pages and search', async () => {
  const get = vi.spyOn(api, 'get').mockImplementation(async (url, config) => {
    if (url.startsWith('/api/user/search')) {
      return envelope({ items: [carol], total: 1 })
    }
    return envelope({
      items: config?.params.p === 2 ? [bob] : [alice],
      total: 11,
    })
  })
  mount(<Picker multiple />)
  await userEvent.click(
    await screen.findByRole('checkbox', { name: 'Select user alice (#7)' })
  )
  await userEvent.click(screen.getByRole('button', { name: 'Go to next page' }))
  await userEvent.click(
    await screen.findByRole('checkbox', { name: 'Select user bob (#8)' })
  )
  await userEvent.type(
    screen.getByRole('textbox', { name: 'Search users' }),
    'carol{Enter}'
  )
  await userEvent.click(
    await screen.findByRole('checkbox', { name: 'Select user carol (#9)' })
  )
  expect(screen.getByText('3 users selected')).toBeVisible()
  expect(screen.getByText('1 / 1')).toBeVisible()
  expect(
    get.mock.calls.some(
      ([url]) => url.includes('keyword=carol') && url.includes('p=1')
    )
  ).toBe(true)
  await userEvent.clear(screen.getByRole('textbox', { name: 'Search users' }))
  await userEvent.click(screen.getByRole('button', { name: /^Search$/ }))
  expect(
    await screen.findByRole('checkbox', { name: 'Select user alice (#7)' })
  ).toBeChecked()
  await userEvent.click(screen.getByRole('button', { name: 'Clear selection' }))
  expect(screen.getByText('0 users selected')).toBeVisible()
})

test('single selection replaces the previous user and existing members cannot be toggled', async () => {
  vi.spyOn(api, 'get').mockResolvedValue(
    envelope({ items: [alice, bob, carol], total: 3 })
  )
  mount(<Picker excludedIds={[9]} />)
  await userEvent.click(
    await screen.findByRole('checkbox', { name: 'Select user alice (#7)' })
  )
  await userEvent.click(
    screen.getByRole('checkbox', { name: 'Select user bob (#8)' })
  )
  expect(
    screen.getByRole('checkbox', { name: 'Select user alice (#7)' })
  ).not.toBeChecked()
  expect(
    screen.getByRole('checkbox', { name: 'Select user bob (#8)' })
  ).toBeChecked()
  const existing = screen.getByRole('checkbox', {
    name: 'Select user carol (#9)',
  })
  expect(existing).toHaveAttribute('aria-disabled', 'true')
  await userEvent.click(existing)
  expect(screen.getByText('1 users selected')).toBeVisible()
})

test('without user read permission the picker explains the missing permission and makes no request', () => {
  useAuthStore.getState().auth.setUser({ id: 2, username: 'limited', role: 10 })
  const get = vi.spyOn(api, 'get')
  mount(<Picker />)
  expect(screen.getByText('User selection unavailable')).toBeVisible()
  expect(get).not.toHaveBeenCalled()
})

test('a failed user request offers a retry and an empty retry shows no matching users', async () => {
  vi.spyOn(api, 'get')
    .mockRejectedValueOnce(new Error('offline'))
    .mockResolvedValue(envelope({ items: [], total: 0 }))
  mount(<Picker />)
  expect(await screen.findByText('Failed to load users')).toBeVisible()
  await userEvent.click(screen.getByRole('button', { name: /retry/i }))
  expect(await screen.findByText('No matching users')).toBeVisible()
})

test('partial member addition reports skipped and existing users and retries only the skipped selection', async () => {
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url.includes('/members')) return envelope({ items: [], total: 0 })
    return envelope({ items: [alice, bob, carol], total: 3 })
  })
  const post = vi.spyOn(api, 'post').mockResolvedValue(
    envelope({
      inserted: 1,
      requested: 3,
      skipped: [{ user_id: 8, reason: 'Permission denied' }],
    })
  )
  mount(<MembersDialog list={list} canWrite onClose={vi.fn()} />)
  for (const name of ['alice (#7)', 'bob (#8)', 'carol (#9)']) {
    await userEvent.click(
      await screen.findByRole('checkbox', { name: `Select user ${name}` })
    )
  }
  await userEvent.click(screen.getByRole('button', { name: 'Add members' }))
  expect(
    await screen.findByText(
      '1 users added, 1 skipped, 1 already members (3 requested)'
    )
  ).toBeVisible()
  expect(screen.getByText('User #8: Permission denied')).toBeVisible()
  expect(screen.getByText('1 users selected')).toBeVisible()
  await userEvent.click(screen.getByRole('button', { name: 'Add members' }))
  expect(post).toHaveBeenLastCalledWith('/api/user-control/lists/4/members', {
    user_ids: [8],
    note: undefined,
  })
})

test('a business failure retains the selected members for retry and exposes the server error', async () => {
  vi.spyOn(api, 'get').mockImplementation(async (url) =>
    envelope(
      url.includes('/members')
        ? { items: [], total: 0 }
        : { items: [alice], total: 1 }
    )
  )
  vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: false, message: 'Write denied' },
  })
  mount(<MembersDialog list={list} canWrite onClose={vi.fn()} />)
  await userEvent.click(
    await screen.findByRole('checkbox', { name: 'Select user alice (#7)' })
  )
  await userEvent.click(screen.getByRole('button', { name: 'Add members' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('Write denied')
  expect(
    screen.getByRole('checkbox', { name: 'Select user alice (#7)' })
  ).toBeChecked()
  expect(screen.getByRole('button', { name: 'Add members' })).toBeEnabled()
})

test('removing the last member on page two returns to the last valid member page', async () => {
  let removed = false
  const get = vi.spyOn(api, 'get').mockImplementation(async (url, config) => {
    if (!url.includes('/members')) return envelope({ items: [], total: 0 })
    const page = config?.params.p
    const items =
      page === 2 && removed
        ? []
        : [
            {
              id: 20,
              user_id: page === 2 ? 8 : 7,
              username: page === 2 ? 'bob' : 'alice',
              created_at: 0,
            },
          ]
    return envelope({ items, total: removed ? 20 : 21 })
  })
  vi.spyOn(api, 'delete').mockImplementation(async () => {
    removed = true
    return envelope({ removed: 1, skipped: [] })
  })
  mount(<MembersDialog list={list} canWrite onClose={vi.fn()} />)
  const members = screen.getByRole('region', { name: 'Current members' })
  await within(members).findByRole('button', {
    name: 'Remove member alice (#7)',
  })
  await userEvent.click(
    within(members).getByRole('button', { name: 'Go to next page' })
  )
  await userEvent.click(
    await within(members).findByRole('button', {
      name: 'Remove member bob (#8)',
    })
  )
  expect(
    await within(members).findByRole('button', {
      name: 'Remove member alice (#7)',
    })
  ).toBeVisible()
  await waitFor(() => expect(within(members).getByText('1 / 1')).toBeVisible())
  expect(
    get.mock.calls.filter(
      ([url, config]) => url.includes('/members') && config?.params.p === 1
    ).length
  ).toBeGreaterThan(1)
})

test('when the user result count shrinks while on page two the picker returns to page one', async () => {
  let shrunk = false
  vi.spyOn(api, 'get').mockImplementation(async (_url, config) =>
    envelope({
      items: shrunk && config?.params.p === 2 ? [] : [alice],
      total: shrunk ? 1 : 11,
    })
  )
  mount(<Picker />)
  await screen.findByRole('checkbox', { name: 'Select user alice (#7)' })
  await userEvent.click(screen.getByRole('button', { name: 'Go to next page' }))
  await screen.findByRole('checkbox', { name: 'Select user alice (#7)' })
  shrunk = true
  await clients[0].invalidateQueries({
    queryKey: ['user-control', 'user-picker'],
  })
  await waitFor(() => expect(screen.getByText('1 / 1')).toBeVisible())
})

test('switching to Chinese translates the dynamic checkbox label and selection count', async () => {
  const i18n = createInstance()
  await i18n.init({
    lng: 'en',
    fallbackLng: 'en',
    resources: {
      zh: {
        translation: {
          'Select user {{name}} (#{{id}})': '选择用户 {{name}}（#{{id}}）',
          '{{count}} users selected': '已选择 {{count}} 位用户',
        },
      },
    },
  })
  vi.spyOn(api, 'get').mockResolvedValue(envelope({ items: [alice], total: 1 }))
  mount(
    <I18nextProvider i18n={i18n}>
      <Picker />
    </I18nextProvider>
  )
  await userEvent.click(
    await screen.findByRole('checkbox', { name: 'Select user alice (#7)' })
  )
  await i18n.changeLanguage('zh')
  expect(
    await screen.findByRole('checkbox', { name: '选择用户 alice（#7）' })
  ).toBeChecked()
  expect(screen.getByText('已选择 1 位用户')).toBeVisible()
})

test('the override entry searches and selects a user then saves the override for that user', async () => {
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url.endsWith('/capabilities')) {
      return envelope({
        read: true,
        override_write: true,
        default_timezone: 'UTC',
      })
    }
    if (url === '/api/subscription/admin/plans') return envelope([])
    if (url.startsWith('/api/user/search')) {
      return envelope({ items: [bob], total: 1 })
    }
    if (url === '/api/user/') return envelope({ items: [alice], total: 1 })
    return envelope({ items: [] })
  })
  const put = vi.spyOn(api, 'put').mockResolvedValue(envelope({}))
  mount(<UserControlSection />)
  await userEvent.click(
    await screen.findByRole('tab', { name: 'User overrides' })
  )
  await userEvent.click(
    screen.getByRole('button', { name: 'Override for user' })
  )
  expect(screen.getByRole('button', { name: 'Continue' })).toBeDisabled()
  await userEvent.type(
    screen.getByRole('textbox', { name: 'Search users' }),
    'bob{Enter}'
  )
  await userEvent.click(
    await screen.findByRole('checkbox', { name: 'Select user bob (#8)' })
  )
  await userEvent.click(screen.getByRole('button', { name: 'Continue' }))
  expect(
    await screen.findByRole('dialog', { name: 'User override #8' })
  ).toBeVisible()
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith(
      '/api/user-control/users/8/override',
      expect.objectContaining({ mode: 'merge', limits: {} })
    )
  )
})

test('saving a new list keeps the Lists tab active after the settings reload and shows the saved list', async () => {
  let finishReload!: () => void
  const reload = new Promise<void>((resolve) => {
    finishReload = resolve
  })
  let capabilityLoads = 0
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url.endsWith('/capabilities')) {
      if (++capabilityLoads === 2) await reload
      return envelope({ read: true, list_write: true, default_timezone: 'UTC' })
    }
    if (url === '/api/subscription/admin/plans') return envelope([])
    if (url.endsWith('/lists')) {
      return envelope({ items: capabilityLoads > 1 ? [list] : [] })
    }
    return envelope({ items: [] })
  })
  const post = vi.spyOn(api, 'post').mockResolvedValue(envelope(list))
  mount(<UserControlSection />)
  await userEvent.click(await screen.findByRole('tab', { name: 'Lists' }))
  await userEvent.click(screen.getByRole('button', { name: 'New list' }))
  await userEvent.type(screen.getByRole('textbox', { name: 'Name' }), list.name)
  await userEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() =>
    expect(screen.queryByRole('tab', { name: 'Lists' })).not.toBeInTheDocument()
  )
  await act(async () => finishReload())
  expect(await screen.findByRole('tab', { name: 'Lists' })).toHaveAttribute(
    'aria-selected',
    'true'
  )
  expect(screen.getByRole('tabpanel', { name: 'Lists' })).toHaveTextContent(
    list.name
  )
  expect(post).toHaveBeenCalledWith(
    '/api/user-control/lists',
    expect.objectContaining({ name: list.name })
  )
})

test.each(['Lists', 'User overrides', 'Subscription plans', 'Simulator'])(
  'refreshing settings while on %s preserves the active tab after loading',
  async (tabName) => {
    let finishReload!: () => void
    const reload = new Promise<void>((resolve) => {
      finishReload = resolve
    })
    let capabilityLoads = 0
    vi.spyOn(api, 'get').mockImplementation(async (url) => {
      if (url.endsWith('/capabilities')) {
        if (++capabilityLoads === 2) await reload
        return envelope({ read: true, default_timezone: 'UTC' })
      }
      if (url === '/api/subscription/admin/plans') return envelope([])
      return envelope({ items: [] })
    })
    mount(<UserControlSection />)
    await userEvent.click(await screen.findByRole('tab', { name: tabName }))
    await userEvent.click(screen.getByRole('button', { name: 'Refresh' }))
    await waitFor(() =>
      expect(
        screen.queryByRole('tab', { name: tabName })
      ).not.toBeInTheDocument()
    )
    await act(async () => finishReload())
    expect(await screen.findByRole('tab', { name: tabName })).toHaveAttribute(
      'aria-selected',
      'true'
    )
    expect(screen.getByRole('tabpanel', { name: tabName })).toBeVisible()
  }
)

test('simulation sends the chosen instant and hides the old result when the selected user changes', async () => {
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url.endsWith('/simulate')) {
      return envelope({
        user_id: 7,
        resolved_at: '2026-10-18T04:00:00Z',
        timezone: 'UTC',
        has_policy: true,
        sources: ['list:4'],
        limits: { blocked: false, rate: { rpm: 12 } },
      })
    }
    return envelope({ items: [alice, bob], total: 2 })
  })
  mount(<Simulator />)
  await userEvent.click(
    await screen.findByRole('checkbox', { name: 'Select user alice (#7)' })
  )
  const at = '2026-10-18T12:00'
  const input = screen.getByLabelText('Evaluate at (optional)')
  await userEvent.type(input, at)
  await userEvent.click(screen.getByRole('button', { name: 'Simulate' }))
  const result = await screen.findByRole('region', {
    name: 'Simulation result',
  })
  expect(result).toHaveTextContent('Requests per minute')
  expect(result).toHaveTextContent('Control list: 4')
  expect(get).toHaveBeenCalledWith('/api/user-control/simulate', {
    params: { user_id: 7, at: new Date(at).toISOString() },
  })
  await userEvent.click(
    screen.getByRole('checkbox', { name: 'Select user bob (#8)' })
  )
  expect(
    screen.queryByRole('region', { name: 'Simulation result' })
  ).not.toBeInTheDocument()
})
