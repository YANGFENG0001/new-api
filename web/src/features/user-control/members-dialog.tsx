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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  getCoreRowModel,
  useReactTable,
  type PaginationState,
} from '@tanstack/react-table'
import { Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { DataTablePagination } from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { toIntlLocale } from '@/i18n/languages'
import {
  getServerErrorMessage,
  requireServerSuccess,
} from '@/lib/server-error-message'

import {
  addMembers,
  listMembers,
  removeMembers,
  type SkippedMember,
} from './api'
import type { ControlList, ListMember } from './types'
import { UserPicker } from './user-picker'

type MembersDialogProps = {
  list: ControlList | null
  canWrite: boolean
  onClose: () => void
}

export function MembersDialog(props: MembersDialogProps) {
  if (!props.list) return null
  return (
    <MembersDialogContent
      key={props.list.id}
      list={props.list}
      canWrite={props.canWrite}
      onClose={props.onClose}
    />
  )
}

function MembersDialogContent(
  props: MembersDialogProps & { list: ControlList }
) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const queryClient = useQueryClient()
  const [selected, setSelected] = useState<number[]>([])
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 20,
  })
  const [outcome, setOutcome] = useState<{
    message: string
    skipped: SkippedMember[]
  } | null>(null)
  const queryKey = ['user-control', 'members', props.list.id]
  const members = useQuery({
    queryKey: [...queryKey, pagination],
    queryFn: async () => {
      const result = requireServerSuccess(
        await listMembers(props.list.id, {
          p: pagination.pageIndex + 1,
          page_size: pagination.pageSize,
        })
      )
      if (!result.data) throw new Error(t('Failed to load list members'))
      return result.data
    },
    retry: false,
    meta: { errorToast: false },
  })
  const mutation = useMutation({
    mutationFn: async (action: { type: 'add' | 'remove'; ids: number[] }) => {
      if (!props.canWrite) {
        throw new Error(t('You do not have permission to manage list members.'))
      }
      if (action.type === 'add') {
        const result = requireServerSuccess(
          await addMembers(props.list.id, action.ids)
        )
        if (!result.data) throw new Error(t('Failed to add members'))
        return { type: 'add' as const, data: result.data }
      }
      const result = requireServerSuccess(
        await removeMembers(props.list.id, action.ids)
      )
      if (!result.data) throw new Error(t('Failed to remove member'))
      return { type: 'remove' as const, data: result.data }
    },
    onMutate: () => setOutcome(null),
    onSuccess: async (result) => {
      const skipped = result.data.skipped ?? []
      let message: string
      if (result.type === 'add') {
        const { inserted, requested } = result.data
        const unchanged = Math.max(0, requested - inserted - skipped.length)
        message = t(
          '{{inserted}} users added, {{skipped}} skipped, {{unchanged}} already members ({{requested}} requested)',
          { inserted, skipped: skipped.length, unchanged, requested }
        )
        setSelected((current) =>
          current.filter((id) => skipped.some((item) => item.user_id === id))
        )
        if (inserted === requested) toast.success(message)
        else toast.warning(message)
      } else {
        message = t('{{removed}} members removed, {{skipped}} skipped', {
          removed: result.data.removed,
          skipped: skipped.length,
        })
      }
      setOutcome({ message, skipped })
      await queryClient.invalidateQueries({ queryKey })
    },
    // The form shows the failure and retains the selection for retry.
    meta: { errorToast: false },
  })
  const rows = members.data?.items ?? []
  const table = useReactTable<ListMember>({
    data: rows,
    columns: [{ accessorKey: 'user_id' }],
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    rowCount: members.data?.total ?? 0,
    state: { pagination },
    onPaginationChange: (updater) => {
      if (!mutation.isPending && !members.isFetching) setPagination(updater)
    },
  })
  useEffect(() => {
    if (!members.data) return
    const lastPage = Math.max(
      0,
      Math.ceil(members.data.total / pagination.pageSize) - 1
    )
    if (pagination.pageIndex > lastPage) {
      setPagination((current) => ({ ...current, pageIndex: lastPage }))
    }
  }, [members.data, pagination.pageSize, pagination.pageIndex])
  const existingIds = rows.map((member) => member.user_id)
  const additions = selected.filter((id) => !existingIds.includes(id))
  const busy = mutation.isPending

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !busy) props.onClose()
      }}
      title={t('Manage members: {{name}}', { name: props.list.name })}
      description={t(
        'Users in this list inherit the list policy. Search and select users to add them.'
      )}
      contentClassName='sm:max-w-2xl'
      titleClassName='break-words pr-6 [overflow-wrap:anywhere]'
      showCloseButton={false}
      footer={
        <Button
          type='button'
          variant='outline'
          onClick={props.onClose}
          disabled={busy}
        >
          {t('Close')}
        </Button>
      }
    >
      <div className='min-w-0 space-y-4'>
        {props.canWrite && (
          <>
            <UserPicker
              multiple
              selected={selected}
              onChange={setSelected}
              excludedIds={existingIds}
              disabled={busy || members.isPending || members.isError}
            />
            <Button
              type='button'
              onClick={() => mutation.mutate({ type: 'add', ids: additions })}
              disabled={
                busy ||
                members.isFetching ||
                members.isError ||
                additions.length === 0
              }
            >
              {t('Add members')}
            </Button>
          </>
        )}
        {outcome && (
          <div
            role='status'
            className='space-y-1 rounded-md border p-3 text-sm'
          >
            <p>{outcome.message}</p>
            {outcome.skipped.map((item) => (
              <p key={item.user_id}>
                {t('User #{{id}}: {{reason}}', {
                  id: item.user_id,
                  reason: t(item.reason),
                })}
              </p>
            ))}
          </div>
        )}
        {mutation.isError && (
          <div role='alert'>
            <ErrorState
              className='min-h-0 py-4'
              title={t('Failed to update list members')}
              description={getServerErrorMessage(mutation.error)}
            />
          </div>
        )}
        <section
          aria-label={t('Current members')}
          className='min-w-0 space-y-2'
        >
          <h3 className='text-sm font-medium'>{t('Current members')}</h3>
          {members.isPending && <LoadingState className='min-h-0 py-6' />}
          {members.isError && (
            <ErrorState
              className='min-h-0 py-4'
              title={t('Failed to load list members')}
              description={getServerErrorMessage(members.error)}
              onRetry={() => void members.refetch()}
            />
          )}
          {members.isSuccess && rows.length === 0 && (
            <EmptyState className='min-h-0 py-4' title={t('No members')} />
          )}
          {members.isSuccess && rows.length > 0 && (
            <ul className='max-h-64 divide-y overflow-y-auto overscroll-contain rounded-md border'>
              {rows.map((member) => (
                <li
                  key={member.id}
                  className='flex min-w-0 flex-wrap items-center gap-2 p-3 text-sm'
                >
                  <span className='min-w-0 flex-1 [overflow-wrap:anywhere] break-words'>
                    {member.username || '-'} #{member.user_id}
                  </span>
                  <span className='text-muted-foreground text-xs'>
                    {new Date(member.created_at * 1000).toLocaleString(locale)}
                  </span>
                  {props.canWrite && (
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon'
                      className='text-destructive size-8'
                      aria-label={t('Remove member {{name}} (#{{id}})', {
                        name: member.username,
                        id: member.user_id,
                      })}
                      disabled={busy}
                      onClick={() =>
                        mutation.mutate({
                          type: 'remove',
                          ids: [member.user_id],
                        })
                      }
                    >
                      <Trash2 className='size-4' />
                    </Button>
                  )}
                </li>
              ))}
            </ul>
          )}
          {members.isSuccess && (
            <fieldset disabled={busy || members.isFetching} className='min-w-0'>
              <DataTablePagination table={table} compact />
            </fieldset>
          )}
        </section>
      </div>
    </Dialog>
  )
}
