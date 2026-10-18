import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { getCoreRowModel, useReactTable } from '@tanstack/react-table'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { toast } from 'sonner'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { TooltipProvider } from '@/components/ui/tooltip'
import { api } from '@/lib/api'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import type { BatchAction } from '../../batch/types'
import type { User } from '../../types'
import { BatchActionDialog } from '../batch-action-dialog'
import { DataTableBulkActions } from '../data-table-bulk-actions'
import { UsersProvider } from '../users-provider'

const users: User[] = [
  {
    id: 2,
    username: 'member',
    display_name: '',
    role: 1,
    status: 1,
    quota: 100,
    used_quota: 0,
    request_count: 0,
    group: 'default',
  },
]

function SelectedUsers() {
  const table = useReactTable({
    data: users,
    columns: [{ accessorKey: 'username' }],
    getCoreRowModel: getCoreRowModel(),
    initialState: { rowSelection: { '0': true } },
  })
  return <DataTableBulkActions table={table} />
}

function renderBulkActions() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <UsersProvider>
          <SelectedUsers />
        </UsersProvider>
      </TooltipProvider>
    </QueryClientProvider>
  )
}

beforeEach(() =>
  Object.defineProperty(Element.prototype, 'getAnimations', {
    configurable: true,
    value: () => [],
  })
)
afterEach(() => {
  Reflect.deleteProperty(Element.prototype, 'getAnimations')
  vi.restoreAllMocks()
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
})

it('opening the real bulk menu and selecting disable renders its preview dialog without crashing', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        can_read: true,
        max_users: 5000,
        actions: [
          {
            action: 'disable',
            permission: 'user_batch.execute',
            allowed: true,
            destructive: false,
            quota: false,
          },
          {
            action: 'add_quota',
            permission: 'user_batch.quota',
            allowed: true,
            destructive: false,
            quota: true,
          },
        ],
      },
    },
  })
  renderBulkActions()
  const trigger = await screen.findByRole('button', { name: 'Bulk actions' })
  await userEvent.click(trigger)
  expect(trigger).toHaveAttribute('aria-expanded', 'true')
  expect(
    screen.getByRole('group', { name: 'Account state' })
  ).toBeInTheDocument()
  await userEvent.click(screen.getByRole('menuitem', { name: 'disable' }))
  expect(
    await screen.findByRole('dialog', { name: 'Bulk disable' })
  ).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Preview' })).toBeEnabled()
  expect(
    screen.getByRole('button', { name: 'Verify and execute' })
  ).toBeDisabled()
})

it('keyboard opening and selecting a quota action keeps empty quota input disabled', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        can_read: true,
        max_users: 5000,
        actions: [
          {
            action: 'add_quota',
            permission: 'user_batch.quota',
            allowed: true,
            destructive: false,
            quota: true,
          },
        ],
      },
    },
  })
  renderBulkActions()
  const trigger = await screen.findByRole('button', { name: 'Bulk actions' })
  trigger.focus()
  await userEvent.keyboard('{ArrowDown}')
  await screen.findByRole('menuitem', { name: 'add_quota' })
  await userEvent.keyboard('{Enter}')
  await screen.findByRole('dialog', { name: 'Bulk add_quota' })
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Preview' })).toBeDisabled()
  )
})

const preview = {
  action: 'disable',
  batch_hash: '0123456789abcdef0123456789abcdef',
  total: 1,
  eligible: 1,
  skipped: 0,
  items: [
    { user_id: 2, username: 'member', eligible: true, before: {}, after: {} },
  ],
}

function renderBatchDialog(action: BatchAction = 'disable') {
  const props = {
    action: {
      action,
      permission: 'user_batch.execute',
      allowed: true,
      destructive: false,
      quota: action.includes('quota'),
    },
    userIds: [2],
    onClose: vi.fn(),
    onCompleted: vi.fn(),
  }
  return render(
    <UsersProvider>
      <BatchActionDialog {...props} />
    </UsersProvider>
  )
}

it.each(['completed', 'running'])(
  'executing a preview yields to verification, sends single-use proof, and reports the %s job state',
  async (status) => {
    const successToast = vi.spyOn(toast, 'success')
    vi.spyOn(api, 'get').mockResolvedValue({
      data: {
        success: true,
        data: {
          scope: 'admin.user.batch',
          methods: [{ method: '2fa', available: true }],
          oauth_providers: [],
          password_encryption_enabled: false,
        },
      },
    })
    const post = vi.spyOn(api, 'post').mockImplementation(async (url) => {
      if (url.endsWith('/preview')) {
        return { data: { success: true, data: preview } }
      }
      if (url === '/api/verify') {
        return {
          data: {
            success: true,
            data: {
              proof_token: 'batch-proof',
              scope: 'admin.user.batch',
              method: '2fa',
              expires_at: Math.floor(Date.now() / 1000) + 60,
            },
          },
        }
      }
      return {
        data: {
          success: true,
          data: {
            job: { status, succeeded: 1, failed: 0, skipped: 0 },
            items: [],
          },
        },
      }
    })
    renderBatchDialog()
    await userEvent.click(screen.getByRole('button', { name: 'Preview' }))
    await userEvent.click(
      await screen.findByRole('button', { name: 'Verify and execute' })
    )
    const code = await screen.findByLabelText(
      'Authenticator code or backup code'
    )
    expect(
      screen.queryByRole('dialog', { name: 'Bulk disable' })
    ).not.toBeInTheDocument()
    await userEvent.type(code, '123456')
    await userEvent.click(screen.getByRole('button', { name: 'Verify' }))
    await waitFor(() =>
      expect(post).toHaveBeenCalledWith(
        '/api/user/batch/execute',
        expect.objectContaining({
          action: 'disable',
          user_ids: [2],
          batch_hash: preview.batch_hash,
          idempotency_key: expect.any(String),
        }),
        expect.objectContaining({
          headers: { 'X-Security-Proof': 'batch-proof' },
          singleUseAuthorization: true,
        })
      )
    )
    expect(post).toHaveBeenCalledWith(
      '/api/verify',
      expect.objectContaining({
        scope: 'admin.user.batch',
        context: {
          action: 'disable',
          user_ids: [2],
          batch_hash: preview.batch_hash,
        },
      }),
      expect.anything()
    )
    expect(successToast).toHaveBeenCalledWith(
      status === 'running'
        ? 'Bulk action started'
        : 'Bulk action finished: 1 succeeded, 0 failed, 0 skipped'
    )
  }
)

it('changing quota after preview disables execution until another preview succeeds', async () => {
  vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, data: { ...preview, action: 'add_quota' } },
  })
  renderBatchDialog('add_quota')
  const input = screen.getByRole('spinbutton')
  await userEvent.type(input, '1')
  await userEvent.click(screen.getByRole('button', { name: 'Preview' }))
  await waitFor(() =>
    expect(
      screen.getByRole('button', { name: 'Verify and execute' })
    ).toBeEnabled()
  )
  await userEvent.clear(input)
  await userEvent.type(input, '2')
  expect(
    screen.getByRole('button', { name: 'Verify and execute' })
  ).toBeDisabled()
})

it('quota input that rounds to zero cannot preview an addition', async () => {
  renderBatchDialog('add_quota')
  await userEvent.type(screen.getByRole('spinbutton'), '0.00000001')
  expect(screen.getByRole('button', { name: 'Preview' })).toBeDisabled()
})
