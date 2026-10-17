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
import { Loader2, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Textarea } from '@/components/ui/textarea'
import { handleServerError } from '@/lib/handle-server-error'

import { addMembers, listMembers, removeMembers } from './api'
import type { ControlList, ListMember } from './types'

export function MembersDialog({
  list,
  canWrite,
  onClose,
}: {
  list: ControlList | null
  canWrite: boolean
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [members, setMembers] = useState<ListMember[]>([])
  const [input, setInput] = useState('')
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)

  const load = async (listId: number) => {
    setLoading(true)
    try {
      const result = await listMembers(listId)
      if (result.success && result.data) setMembers(result.data.items)
    } catch (err) {
      handleServerError(err, t('Failed to load list members'))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    if (list) void load(list.id)
    else setMembers([])
    setInput('')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [list?.id])

  const handleAdd = async () => {
    if (!list) return
    const ids = input
      .split(/[,\s]+/)
      .map((value) => Number.parseInt(value, 10))
      .filter((value) => Number.isFinite(value) && value > 0)
    if (ids.length === 0) {
      toast.error(t('Enter at least one user ID'))
      return
    }
    setSaving(true)
    try {
      const result = await addMembers(list.id, ids)
      if (result.success && result.data) {
        toast.success(
          t('{{inserted}} users added ({{requested}} requested)', {
            inserted: result.data.inserted,
            requested: result.data.requested,
          })
        )
        setInput('')
        await load(list.id)
      } else {
        handleServerError(result, t('Failed to add members'))
      }
    } catch (err) {
      handleServerError(err, t('Failed to add members'))
    } finally {
      setSaving(false)
    }
  }

  const handleRemove = async (userId: number) => {
    if (!list) return
    setSaving(true)
    try {
      const result = await removeMembers(list.id, [userId])
      if (result.success) {
        await load(list.id)
      } else {
        handleServerError(result, t('Failed to remove member'))
      }
    } catch (err) {
      handleServerError(err, t('Failed to remove member'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={list !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className='flex max-h-[85vh] flex-col sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>
            {t('Manage members')}: {list?.name}
          </DialogTitle>
          <DialogDescription>
            {t(
              'Users in this list inherit the list policy. Add several IDs at once to bulk enroll users.'
            )}
          </DialogDescription>
        </DialogHeader>

        {canWrite && (
          <div className='space-y-2'>
            <Label>{t('User IDs')}</Label>
            <Textarea
              rows={2}
              value={input}
              onChange={(event) => setInput(event.target.value)}
              placeholder={t(
                'One or more numeric IDs, separated by commas or spaces'
              )}
            />
            <Button
              size='sm'
              onClick={() => void handleAdd()}
              disabled={saving}
            >
              {saving && <Loader2 className='size-4 animate-spin' />}
              {t('Add members')}
            </Button>
          </div>
        )}

        <ScrollArea className='min-h-0 flex-1 rounded-md border'>
          {loading ? (
            <div className='flex items-center justify-center p-8'>
              <Loader2 className='size-5 animate-spin' />
            </div>
          ) : members.length === 0 ? (
            <p className='text-muted-foreground p-4 text-sm'>
              {t('No members')}
            </p>
          ) : (
            <ul className='divide-y'>
              {members.map((member) => (
                <li
                  key={member.id}
                  className='flex items-center gap-2 p-2 text-sm'
                >
                  <span className='font-medium'>{member.username || '-'}</span>
                  <span className='text-muted-foreground'>
                    #{member.user_id}
                  </span>
                  <span className='text-muted-foreground ml-auto text-xs'>
                    {new Date(member.created_at * 1000).toLocaleString()}
                  </span>
                  {canWrite && (
                    <Button
                      variant='ghost'
                      size='icon'
                      className='text-destructive size-8'
                      onClick={() => void handleRemove(member.user_id)}
                      disabled={saving}
                      aria-label={t('Remove')}
                    >
                      <Trash2 className='size-4' />
                    </Button>
                  )}
                </li>
              ))}
            </ul>
          )}
        </ScrollArea>

        <DialogFooter>
          <Button variant='outline' onClick={onClose}>
            {t('Close')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
