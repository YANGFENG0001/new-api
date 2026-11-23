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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import {
  SecureVerificationDialog,
  useSecureVerification,
} from '@/features/auth/secure-verification'

import {
  syncAPI,
  syncSourceSchema,
  syncStaleSeconds,
  emptySyncSource,
  type SyncSource,
  type SyncMapping,
} from './sync-api'
import {
  SyncAuthenticationSettings,
  SyncAuthenticationActions,
} from './sync-authentication'
import { SyncMappingEditor } from './sync-mapping-editor'
import {
  syncSourceFormSchema,
  sourceRequest,
  type SyncSourceForm,
} from './sync-source-schema'

export function SyncSourceEditor(props: {
  source?: SyncSource
  template: SyncMapping
  canEdit: boolean
  privileged: boolean
  onClose: () => void
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const verification = useSecureVerification()
  const [authSource, setAuthSource] = useState(props.source)
  const form = useForm<SyncSourceForm>({
    resolver: zodResolver(syncSourceFormSchema),
    defaultValues: {
      ...(props.source ?? emptySyncSource(props.template)),
      replace_request: false,
      auth_type: 'none',
      token: '',
      auth_header: 'X-API-Key',
      username: '',
      password: '',
      refresh_token: '',
      client_id: '',
      client_secret: '',
      headers_json: '{}',
      query_json: '{}',
      body: '',
    },
  })
  const values = form.watch()
  const save = useMutation({
    retry: false,
    mutationFn: async (data: SyncSourceForm) => {
      const proof = await verification.requestVerification({
        scope: 'subscription.sync.credentials',
        context: {
          source_id: data.id,
          action: 'save',
          version: data.version,
          credential_revision: data.credential_revision,
        },
      })
      if (!proof) return null
      return syncAPI.save(
        syncSourceSchema.parse(data),
        sourceRequest(data),
        proof.proof_token
      )
    },
    onSuccess: (result) => {
      if (!result) return
      void client.invalidateQueries({ queryKey: ['subscription-sync'] })
      props.onClose()
    },
  })
  return (
    <form
      className='mx-auto w-full max-w-5xl space-y-4'
      onSubmit={form.handleSubmit((data) => save.mutate(data))}
    >
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <h3>{t('Synchronization connection')}</h3>
        <Button type='button' variant='outline' onClick={props.onClose}>
          {t('Back')}
        </Button>
      </div>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Read cached usage only. This connector never enables upstream probing.'
        )}
      </p>
      <fieldset
        disabled={!props.canEdit || save.isPending}
        className='space-y-4'
      >
        <div className='grid gap-3 sm:grid-cols-2'>
          <div className='space-y-1.5'>
            <Label htmlFor='sync-name'>{t('Name')}</Label>
            <Input id='sync-name' {...form.register('name')} />
          </div>
          <div className='space-y-1.5'>
            <Label htmlFor='sync-url'>{t('API URL')}</Label>
            <Input
              id='sync-url'
              placeholder='https://example.com/api/panel/usage'
              {...form.register('url')}
              disabled={!props.privileged && !!props.source}
            />
          </div>
          <div className='space-y-1.5'>
            <Label htmlFor='sync-method'>{t('Request method')}</Label>
            <Combobox
              id='sync-method'
              value={values.method}
              disabled={!props.privileged && !!props.source}
              options={[
                { value: 'GET', label: 'GET' },
                { value: 'POST', label: 'POST' },
              ]}
              onValueChange={(v) => {
                if (v === 'GET' || v === 'POST') {
                  form.setValue('method', v, { shouldDirty: true })
                }
              }}
            />
          </div>
          {(
            [
              ['interval_seconds', t('Polling interval (seconds)'), 10, 86400],
              ['stale_seconds', t('Stale after (seconds)'), 10, 604800],
              ['timeout_seconds', t('Request timeout (seconds)'), 1, 60],
            ] as const
          ).map(([key, label, min, max]) => (
            <div key={key} className='space-y-1.5'>
              <Label htmlFor={`sync-${key}`}>{label}</Label>
              <Input
                id={`sync-${key}`}
                type='number'
                min={min}
                max={max}
                {...form.register(key, { valueAsNumber: true })}
              />
            </div>
          ))}
        </div>
        <p className='text-muted-foreground text-xs'>
          {t(
            'Poll every {{interval}}s; observations expire after {{threshold}}s.',
            {
              interval: values.interval_seconds,
              threshold: syncStaleSeconds(values),
            }
          )}
        </p>
        {values.method === 'POST' && (
          <p role='note' className='text-destructive text-sm'>
            {t(
              'Use POST only for a documented read-only query. It may have upstream side effects.'
            )}
          </p>
        )}
        <Label className='flex items-center gap-2'>
          <Switch
            checked={values.enabled}
            onCheckedChange={(v) =>
              form.setValue('enabled', v, { shouldDirty: true })
            }
          />
          {t('Automatic synchronization')}
        </Label>
        <details className='space-y-3 rounded-lg border p-3'>
          <summary className='cursor-pointer text-sm font-medium select-none'>
            {t('Authentication and renewal')}
          </summary>
          <SyncAuthenticationSettings
            form={form}
            privileged={props.privileged}
            existing={!!props.source}
          />
          <p className='text-muted-foreground text-xs'>
            {t(
              'Saved secrets are write-only. Leave replacement off to preserve existing request settings. Set SUBSCRIPTION_SYNC_SECRET_KEY before storing credentials.'
            )}
          </p>
          <Label className='flex items-center gap-2'>
            <Switch
              checked={values.replace_request}
              onCheckedChange={(v) => form.setValue('replace_request', v)}
            />
            {t('Replace saved request settings')}
          </Label>
          {values.replace_request && (
            <div className='space-y-3'>
              <Label htmlFor='sync-auth'>{t('Authentication')}</Label>
              <Combobox
                id='sync-auth'
                disabled={values.refresh_mode !== 'manual'}
                value={values.auth_type}
                options={[
                  { value: 'none', label: t('None') },
                  { value: 'bearer', label: 'Bearer' },
                  { value: 'header', label: t('Custom header') },
                  { value: 'basic', label: 'Basic' },
                ]}
                onValueChange={(v) => {
                  if (
                    v === 'none' ||
                    v === 'bearer' ||
                    v === 'header' ||
                    v === 'basic'
                  ) {
                    form.setValue('auth_type', v)
                  }
                }}
              />
              {values.auth_type === 'header' && (
                <div>
                  <Label htmlFor='sync-auth-header'>{t('Header name')}</Label>
                  <Input
                    id='sync-auth-header'
                    {...form.register('auth_header')}
                  />
                </div>
              )}
              {(values.auth_type === 'bearer' ||
                values.auth_type === 'header') && (
                <div>
                  <Label htmlFor='sync-token'>{t('API key')}</Label>
                  <Input
                    id='sync-token'
                    type='password'
                    autoComplete='new-password'
                    {...form.register('token')}
                  />
                </div>
              )}
              {(values.auth_type === 'basic' ||
                values.refresh_mode === 'login') && (
                <>
                  <Label htmlFor='sync-username'>{t('Username')}</Label>
                  <Input
                    id='sync-username'
                    autoComplete='off'
                    {...form.register('username')}
                  />
                  <Label htmlFor='sync-password'>{t('Password')}</Label>
                  <Input
                    id='sync-password'
                    type='password'
                    autoComplete='new-password'
                    {...form.register('password')}
                  />
                </>
              )}
              <Label htmlFor='sync-headers'>
                {t('Request headers (JSON)')}
              </Label>
              <Textarea
                id='sync-headers'
                placeholder='{"X-API-Key":"..."}'
                {...form.register('headers_json')}
              />
              <Label htmlFor='sync-query'>{t('Query parameters (JSON)')}</Label>
              <Textarea
                id='sync-query'
                placeholder='{}'
                {...form.register('query_json')}
              />
              {values.method === 'POST' && (
                <>
                  <Label htmlFor='sync-body'>{t('Request body (JSON)')}</Label>
                  <Textarea
                    id='sync-body'
                    placeholder='{"account_id":"..."}'
                    {...form.register('body')}
                  />
                </>
              )}
            </div>
          )}
        </details>
        <details className='space-y-3 rounded-lg border p-3'>
          <summary>{t('Destination safety')}</summary>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Only super administrators may authorize HTTP or exact private IP addresses. Metadata destinations and redirects remain blocked.'
            )}
          </p>
          <Label>
            <Switch
              disabled={!props.privileged}
              checked={values.allow_http}
              onCheckedChange={(v) =>
                form.setValue('allow_http', v, { shouldDirty: true })
              }
            />
            {t('Allow HTTP for this source')}
          </Label>
          <Label htmlFor='sync-private'>
            {t('Allowed private IP addresses')}
          </Label>
          <Textarea
            id='sync-private'
            disabled={!props.privileged}
            placeholder='127.0.0.1'
            {...form.register('private_ips')}
          />
        </details>
        <details className='space-y-3 rounded-lg border p-3'>
          <summary>{t('Response field mapping')}</summary>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Paths use dot-separated keys. Percentages and estimates never overwrite local billing.'
            )}
          </p>
          <SyncMappingEditor form={form} />
        </details>
        {Object.keys(form.formState.errors).length > 0 && (
          <p role='alert' className='text-destructive'>
            {t(
              'Check connection fields, JSON and cycle mappings before saving.'
            )}
          </p>
        )}
        <Button type='submit' disabled={save.isPending}>
          {save.isPending ? t('Saving...') : t('Save')}
        </Button>
      </fieldset>
      {authSource && (
        <SyncAuthenticationActions
          source={authSource}
          disabled={!props.canEdit || save.isPending || form.formState.isDirty}
          onChanged={(next) => {
            setAuthSource({ ...authSource, ...next })
            form.setValue('credential_revision', next.credential_revision)
          }}
        />
      )}
      <SecureVerificationDialog {...verification.dialogProps} />
    </form>
  )
}
