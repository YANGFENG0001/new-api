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
import { AlertTriangle, CheckCircle2, Loader2, ShieldCheck } from 'lucide-react'
import { nanoid } from 'nanoid'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Separator } from '@/components/ui/separator'
import { Textarea } from '@/components/ui/textarea'
import { parseQuotaFromDollars } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'

import { executeBatch, previewBatch } from '../batch/api'
import type {
  BatchActionInfo,
  BatchPreview,
  BatchRequest,
} from '../batch/types'
import { useUsers } from './users-provider'

interface BatchActionDialogProps {
  action: BatchActionInfo | null
  userIds: number[]
  onClose: () => void
  onCompleted: () => void
}

const REASON_ACTIONS = new Set([
  'add_quota',
  'subtract_quota',
  'override_quota',
  'delete',
  'promote',
  'demote',
  'reset_2fa',
  'reset_passkey',
  'clear_bindings',
  'invalidate_subscriptions',
  'delete_subscriptions',
  'block',
])

export function BatchActionDialog({
  action,
  userIds,
  onClose,
  onCompleted,
}: BatchActionDialogProps) {
  const { t } = useTranslation()
  const { requestVerification, verificationActive, triggerRefresh } = useUsers()
  const [group, setGroup] = useState('')
  const [quotaDollars, setQuotaDollars] = useState('')
  const [reason, setReason] = useState('')
  const [note, setNote] = useState('')
  const [preview, setPreview] = useState<BatchPreview | null>(null)
  const [previewRequest, setPreviewRequest] = useState('')
  const [loading, setLoading] = useState(false)
  const [executing, setExecuting] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    setGroup('')
    setQuotaDollars('')
    setReason('')
    setNote('')
    setPreview(null)
    setError('')
  }, [action?.action, userIds.length])

  const parseQuotaInput = useCallback((): number | null => {
    if (!quotaDollars.trim()) return null
    const parsed = Number(quotaDollars)
    if (!Number.isFinite(parsed) || parsed < 0) return null
    // Only override_quota accepts zero, which resets the balance to empty.
    if (action?.action !== 'override_quota' && parsed <= 0) return null
    const quota = parseQuotaFromDollars(parsed)
    if (!Number.isSafeInteger(quota)) return null
    if (action?.action !== 'override_quota' && quota <= 0) return null
    return quota
  }, [action?.action, quotaDollars])

  const buildRequest = useCallback((): BatchRequest | null => {
    if (!action) return null
    const request: BatchRequest = {
      action: action.action,
      user_ids: userIds,
    }
    if (action.action === 'group') {
      if (!group.trim()) return null
      request.group = group.trim()
    }
    if (action.quota) {
      const parsed = parseQuotaInput()
      if (parsed === null) return null
      request.quota = parsed
    }
    if (reason.trim()) request.reason = reason.trim()
    if (note.trim()) request.note = note.trim()
    return request
  }, [action, group, note, parseQuotaInput, reason, userIds])

  const canPreview = useMemo(() => {
    if (!action || userIds.length === 0) return false
    if (action.action === 'group') return group.trim().length > 0
    if (action.quota) {
      return parseQuotaInput() !== null
    }
    return true
  }, [action, group, parseQuotaInput, userIds.length])

  const previewCurrent =
    preview !== null && previewRequest === JSON.stringify(buildRequest())

  const handlePreview = async () => {
    const request = buildRequest()
    if (!request) return
    setLoading(true)
    setPreview(null)
    setError('')
    try {
      const result = await previewBatch(request)
      if (!result.success || !result.data) {
        setError(t(result.message || 'Preview failed'))
        return
      }
      setPreview(result.data)
      setPreviewRequest(JSON.stringify(request))
    } catch (err) {
      handleServerError(err, t('Preview failed'))
    } finally {
      setLoading(false)
    }
  }

  const handleExecute = async () => {
    if (!preview || !previewCurrent || !action) return
    const request = buildRequest()
    if (!request) return
    const eligibleIds = preview.items
      .filter((item) => item.eligible)
      .map((item) => item.user_id)
    if (eligibleIds.length === 0) return
    setExecuting(true)
    try {
      const proof = await requestVerification({
        scope: 'admin.user.batch',
        context: {
          action: preview.action,
          user_ids: eligibleIds,
          batch_hash: preview.batch_hash,
        },
        title: t('Verify to run bulk action'),
        description: t(
          'Confirm your identity before applying "{{action}}" to {{count}} users.',
          { action: t(action.action), count: eligibleIds.length }
        ),
      })
      if (!proof) return
      const result = await executeBatch(
        { ...request, idempotency_key: nanoid() },
        preview.batch_hash,
        proof.proof_token
      )
      if (!result.success || !result.data) {
        handleServerError(result, t('Bulk action failed'))
        return
      }
      const job = result.data.job
      toast.success(
        ['pending', 'running'].includes(job.status)
          ? t('Bulk action started')
          : t(
              'Bulk action finished: {{succeeded}} succeeded, {{failed}} failed, {{skipped}} skipped',
              {
                succeeded: job.succeeded,
                failed: job.failed,
                skipped: job.skipped,
              }
            )
      )
      triggerRefresh()
      onCompleted()
      onClose()
    } catch (err) {
      handleServerError(err, t('Bulk action failed'))
    } finally {
      setExecuting(false)
    }
  }

  const title = action ? t('Bulk {{action}}', { action: t(action.action) }) : ''

  return (
    <Dialog
      open={action !== null && !verificationActive}
      onOpenChange={(open) => !open && !executing && onClose()}
      contentClassName='max-h-[90vh] sm:max-w-3xl'
      bodyClassName='space-y-4'
      titleClassName='flex items-center gap-2'
      showCloseButton={!executing}
      title={
        <>
          {action?.destructive ? (
            <AlertTriangle className='text-destructive size-5' />
          ) : (
            <ShieldCheck className='size-5' />
          )}
          {title}
        </>
      }
      description={
        <>
          {t(
            '{{count}} users selected. Preview the eligible set before executing.',
            {
              count: userIds.length,
            }
          )}
        </>
      }
      footer={
        <>
          <Button variant='outline' onClick={onClose} disabled={executing}>
            {t('Cancel')}
          </Button>
          <Button
            variant='secondary'
            onClick={handlePreview}
            disabled={!canPreview || loading || executing}
          >
            {loading && <Loader2 className='size-4 animate-spin' />}
            {t('Preview')}
          </Button>
          <Button
            variant={action?.destructive ? 'destructive' : 'default'}
            onClick={handleExecute}
            disabled={
              !previewCurrent ||
              !preview ||
              preview.eligible === 0 ||
              loading ||
              executing
            }
          >
            {executing && <Loader2 className='size-4 animate-spin' />}
            {t('Verify and execute')}
          </Button>
        </>
      }
    >
      {action?.action === 'group' && (
        <div className='space-y-2'>
          <Label>{t('Target group')}</Label>
          <Input
            disabled={executing}
            value={group}
            onChange={(event) => setGroup(event.target.value)}
            placeholder={t('Group name')}
          />
        </div>
      )}

      {action?.quota && (
        <div className='space-y-2'>
          <Label>
            {action.action === 'override_quota'
              ? t('Quota to set (USD)')
              : t('Quota amount (USD)')}
          </Label>
          <Input
            disabled={executing}
            type='number'
            min='0'
            step='0.01'
            value={quotaDollars}
            onChange={(event) => setQuotaDollars(event.target.value)}
            placeholder='1.00'
          />
        </div>
      )}

      {action && REASON_ACTIONS.has(action.action) && (
        <div className='space-y-2'>
          <Label>{t('Reason (recorded in the audit log)')}</Label>
          <Textarea
            disabled={executing}
            value={reason}
            onChange={(event) => setReason(event.target.value)}
            rows={2}
            maxLength={200}
          />
        </div>
      )}

      <div className='space-y-2'>
        <Label>{t('Internal note (optional)')}</Label>
        <Input
          disabled={executing}
          value={note}
          onChange={(event) => setNote(event.target.value)}
          maxLength={200}
        />
      </div>

      {error && (
        <p className='text-destructive text-sm' role='alert'>
          {error}
        </p>
      )}

      {preview && (
        <>
          <Separator />
          <div className='flex flex-wrap items-center gap-2 text-sm'>
            <Badge variant='secondary'>
              {t('{{count}} eligible', { count: preview.eligible })}
            </Badge>
            <Badge variant='outline'>
              {t('{{count}} skipped', { count: preview.skipped })}
            </Badge>
            <span className='text-muted-foreground'>
              {t('Preview hash')}: {preview.batch_hash.slice(0, 12)}
            </span>
          </div>
          {(preview.warnings ?? []).length > 0 && (
            <ul className='list-disc space-y-1 pl-5 text-sm text-amber-600'>
              {preview.warnings?.map((warning) => (
                <li key={warning}>{t(warning)}</li>
              ))}
            </ul>
          )}
          <ScrollArea className='h-64 rounded-md border'>
            <table className='w-full text-sm'>
              <thead className='bg-muted/80 sticky top-0 text-left'>
                <tr>
                  <th className='p-2'>{t('User')}</th>
                  <th className='p-2'>{t('Status')}</th>
                  <th className='p-2'>{t('Details')}</th>
                </tr>
              </thead>
              <tbody>
                {preview.items.map((item) => (
                  <tr key={item.user_id} className='border-t'>
                    <td className='p-2'>
                      {item.username}
                      <span className='text-muted-foreground'>
                        {' '}
                        #{item.user_id}
                      </span>
                    </td>
                    <td className='p-2'>
                      {item.eligible ? (
                        <span className='inline-flex items-center gap-1 text-emerald-600'>
                          <CheckCircle2 className='size-4' />
                          {t('Eligible')}
                        </span>
                      ) : (
                        <span className='text-muted-foreground'>
                          {t('Skipped')}
                        </span>
                      )}
                    </td>
                    <td className='text-muted-foreground p-2'>
                      {item.eligible
                        ? (item.warnings ?? [])
                            .map((warning) => t(warning))
                            .join('; ') || '-'
                        : t(item.skip_reason || '-')}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </ScrollArea>
        </>
      )}
    </Dialog>
  )
}
