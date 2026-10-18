import { zodResolver } from '@hookform/resolvers/zod'
import { useEffect, useId, useState, useRef } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { MultiSelect } from '@/components/multi-select'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { handleServerError } from '@/lib/handle-server-error'
import {
  getServerErrorMessage,
  requireServerSuccess,
} from '@/lib/server-error-message'

import { saveApplication, restoreLegacyApplication } from './api'
import { parseTargets } from './application-targets'
import {
  isBehaviorPolicy,
  type Application,
  type ApplicationTarget,
  type OwnerKind,
  type PolicyView,
  type PoolView,
} from './types'

const schema = z.object({
  targets: z.array(z.string()),
  replace: z.boolean(),
  expires: z.string(),
  reason: z.string().max(255),
  confirm: z.boolean(),
  operation: z.enum(['set', 'append', 'remove']),
})
type Form = z.infer<typeof schema>

function localDate(value?: number) {
  if (!value) return ''
  const d = new Date(value * 1000)
  const offset = d.getTimezoneOffset() * 60_000
  return new Date(d.getTime() - offset).toISOString().slice(0, 16)
}

export function ApplicationEditor(props: {
  owner: { kind: OwnerKind; id: number; name: string } | null
  bulkUsers?: number[]
  applications?: Application[]
  application?: Application
  legacy?: string
  policies: PolicyView[]
  pools: PoolView[]
  canRestore?: boolean
  canRelax: boolean
  onClose: () => void
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const id = useId()
  const form = useForm<Form>({
    resolver: zodResolver(schema),
    defaultValues: {
      targets: [],
      replace: false,
      expires: '',
      reason: '',
      confirm: false,
      operation: 'set',
    },
  })
  const [outcomes, setOutcomes] = useState<
    { id: number; success: boolean; message?: string }[]
  >([])
  const [restoreOpen, setRestoreOpen] = useState(false)
  const [restoring, setRestoring] = useState(false)
  const initialized = useRef<string | null>(null)
  const [invalidData, setInvalidData] = useState(false)
  const values = useWatch({ control: form.control })
  useEffect(() => {
    if (!props.owner) {
      initialized.current = null
      return
    }
    const key = `${props.owner.kind}:${props.owner.id}:${props.bulkUsers?.join(',') ?? ''}`
    if (initialized.current === key) return
    initialized.current = key
    setInvalidData(false)
    setOutcomes([])
    try {
      form.reset({
        targets: parseTargets(
          props.application?.use_legacy ? undefined : props.application?.targets
        ).map((target) => `${target.kind}:${target.id}`),
        replace:
          !props.application?.use_legacy &&
          props.application?.mode === 'replace',
        expires: localDate(props.application?.expires_at),
        reason: props.application?.reason ?? '',
        confirm: false,
        operation: props.bulkUsers?.length ? 'append' : 'set',
      })
    } catch (err) {
      setInvalidData(true)
      handleServerError(err, t('Invalid application configuration'))
    }
  }, [props.owner, props.application, props.bulkUsers, form, t])
  const options = [
    ...props.policies.filter(isBehaviorPolicy).map((p) => ({
      value: `policy:${p.policy.id}`,
      label: `${t('Policy')}: ${p.policy.name}`,
      hint: t('All day'),
    })),
    ...props.pools.map((p) => ({
      value: `pool:${p.pool.id}`,
      label: `${t('Strategy pool')}: ${p.pool.name}`,
      hint: t('Controlled by pool schedule and probability'),
    })),
  ]
  for (const value of values.targets ?? []) {
    if (!options.some((o) => o.value === value)) {
      options.push({ value, label: value, hint: t('Invalid reference') })
    }
  }
  const save = form.handleSubmit(async (data) => {
    if (!props.owner || invalidData) return
    if (data.replace && (!data.reason.trim() || !data.targets.length)) {
      form.setError('reason', {
        message: t('Replacement requires a reason and at least one target'),
      })
      return
    }
    if (
      props.legacy &&
      (!props.application || props.application.use_legacy) &&
      !data.confirm
    ) {
      form.setError('confirm', {
        message: t(
          'Review and confirm replacement of the legacy configuration'
        ),
      })
      return
    }
    try {
      const targets = data.targets.map((value) => {
        const [kind, id] = value.split(':')
        return { kind, id: Number(id) }
      }) as ApplicationTarget[]
      const users = props.bulkUsers?.length ? props.bulkUsers : [props.owner.id]
      const results: { id: number; success: boolean; message?: string }[] = []
      for (const userId of users) {
        // A retry only resubmits failures, never repeats already committed changes.
        if (outcomes.some((o) => o.id === userId && o.success)) {
          results.push({ id: userId, success: true })
          continue
        }
        const existing = props.bulkUsers?.length
          ? props.applications?.find(
              (a) => a.owner_kind === 'user' && a.owner_id === userId
            )
          : props.application
        try {
          requireServerSuccess(
            await saveApplication(props.owner.kind, userId, {
              version: existing?.version ?? 0,
              operation: data.operation,
              targets,
              mode: data.replace ? 'replace' : 'merge',
              expires_at: data.expires
                ? Math.floor(new Date(data.expires).getTime() / 1000)
                : 0,
              reason: data.reason,
              confirm_legacy: data.confirm,
            })
          )
          results.push({ id: userId, success: true })
        } catch (err) {
          results.push({
            id: userId,
            success: false,
            message: getServerErrorMessage(err),
          })
        }
      }
      setOutcomes(results)
      props.onSaved()
      if (results.every((o) => o.success)) props.onClose()
    } catch (err) {
      handleServerError(err, t('Failed to save application'))
    }
  })
  return (
    <Dialog
      open={!!props.owner}
      onOpenChange={(open) => {
        if (!open && !form.formState.isSubmitting) props.onClose()
      }}
      title={t('Applications for {{name}}', { name: props.owner?.name })}
      description={t(
        'Direct policies apply all day. Pools follow their own schedule. Removing all personal bindings restores inheritance.'
      )}
      contentClassName='sm:max-w-2xl'
      showCloseButton={false}
      footer={
        <>
          <Button
            variant='outline'
            onClick={props.onClose}
            disabled={form.formState.isSubmitting}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='submit'
            form={id}
            disabled={form.formState.isSubmitting || invalidData}
          >
            {t('Save')}
          </Button>
        </>
      }
    >
      <form
        id={id}
        onSubmit={save}
        className='min-w-0 space-y-4'
        inert={form.formState.isSubmitting}
      >
        {props.bulkUsers?.length ? (
          <>
            <Label htmlFor={`${id}-operation`}>
              {t('Batch binding operation')}
            </Label>
            <Select
              items={[
                { value: 'append', label: t('Append bindings') },
                { value: 'remove', label: t('Remove selected bindings') },
                { value: 'set', label: t('Replace personal bindings') },
              ]}
              value={values.operation}
              onValueChange={(value) =>
                form.setValue('operation', value as Form['operation'])
              }
            >
              <SelectTrigger id={`${id}-operation`}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {(['append', 'remove', 'set'] as const).map((value) => (
                  <SelectItem key={value} value={value}>
                    {t(
                      {
                        append: 'Append bindings',
                        remove: 'Remove selected bindings',
                        set: 'Replace personal bindings',
                      }[value]
                    )}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </>
        ) : null}
        <Label htmlFor={`${id}-targets`}>
          {t('Applied policies and pools')}
        </Label>
        <MultiSelect
          id={`${id}-targets`}
          options={options}
          selected={values.targets ?? []}
          onChange={(targets) => form.setValue('targets', targets)}
        />
        <p className='text-muted-foreground text-sm'>
          {t(
            'Save replaces this owner’s binding list only. Existing policy behavior is not edited. Select multiple entries to combine all-day policies and scheduled pools.'
          )}
        </p>
        {props.owner?.kind === 'user' && values.operation === 'set' && (
          <>
            <label className='flex items-center gap-2'>
              <Switch
                disabled={!props.canRelax}
                checked={values.replace}
                onCheckedChange={(value) => form.setValue('replace', value)}
              />
              {t('Replace inherited user-control settings')}
            </label>
            {values.replace && (
              <p role='status' className='text-destructive text-sm'>
                {t(
                  'This replaces list and subscription controls for this user, including when a selected pool is inactive. Account and API key permissions remain enforced.'
                )}
              </p>
            )}
            <Label htmlFor={`${id}-expires`}>
              {t('Expires at (optional)')}
            </Label>
            <Input
              id={`${id}-expires`}
              type='datetime-local'
              {...form.register('expires')}
            />
            <Label htmlFor={`${id}-reason`}>{t('Reason')}</Label>
            <Textarea id={`${id}-reason`} {...form.register('reason')} />
            {form.formState.errors.reason && (
              <p role='alert'>{form.formState.errors.reason.message}</p>
            )}
          </>
        )}
        {outcomes.length > 0 && (
          <p role='status'>
            {t('Applied to {{success}} users; {{failed}} failed.', {
              success: outcomes.filter((o) => o.success).length,
              failed: outcomes.filter((o) => !o.success).length,
            })}
          </p>
        )}
        {outcomes
          .filter((o) => !o.success)
          .map((o) => (
            <p role='alert' key={o.id}>
              #{o.id}: {o.message}
            </p>
          ))}
        {props.legacy &&
          (!props.application || props.application.use_legacy) && (
            <section className='space-y-2 rounded border p-3'>
              <h3>{t('Legacy configuration preview')}</h3>
              <pre className='max-h-60 overflow-auto text-xs break-all whitespace-pre-wrap'>
                {props.legacy}
              </pre>
              <p className='text-muted-foreground text-sm'>
                {t(
                  'The legacy configuration stays active until you confirm this switch. Its original record is retained as a backup and will not run twice.'
                )}
              </p>
              <label className='flex items-center gap-2'>
                <Switch
                  checked={values.confirm}
                  onCheckedChange={(value) => form.setValue('confirm', value)}
                />
                {t(
                  'I reviewed the legacy configuration and confirm the switch'
                )}
              </label>
              {form.formState.errors.confirm && (
                <p role='alert'>{form.formState.errors.confirm.message}</p>
              )}
            </section>
          )}
        {props.application &&
          !props.application.use_legacy &&
          props.legacy &&
          props.canRestore &&
          !props.bulkUsers?.length && (
            <Button
              type='button'
              variant='outline'
              onClick={() => setRestoreOpen(true)}
            >
              {t('Restore legacy backup')}
            </Button>
          )}
      </form>
      <ConfirmDialog
        open={restoreOpen}
        onOpenChange={setRestoreOpen}
        title={t('Restore legacy backup')}
        desc={t(
          'This removes the new bindings and restores the retained legacy configuration. Review the backup before continuing.'
        )}
        destructive
        isLoading={restoring}
        handleConfirm={() => {
          if (!props.owner || !props.application) return
          setRestoring(true)
          void restoreLegacyApplication(
            props.owner.kind,
            props.owner.id,
            props.application.version
          )
            .then(requireServerSuccess)
            .then(() => {
              setRestoreOpen(false)
              props.onSaved()
              props.onClose()
            })
            .catch((err: unknown) =>
              handleServerError(err, t('Failed to restore legacy backup'))
            )
            .finally(() => setRestoring(false))
        }}
      >
        <pre className='max-h-60 overflow-auto text-xs break-all whitespace-pre-wrap'>
          {props.legacy}
        </pre>
      </ConfirmDialog>
    </Dialog>
  )
}
