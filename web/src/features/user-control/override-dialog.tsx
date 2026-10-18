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
import { useEffect, useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
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
  const formId = useId()
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
    <Dialog
      open={userId !== null}
      onOpenChange={(open) => {
        if (!open && !saving) onClose()
      }}
      title={t('User override #{{id}}', { id: userId })}
      description={t(
        'Override limits are merged with the user\u0027s policy. Replace mode bypasses inherited limits and requires the relax permission.'
      )}
      contentClassName='sm:max-w-4xl'
      showCloseButton={false}
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={onClose}
            disabled={saving}
          >
            {t('Cancel')}
          </Button>
          <Button type='submit' form={formId} disabled={saving}>
            {saving && <Loader2 className='size-4 animate-spin' />}
            {t('Save')}
          </Button>
        </>
      }
    >
      <form
        id={formId}
        className='min-w-0 space-y-4'
        inert={saving}
        onSubmit={(event) => {
          event.preventDefault()
          void handleSave()
        }}
      >
        <div className='grid min-w-0 gap-3 md:grid-cols-2'>
          <div className='min-w-0 space-y-1'>
            <Label htmlFor={`${formId}-policy`}>{t('Policy template')}</Label>
            <Select
              items={[
                { value: '0', label: t('None (limits only)') },
                ...policies.map((item) => ({
                  value: String(item.policy.id),
                  label: item.policy.name,
                })),
              ]}
              value={String(policyId)}
              onValueChange={(value) => setPolicyId(Number(value))}
            >
              <SelectTrigger id={`${formId}-policy`} className='w-full min-w-0'>
                <SelectValue>
                  {policies.find((item) => item.policy.id === policyId)?.policy
                    .name ?? t('None (limits only)')}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value='0'>{t('None (limits only)')}</SelectItem>
                  {policies.map((item) => (
                    <SelectItem
                      key={item.policy.id}
                      value={String(item.policy.id)}
                    >
                      {item.policy.name}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </div>
          <div className='min-w-0 space-y-1'>
            <Label htmlFor={`${formId}-mode`}>{t('Mode')}</Label>
            <Select
              items={[
                { value: 'merge', label: t('Merge (tighten only)') },
                {
                  value: 'replace',
                  label: t('Replace (relax inherited limits)'),
                },
              ]}
              value={mode}
              onValueChange={(value) =>
                setMode(value === 'replace' ? 'replace' : 'merge')
              }
            >
              <SelectTrigger id={`${formId}-mode`} className='w-full min-w-0'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value='merge'>
                    {t('Merge (tighten only)')}
                  </SelectItem>
                  <SelectItem value='replace' disabled={!canRelax}>
                    {t('Replace (relax inherited limits)')}
                  </SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </div>
          <div className='min-w-0 space-y-1'>
            <Label htmlFor={`${formId}-expiry`}>
              {t('Expires at (optional)')}
            </Label>
            <Input
              id={`${formId}-expiry`}
              type='datetime-local'
              value={expiresAt}
              onChange={(event) => setExpiresAt(event.target.value)}
            />
          </div>
          <div className='min-w-0 space-y-1'>
            <Label htmlFor={`${formId}-reason`}>{t('Reason')}</Label>
            <Textarea
              id={`${formId}-reason`}
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
          <ScheduleEditor
            value={schedule}
            onChange={setSchedule}
            defaultTimezone={defaultTimezone}
          />
        </section>
      </form>
    </Dialog>
  )
}
