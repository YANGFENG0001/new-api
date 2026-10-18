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
import type { Table } from '@tanstack/react-table'
import { ChevronDown, History } from 'lucide-react'
import { Fragment, useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTableBulkActions as BulkActionsToolbar } from '@/components/data-table'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

import { getBatchActions } from '../batch/api'
import type { BatchActionInfo } from '../batch/types'
import type { User } from '../types'
import { BatchActionDialog } from './batch-action-dialog'
import { BatchJobsSheet } from './batch-jobs-sheet'
import { useUsers } from './users-provider'

interface DataTableBulkActionsProps {
  table: Table<User>
}

const GROUPS: { key: string; actions: string[] }[] = [
  {
    key: 'Account state',
    actions: ['enable', 'disable', 'block', 'unblock', 'group'],
  },
  { key: 'Quota', actions: ['add_quota', 'subtract_quota', 'override_quota'] },
  { key: 'Role', actions: ['promote', 'demote'] },
  {
    key: 'Security',
    actions: ['reset_2fa', 'reset_passkey', 'clear_bindings'],
  },
  {
    key: 'Subscriptions',
    actions: ['invalidate_subscriptions', 'delete_subscriptions'],
  },
  { key: 'Danger zone', actions: ['delete'] },
]

export function DataTableBulkActions({ table }: DataTableBulkActionsProps) {
  const { t } = useTranslation()
  const { triggerRefresh } = useUsers()
  const [actions, setActions] = useState<BatchActionInfo[]>([])
  const [canRead, setCanRead] = useState(false)
  const [selectedAction, setSelectedAction] = useState<BatchActionInfo | null>(
    null
  )
  const [jobsOpen, setJobsOpen] = useState(false)

  const selectedRows = table.getFilteredSelectedRowModel().rows
  const selectedIds = useMemo(
    () => selectedRows.map((row) => row.original.id),
    [selectedRows]
  )

  const loadActions = useCallback(async () => {
    try {
      const result = await getBatchActions()
      if (result.success && result.data) {
        setActions(result.data.actions)
        setCanRead(result.data.can_read)
      }
    } catch {
      // A user without user_batch.read simply sees no bulk controls.
      setActions([])
      setCanRead(false)
    }
  }, [])

  useEffect(() => {
    if (selectedIds.length > 0) void loadActions()
  }, [loadActions, selectedIds.length])

  const anyAllowed = actions.some((item) => item.allowed)
  if (selectedIds.length === 0) return null

  return (
    <>
      <BulkActionsToolbar table={table} entityName={t('User')}>
        <Button
          variant='outline'
          size='sm'
          onClick={() => setJobsOpen(true)}
          disabled={!canRead}
        >
          <History className='size-4' />
          {t('Batch jobs')}
        </Button>

        {anyAllowed && (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button size='sm' variant='default'>
                  {t('Bulk actions')}
                  <ChevronDown className='size-4' />
                </Button>
              }
            />
            <DropdownMenuContent align='end' className='w-60'>
              {GROUPS.map((group, index) => {
                const items = group.actions
                  .map((action) =>
                    actions.find((item) => item.action === action)
                  )
                  .filter((item): item is BatchActionInfo =>
                    Boolean(item?.allowed)
                  )
                if (items.length === 0) return null
                return (
                  <Fragment key={group.key}>
                    {index > 0 && <DropdownMenuSeparator />}
                    <DropdownMenuGroup>
                      <DropdownMenuLabel>{t(group.key)}</DropdownMenuLabel>
                      {items.map((item) => (
                        <DropdownMenuItem
                          key={item.action}
                          variant={item.destructive ? 'destructive' : 'default'}
                          onClick={() => setSelectedAction(item)}
                        >
                          {t(item.action)}
                        </DropdownMenuItem>
                      ))}
                    </DropdownMenuGroup>
                  </Fragment>
                )
              })}
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </BulkActionsToolbar>

      <BatchActionDialog
        action={selectedAction}
        userIds={selectedIds}
        onClose={() => setSelectedAction(null)}
        onCompleted={() => {
          table.resetRowSelection()
          triggerRefresh()
        }}
      />
      <BatchJobsSheet open={jobsOpen} onOpenChange={setJobsOpen} />
    </>
  )
}
