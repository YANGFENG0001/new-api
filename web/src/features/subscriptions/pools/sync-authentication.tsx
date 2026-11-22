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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import type { UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import {
  SecureVerificationDialog,
  useSecureVerification,
} from '@/features/auth/secure-verification'
import { toIntlLocale } from '@/i18n/languages'
import { getServerErrorMessage } from '@/lib/server-error-message'

import { syncAPI, type SyncSource } from './sync-api'
import type { SyncSourceForm } from './sync-source-schema'

export function SyncAuthenticationSettings(props: {
  form: UseFormReturn<SyncSourceForm>
  privileged: boolean
  existing: boolean
}) {
  const { t } = useTranslation()
  const values = props.form.watch()
  return (
    <div className='space-y-3'>
      <Label htmlFor='sync-refresh-mode'>{t('Token renewal mode')}</Label>
      <Combobox
        id='sync-refresh-mode'
        value={values.refresh_mode}
        disabled={!props.privileged && props.existing}
        options={[
          { value: 'manual', label: t('Manual token') },
          { value: 'login', label: t('Account login renewal') },
          { value: 'oauth', label: t('Standard OAuth refresh') },
        ]}
        onValueChange={(mode) => {
          if (mode !== 'manual' && mode !== 'login' && mode !== 'oauth') return
          props.form.setValue('refresh_mode', mode, { shouldDirty: true })
          props.form.setValue('replace_request', true, { shouldDirty: true })
          if (mode !== 'manual') props.form.setValue('auth_type', 'bearer')
          else {
            props.form.setValue('auto_refresh', false)
            props.form.setValue('refresh_url', '')
          }
        }}
      />
      {values.refresh_mode !== 'manual' && (
        <>
          <Label htmlFor='sync-refresh-url'>{t('Login or refresh URL')}</Label>
          <Input
            id='sync-refresh-url'
            {...props.form.register('refresh_url')}
            disabled={!props.privileged && props.existing}
            placeholder='https://example.com/api/panel/login'
          />
          <p className='text-muted-foreground text-xs'>
            {t(
              'Renewal requires HTTPS or an authorized loopback tunnel, the same origin, and a read-only GET usage source.'
            )}
          </p>
          <Label>
            <Switch
              checked={values.auto_refresh}
              onCheckedChange={(v) =>
                props.form.setValue('auto_refresh', v, { shouldDirty: true })
              }
            />
            {t('Automatic token renewal')}
          </Label>
          <Label htmlFor='sync-refresh-before'>
            {t('Renew before expiry (seconds)')}
          </Label>
          <Input
            id='sync-refresh-before'
            type='number'
            min={1}
            max={604800}
            {...props.form.register('refresh_before', { valueAsNumber: true })}
          />
          <p className='text-muted-foreground text-xs'>
            {t(
              'Renewal runs during normal synchronization. Unknown expiry is managed upstream; a 401 may trigger one renewal, never a 403 or 429.'
            )}
          </p>
          {values.replace_request && values.refresh_mode === 'oauth' && (
            <>
              <p className='text-muted-foreground text-xs'>
                {t(
                  'Use only a documented OAuth token endpoint: refresh_token grant with client_secret_post. vm2api panel sessions do not have this endpoint.'
                )}
              </p>
              <Label htmlFor='sync-refresh-token'>{t('Refresh token')}</Label>
              <Input
                id='sync-refresh-token'
                type='password'
                autoComplete='new-password'
                {...props.form.register('refresh_token')}
              />
              <Label htmlFor='sync-client-id'>{t('Client ID')}</Label>
              <Input
                id='sync-client-id'
                autoComplete='off'
                {...props.form.register('client_id')}
              />
              <Label htmlFor='sync-client-secret'>{t('Client secret')}</Label>
              <Input
                id='sync-client-secret'
                type='password'
                autoComplete='new-password'
                {...props.form.register('client_secret')}
              />
            </>
          )}
        </>
      )}
    </div>
  )
}

type AuthAction = 'check' | 'refresh' | 'replace'
export function SyncAuthenticationActions(props: {
  source: SyncSource
  disabled: boolean
  onChanged: (source: SyncSource) => void
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const client = useQueryClient()
  const verification = useSecureVerification()
  const [action, setAction] = useState<AuthAction | null>(null)
  const [replacement, setReplacement] = useState('')
  const mutation = useMutation({
    retry: false,
    mutationFn: async (next: AuthAction) => {
      const proof = await verification.requestVerification({
        scope: 'subscription.sync.credentials',
        context: {
          source_id: props.source.id,
          action: next,
          version: props.source.version,
          credential_revision: props.source.credential_revision,
        },
      })
      if (!proof) return null
      return syncAPI.authenticate(
        props.source,
        next,
        replacement,
        proof.proof_token
      )
    },
    onSuccess: (source) => {
      if (!source) return
      setReplacement('')
      setAction(null)
      props.onChanged(source)
      void client.invalidateQueries({ queryKey: ['subscription-sync'] })
    },
  })
  const disabled = props.disabled || mutation.isPending
  return (
    <section
      className='space-y-3 rounded-lg border p-3'
      aria-label={t('Token status')}
    >
      <h4>{t('Token status')}</h4>
      <dl className='grid gap-2 text-sm sm:grid-cols-2'>
        {(
          [
            ['Token expiry', props.source.token_expires_at],
            ['Last authentication success', props.source.last_auth_success],
            ['Last token renewal', props.source.last_token_refresh],
            ['Next renewal attempt', props.source.auth_next_attempt],
          ] as const
        ).map(([key, time]) => (
          <div key={key} className='min-w-0 break-words'>
            <dt className='text-muted-foreground'>{t(key)}</dt>
            <dd>
              {time
                ? new Date(time * 1000).toLocaleString(locale)
                : t(key === 'Token expiry' ? 'Managed upstream' : 'Never')}
            </dd>
          </div>
        ))}
      </dl>
      {props.source.auth_error && (
        <p role='status' className='text-destructive'>
          {t(props.source.auth_error)}
        </p>
      )}
      {props.source.refresh_blocked && (
        <p role='alert'>
          {t(
            'Refresh token may have been consumed. Replace saved request settings with a new refresh token before retrying.'
          )}
        </p>
      )}
      <p className='text-muted-foreground text-xs'>
        {t(
          'Save pending edits before authentication actions. Checking may extend an upstream sliding session but does not reissue a token or infer its expiry.'
        )}
      </p>
      <div className='flex flex-wrap gap-2'>
        <Button
          type='button'
          variant='outline'
          disabled={disabled}
          onClick={() => {
            mutation.reset()
            setAction('check')
          }}
        >
          {t('Check authentication')}
        </Button>
        <Button
          type='button'
          variant='outline'
          disabled={
            disabled ||
            !props.source.refresh_mode ||
            props.source.refresh_mode === 'manual' ||
            props.source.refresh_blocked
          }
          onClick={() => {
            mutation.reset()
            setAction('refresh')
          }}
        >
          {t('Renew token now')}
        </Button>
        <Button
          type='button'
          variant='outline'
          disabled={disabled}
          onClick={() => {
            mutation.reset()
            setAction('replace')
          }}
        >
          {t('Replace token')}
        </Button>
      </div>
      <ConfirmDialog
        open={action !== null && !verification.isActive}
        onOpenChange={(open) => {
          if (!open) {
            setAction(null)
            setReplacement('')
          }
        }}
        title={t('Confirm authentication action')}
        desc={t(
          'This contacts only the configured upstream interface. New tokens are saved only after validation; old tokens are not automatically revoked.'
        )}
        isLoading={mutation.isPending}
        disabled={disabled || (action === 'replace' && !replacement.trim())}
        handleConfirm={() => {
          if (action) mutation.mutate(action)
        }}
      >
        {mutation.isError && (
          <ErrorState
            className='min-h-0'
            description={getServerErrorMessage(
              mutation.error,
              t('Authentication renewal failed')
            )}
          />
        )}
        {action === 'replace' && (
          <div className='space-y-2'>
            <Label htmlFor='sync-replacement'>{t('New token')}</Label>
            <Input
              id='sync-replacement'
              type='password'
              autoComplete='new-password'
              value={replacement}
              onChange={(e) => setReplacement(e.target.value)}
            />
          </div>
        )}
      </ConfirmDialog>
      <SecureVerificationDialog {...verification.dialogProps} />
    </section>
  )
}
