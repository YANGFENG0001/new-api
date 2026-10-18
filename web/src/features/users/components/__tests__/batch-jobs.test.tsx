import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { BatchJobsSheet } from '../batch-jobs-sheet'
import { UsersProvider } from '../users-provider'

const job = {
  id: 'job-retry',
  action: 'disable',
  batch_hash: '0123456789abcdef0123456789abcdef',
  total: 2,
  succeeded: 1,
  failed: 1,
  skipped: 0,
  status: 'partial',
  created_by: 1,
  created_at: 1,
  started_at: 1,
  finished_at: 2,
}
const items = [
  { id: 1, job_id: job.id, user_id: 2, username: 'done', status: 'succeeded' },
  {
    id: 2,
    job_id: job.id,
    user_id: 3,
    username: 'retry',
    status: 'failed',
    error_message: 'user does not exist',
  },
]

function renderJobs() {
  vi.spyOn(api, 'get').mockImplementation(async (url, config) => {
    if (url === '/api/user/batch/jobs') {
      return {
        data: {
          success: true,
          data: {
            items: [job],
            total: 40,
            page: config?.params?.p ?? 1,
            page_size: 20,
          },
        },
      }
    }
    if (url === '/api/verify/methods') {
      return {
        data: {
          success: true,
          data: {
            scope: 'admin.user.batch',
            methods: [{ method: '2fa', available: true }],
            oauth_providers: [],
            password_encryption_enabled: false,
          },
        },
      }
    }
    return { data: { success: true, data: { job, items } } }
  })
  return render(
    <UsersProvider>
      <BatchJobsSheet open onOpenChange={vi.fn()} />
    </UsersProvider>
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
})

it('retry verifies only failed users and sends a single-use proof bound to the stored batch hash', async () => {
  const post = vi.spyOn(api, 'post').mockImplementation(async (url) => {
    if (url === '/api/verify') {
      return {
        data: {
          success: true,
          data: {
            proof_token: 'retry-proof',
            method: '2fa',
            scope: 'admin.user.batch',
            expires_at: Math.floor(Date.now() / 1000) + 60,
          },
        },
      }
    }
    return { data: { success: true, data: { job, items } } }
  })
  renderJobs()
  await userEvent.click(
    await screen.findByRole('button', { name: 'Retry failed' })
  )
  const code = await screen.findByLabelText('Authenticator code or backup code')
  expect(
    screen.queryByRole('dialog', { name: 'Bulk action jobs' })
  ).not.toBeInTheDocument()
  expect(post).not.toHaveBeenCalled()
  await userEvent.type(code, '123456')
  await userEvent.click(screen.getByRole('button', { name: 'Verify' }))
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/user/batch/jobs/job-retry/retry',
      {},
      expect.objectContaining({
        headers: { 'X-Security-Proof': 'retry-proof' },
        singleUseAuthorization: true,
      })
    )
  )
  expect(post).toHaveBeenCalledWith(
    '/api/verify',
    expect.objectContaining({
      context: { action: 'disable', user_ids: [3], batch_hash: job.batch_hash },
    }),
    expect.anything()
  )
})

it('next page uses the backend p query parameter', async () => {
  renderJobs()
  await userEvent.click(await screen.findByRole('button', { name: 'Next' }))
  await waitFor(() =>
    expect(api.get).toHaveBeenCalledWith('/api/user/batch/jobs', {
      params: { p: 2, page_size: 20 },
    })
  )
  expect(await screen.findByText('2 / 2')).toBeInTheDocument()
})
