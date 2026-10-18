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
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { handleServerError } from '@/lib/handle-server-error'

import { createPolicy, updatePolicy } from './api'
import { LimitsEditor } from './limits-editor'
import type { Limits, PolicyView } from './types'

export function PolicyEditorDialog({
  open,
  policy,
  onOpenChange,
  onSaved,
}: {
  open: boolean
  policies?: PolicyView[]
  createPool?: boolean
  policy: PolicyView | null
  defaultTimezone: string
  onOpenChange: (open: boolean) => void
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const formId = useId()
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [enabled, setEnabled] = useState(true)
  const [priority, setPriority] = useState(0)
  const [limits, setLimits] = useState<Limits>({})
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!open) return
    if (policy) {
      setName(policy.policy.name)
      setDescription(policy.policy.description)
      setEnabled(policy.policy.enabled)
      setPriority(policy.policy.priority)
      setLimits(policy.limits ?? {})
    } else {
      setName('')
      setDescription('')
      setEnabled(true)
      setPriority(0)
      setLimits({})
    }
  }, [open, policy])

  const handleSave = async () => {
    if (!name.trim()) {
      toast.error(t('Policy name is required'))
      return
    }
    setSaving(true)
    try {
      const payload = {
        name: name.trim(),
        description,
        enabled,
        priority,
        limits,
        schema_version: 2,
        schedule: null,
      }
      const result = policy
        ? await updatePolicy(policy.policy.id, payload)
        : await createPolicy(payload)
      if (!result.success) {
        handleServerError(result, t('Failed to save policy'))
        return
      }
      toast.success(t('Policy saved'))
      onSaved()
      onOpenChange(false)
    } catch (err) {
      handleServerError(err, t('Failed to save policy'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!saving) onOpenChange(nextOpen)
      }}
      title={policy ? t('Edit control policy') : t('New control policy')}
      description={t(
        'Policies define behavior only. Apply directly for all-day control, or add to a pool for scheduling.'
      )}
      contentClassName='sm:max-w-4xl'
      titleClassName='break-words pr-6'
      showCloseButton={false}
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => onOpenChange(false)}
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
        className='min-w-0 space-y-5'
        inert={saving}
        onSubmit={(event) => {
          event.preventDefault()
          void handleSave()
        }}
      >
        <div className='grid min-w-0 gap-3 md:grid-cols-2'>
          <div className='min-w-0 space-y-1'>
            <Label htmlFor={`${formId}-name`}>{t('Name')}</Label>
            <Input
              id={`${formId}-name`}
              value={name}
              onChange={(event) => setName(event.target.value)}
              maxLength={64}
            />
          </div>
          <div className='min-w-0 space-y-1'>
            <Label htmlFor={`${formId}-priority`}>{t('Priority')}</Label>
            <Input
              id={`${formId}-priority`}
              type='number'
              value={priority}
              onChange={(event) =>
                setPriority(Number.parseInt(event.target.value || '0', 10) || 0)
              }
            />
          </div>
          <div className='space-y-1 md:col-span-2'>
            <Label htmlFor={`${formId}-description`}>{t('Description')}</Label>
            <Textarea
              id={`${formId}-description`}
              rows={2}
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              maxLength={255}
            />
          </div>
          <label className='flex items-center gap-2 text-sm'>
            <Switch checked={enabled} onCheckedChange={setEnabled} />
            {t('Enabled')}
          </label>
        </div>

        <section className='space-y-2 rounded-lg border p-3'>
          <h3 className='text-sm font-semibold'>{t('Base limits')}</h3>
          <LimitsEditor value={limits} onChange={setLimits} />
        </section>
      </form>
    </Dialog>
  )
}
