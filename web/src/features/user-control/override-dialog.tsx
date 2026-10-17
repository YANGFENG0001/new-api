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
import { Loader2 } from 'lucide-react'
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
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Textarea } from '@/components/ui/textarea'
import { handleServerError } from '@/lib/handle-server-error'

import { putUserOverride } from './api'
import { LimitsEditor } from './limits-editor'
import { ScheduleEditor } from './schedule-editor'
import type { Limits, PolicyView, Schedule, UserOverride } from './types'

function toLocalInput(value?: number): string {
  if (!value) return ''
  const date = new Date(value * 1000)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

export function OverrideDialog({
  userId,
  existing,
  policies,
  defaultTimezone,
  canRelax,
  onClose,
  onSaved,
}: {
  userId: number | null
  existing: UserOverride | null
  policies: PolicyView[]
  defaultTimezone: string
  canRelax: boolean
  onClose: () => void
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const [policyId, setPolicyId] = useState(0)
  const [mode, setMode] = useState<'merge' | 'replace'>('merge')
  const [expiresAt, setExpiresAt] = useState('')
  const [reason, setReason] = useState('')
  const [limits, setLimits] = useState<Limits>({})
  const [schedule, setSchedule] = useState<Schedule | null>(null)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (userId === null) return
    setPolicyId(existing?.policy_id ?? 0)
    setMode(existing?.mode ?? 'merge')
    setExpiresAt(toLocalInput(existing?.expires_at))
    setReason(existing?.reason ?? '')
    setLimits(existing?.limits_view ?? {})
    setSchedule(existing?.schedule_view ?? null)
  }, [userId, existing])

  const handleSave = async () => {
    if (userId === null) return
    setSaving(true)
    try {
      const result = await putUserOverride(userId, {
        policy_id: policyId || undefined,
        mode,
        expires_at: expiresAt
          ? Math.floor(new Date(expiresAt).getTime() / 1000)
          : 0,
        reason,
        limits,
        schedule: schedule
          ? { ...schedule, timezone: schedule.timezone || defaultTimezone }
          : null,
      })
      if (!result.success) {
        handleServerError(result, t('Failed to save override'))
        return
      }
      toast.success(t('User override saved'))
      onSaved()
      onClose()
    } catch (err) {
      handleServerError(err, t('Failed to save override'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={userId !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className='flex max-h-[92vh] flex-col sm:max-w-4xl'>
        <DialogHeader>
          <DialogTitle>
            {t('User override')} #{userId}
          </DialogTitle>
          <DialogDescription>
            {t(
              'Override limits are merged with the user\u0027s policy. Replace mode bypasses inherited limits and requires the relax permission.'
            )}
          </DialogDescription>
        </DialogHeader>

        <ScrollArea className='min-h-0 flex-1 pr-3'>
          <div className='space-y-4 pb-4'>
            <div className='grid gap-3 md:grid-cols-2'>
              <div className='space-y-1'>
                <Label>{t('Policy template')}</Label>
                <select
                  className='h-9 w-full rounded-md border bg-transparent px-2 text-sm'
                  value={policyId}
                  onChange={(event) => setPolicyId(Number(event.target.value))}
                >
                  <option value={0}>{t('None (limits only)')}</option>
                  {policies.map((item) => (
                    <option key={item.policy.id} value={item.policy.id}>
                      {item.policy.name}
                    </option>
                  ))}
                </select>
              </div>
              <div className='space-y-1'>
                <Label>{t('Mode')}</Label>
                <select
                  className='h-9 w-full rounded-md border bg-transparent px-2 text-sm'
                  value={mode}
                  onChange={(event) =>
                    setMode(
                      event.target.value === 'replace' ? 'replace' : 'merge'
                    )
                  }
                >
                  <option value='merge'>{t('Merge (tighten only)')}</option>
                  <option value='replace' disabled={!canRelax}>
                    {t('Replace (relax inherited limits)')}
                  </option>
                </select>
              </div>
              <div className='space-y-1'>
                <Label>{t('Expires at (optional)')}</Label>
                <Input
                  type='datetime-local'
                  value={expiresAt}
                  onChange={(event) => setExpiresAt(event.target.value)}
                />
              </div>
              <div className='space-y-1'>
                <Label>{t('Reason')}</Label>
                <Textarea
                  rows={1}
                  maxLength={255}
                  value={reason}
                  onChange={(event) => setReason(event.target.value)}
                />
              </div>
            </div>

            <section className='space-y-2 rounded-lg border p-3'>
              <h3 className='text-sm font-semibold'>{t('Override limits')}</h3>
              <LimitsEditor value={limits} onChange={setLimits} />
            </section>

            <section className='space-y-2 rounded-lg border p-3'>
              <h3 className='text-sm font-semibold'>
                {t('Override time windows')}
              </h3>
              <ScheduleEditor value={schedule} onChange={setSchedule} />
            </section>
          </div>
        </ScrollArea>

        <DialogFooter>
          <Button variant='outline' onClick={onClose} disabled={saving}>
            {t('Cancel')}
          </Button>
          <Button onClick={() => void handleSave()} disabled={saving}>
            {saving && <Loader2 className='size-4 animate-spin' />}
            {t('Save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
