import { useQuery } from '@tanstack/react-query'
import {
  getCoreRowModel,
  useReactTable,
  type PaginationState,
} from '@tanstack/react-table'
import { useEffect, useId, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTablePagination } from '@/components/data-table'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { getUsers, searchUsers } from '@/features/users/api'
import type { User } from '@/features/users/types'
import { hasPermission } from '@/lib/admin-permissions'
import {
  getServerErrorMessage,
  getServerErrorStatus,
  requireServerSuccess,
} from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

type UserPickerProps = {
  selected: number[]
  onChange: (ids: number[]) => void
  multiple?: boolean
  disabled?: boolean
  excludedIds?: number[]
}

const EMPTY_USERS: User[] = []
const COLUMNS = [{ accessorKey: 'id' }]

// A feature composition: the shared MultiSelect only filters local options;
// this picker keeps selection independent of server search and pagination.
export function UserPicker(props: UserPickerProps) {
  const { t } = useTranslation()
  const searchId = useId()
  const user = useAuthStore((state) => state.auth.user)
  const canRead = hasPermission(user, 'user', 'read')
  const [draft, setDraft] = useState('')
  const [keyword, setKeyword] = useState('')
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 10,
  })
  const users = useQuery({
    queryKey: ['user-control', 'user-picker', user?.id, keyword, pagination],
    queryFn: async () => {
      const params = {
        p: pagination.pageIndex + 1,
        page_size: pagination.pageSize,
      }
      const result = requireServerSuccess(
        await (keyword ? searchUsers({ ...params, keyword }) : getUsers(params))
      )
      if (!result.data) throw new Error(t('Failed to load users'))
      return result.data
    },
    enabled: canRead && !props.disabled,
    retry: false,
    refetchOnWindowFocus: false,
    meta: { errorToast: false },
  })
  const rows = users.data?.items ?? EMPTY_USERS
  const table = useReactTable({
    data: rows,
    columns: COLUMNS,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    rowCount: users.data?.total ?? 0,
    state: { pagination },
    onPaginationChange: (updater) => {
      if (props.disabled || users.isFetching) return
      setPagination(updater)
    },
  })
  useEffect(() => {
    if (!users.data) return
    const lastPage = Math.max(
      0,
      Math.ceil(users.data.total / pagination.pageSize) - 1
    )
    if (pagination.pageIndex > lastPage) {
      setPagination((current) => ({ ...current, pageIndex: lastPage }))
    }
  }, [users.data, pagination.pageSize, pagination.pageIndex])

  if (!canRead || getServerErrorStatus(users.error) === 403) {
    return (
      <ErrorState
        className='min-h-0 py-4'
        title={t('User selection unavailable')}
        description={t(
          'You do not have permission to view users. Ask an administrator for user read access.'
        )}
      />
    )
  }

  return (
    <section aria-label={t('Select users')} className='min-w-0 space-y-3'>
      <form
        className='flex min-w-0 flex-wrap items-end gap-2'
        onSubmit={(event) => {
          event.preventDefault()
          setKeyword(draft.trim())
          setPagination((current) => ({ ...current, pageIndex: 0 }))
        }}
      >
        <div className='min-w-0 flex-1 space-y-1'>
          <Label htmlFor={searchId}>{t('Search users')}</Label>
          <Input
            id={searchId}
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            placeholder={t('Search by username, display name, email or ID')}
            disabled={props.disabled}
          />
        </div>
        <Button type='submit' variant='outline' disabled={props.disabled}>
          {t('Search')}
        </Button>
      </form>
      <div
        className='flex flex-wrap items-center justify-between gap-2 text-sm'
        aria-live='polite'
      >
        <span>
          {t('{{count}} users selected', { count: props.selected.length })}
        </span>
        <Button
          type='button'
          size='sm'
          variant='ghost'
          disabled={props.disabled || props.selected.length === 0}
          onClick={() => props.onChange([])}
        >
          {t('Clear selection')}
        </Button>
      </div>
      <div
        className='max-h-72 overflow-y-auto overscroll-contain rounded-md border'
        aria-busy={users.isFetching}
      >
        {users.isFetching && <LoadingState className='min-h-0 py-6' />}
        {!users.isFetching && users.isError && (
          <ErrorState
            className='min-h-0 py-4'
            title={t('Failed to load users')}
            description={getServerErrorMessage(users.error)}
            onRetry={() => void users.refetch()}
          />
        )}
        {!users.isFetching && !users.isError && rows.length === 0 && (
          <EmptyState className='min-h-0 py-4' title={t('No matching users')} />
        )}
        {!users.isFetching && !users.isError && rows.length > 0 && (
          <ul className='divide-y'>
            {rows.map((item) => {
              const excluded = props.excludedIds?.includes(item.id) ?? false
              const label = t('Select user {{name}} (#{{id}})', {
                name: item.username,
                id: item.id,
              })
              return (
                <li key={item.id}>
                  <label className='flex min-w-0 items-center gap-3 p-3'>
                    <span
                      id={`${searchId}-user-${item.id}`}
                      className='sr-only'
                    >
                      {label}
                    </span>
                    <Checkbox
                      aria-labelledby={`${searchId}-user-${item.id}`}
                      checked={excluded || props.selected.includes(item.id)}
                      disabled={props.disabled || excluded}
                      onCheckedChange={(checked) => {
                        if (!checked) {
                          props.onChange(
                            props.selected.filter((id) => id !== item.id)
                          )
                        } else {
                          props.onChange(
                            props.multiple
                              ? [...new Set([...props.selected, item.id])]
                              : [item.id]
                          )
                        }
                      }}
                    />
                    <span className='min-w-0 flex-1 [overflow-wrap:anywhere] break-words'>
                      <span className='font-medium'>{item.username}</span>
                      {item.display_name && (
                        <span className='text-muted-foreground ml-2'>
                          {item.display_name}
                        </span>
                      )}
                      <span className='text-muted-foreground block text-xs'>
                        #{item.id} · {item.email || item.group}
                      </span>
                    </span>
                    {excluded && (
                      <span className='text-muted-foreground text-xs'>
                        {t('Already a member')}
                      </span>
                    )}
                  </label>
                </li>
              )
            })}
          </ul>
        )}
      </div>
      <fieldset
        disabled={props.disabled || users.isFetching || users.isError}
        className='min-w-0'
      >
        <DataTablePagination table={table} compact />
      </fieldset>
    </section>
  )
}
