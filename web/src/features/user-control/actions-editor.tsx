import { useId } from 'react'
import { useTranslation } from 'react-i18next'

import { JsonCodeEditor } from '@/components/json-code-editor'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'

import { TransportEditor } from './transport-editor'
import type { ControlActions } from './types'

export function ActionsEditor(props: {
  value?: ControlActions
  onChange: (value: ControlActions) => void
}) {
  const { t } = useTranslation()
  const id = useId()
  const value = props.value ?? {}
  const set = (patch: Partial<ControlActions>) =>
    props.onChange({ ...value, ...patch })
  const setResponse = (
    patch: Partial<NonNullable<ControlActions['response']>>
  ) => {
    if (value.response) {
      set({ response: { ...value.response, ...patch } })
    }
  }
  const responseModes = [
    { value: 'off', label: t('Disabled') },
    { value: 'text', label: t('Simulated text response') },
    { value: 'json', label: t('Simulated JSON response') },
    { value: 'error', label: t('Simulated error response') },
  ]
  const requestModes = [
    { value: 'merge', label: t('Merge JSON fields') },
    { value: 'replace', label: t('Replace request body') },
  ]
  const effortOptions = [
    { value: 'keep', label: t('Keep original') },
    { value: 'none', label: t('No reasoning') },
    { value: 'minimal', label: t('Minimal reasoning') },
    { value: 'low', label: t('Low reasoning') },
    { value: 'medium', label: t('Medium reasoning') },
    { value: 'high', label: t('High reasoning') },
    { value: 'xhigh', label: t('Extra high reasoning') },
  ]
  return (
    <FieldGroup className='gap-4'>
      <TransportEditor value={value} onChange={set} />
      <Alert>
        <AlertDescription>
          {t(
            'Actions run on text JSON endpoints. Templates cannot change model, group or stream; use model routing. Rewritten requests are validated and billed normally.'
          )}
        </AlertDescription>
      </Alert>
      <div className='grid min-w-0 gap-3 sm:grid-cols-2'>
        <Field>
          <FieldLabel htmlFor={`${id}-model`}>{t('Route to model')}</FieldLabel>
          <Input
            id={`${id}-model`}
            value={value.route_model ?? ''}
            maxLength={128}
            placeholder={t('Keep original')}
            onChange={(e) => set({ route_model: e.target.value || undefined })}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor={`${id}-effort`}>
            {t('Reasoning effort override')}
          </FieldLabel>
          <Select
            items={effortOptions}
            value={value.reasoning_effort || 'keep'}
            onValueChange={(v) =>
              set({
                reasoning_effort: v === 'keep' ? undefined : (v ?? undefined),
              })
            }
          >
            <SelectTrigger id={`${id}-effort`} className='w-full'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                {effortOptions.map((o) => (
                  <SelectItem key={o.value} value={o.value}>
                    {o.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </Field>
      </div>
      <Field orientation='horizontal'>
        <Switch
          id={`${id}-rewrite`}
          checked={value.request_body !== undefined}
          onCheckedChange={(enabled) =>
            set({
              request_body: enabled ? '{\n  "temperature": 0.7\n}' : undefined,
              request_mode: enabled ? 'merge' : undefined,
            })
          }
        />
        <FieldLabel htmlFor={`${id}-rewrite`}>
          {t('Request body rewrite')}
        </FieldLabel>
      </Field>
      {value.request_body !== undefined && (
        <>
          <Field>
            <FieldLabel htmlFor={`${id}-mode`}>
              {t('Request rewrite mode')}
            </FieldLabel>
            <Select
              items={requestModes}
              value={value.request_mode ?? 'merge'}
              onValueChange={(v) =>
                set({ request_mode: v === 'replace' ? 'replace' : 'merge' })
              }
            >
              <SelectTrigger id={`${id}-mode`} className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {requestModes.map((o) => (
                    <SelectItem key={o.value} value={o.value}>
                      {o.label}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>
          <JsonCodeEditor
            value={value.request_body}
            onChange={(request_body) => set({ request_body })}
            ariaLabel={t('Request JSON template')}
            heightClassName='h-48 min-h-48 max-h-48'
          />
        </>
      )}
      <div className='grid min-w-0 gap-3 sm:grid-cols-2'>
        <Field>
          <FieldLabel htmlFor={`${id}-prefix`}>
            {t('Prepend system prompt')}
          </FieldLabel>
          <Textarea
            id={`${id}-prefix`}
            value={value.prompt_prefix ?? ''}
            maxLength={65536}
            onChange={(e) =>
              set({ prompt_prefix: e.target.value || undefined })
            }
          />
        </Field>
        <Field>
          <FieldLabel htmlFor={`${id}-suffix`}>
            {t('Append system prompt')}
          </FieldLabel>
          <Textarea
            id={`${id}-suffix`}
            value={value.prompt_suffix ?? ''}
            maxLength={65536}
            onChange={(e) =>
              set({ prompt_suffix: e.target.value || undefined })
            }
          />
        </Field>
      </div>
      <Field>
        <FieldLabel htmlFor={`${id}-response`}>
          {t('Response replacement (simulation)')}
        </FieldLabel>
        <Select
          items={responseModes}
          value={value.response?.mode ?? 'off'}
          onValueChange={(mode) => {
            if (mode === 'text' || mode === 'json' || mode === 'error') {
              set({
                response: {
                  mode,
                  body: mode === 'json' ? '{\n  "message": ""\n}' : '',
                  status: mode === 'error' ? 429 : 200,
                },
              })
            } else set({ response: undefined })
          }}
        >
          <SelectTrigger id={`${id}-response`} className='w-full'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              {responseModes.map((o) => (
                <SelectItem key={o.value} value={o.value}>
                  {o.label}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
      </Field>
      {value.response && (
        <>
          <Alert>
            <AlertDescription>
              {t(
                'Simulated responses do not call upstream or charge usage. They are marked in the response and admin audit. JSON mode requires a non-streaming request.'
              )}
            </AlertDescription>
          </Alert>
          {value.response.mode === 'json' ? (
            <JsonCodeEditor
              value={value.response.body}
              onChange={(body) => setResponse({ body })}
              ariaLabel={t('Response JSON template')}
              heightClassName='h-48 min-h-48 max-h-48'
            />
          ) : (
            <Field>
              <FieldLabel htmlFor={`${id}-body`}>
                {t('Replacement content')}
              </FieldLabel>
              <Textarea
                id={`${id}-body`}
                value={value.response.body}
                maxLength={65536}
                onChange={(e) => setResponse({ body: e.target.value })}
              />
            </Field>
          )}
          {value.response.mode === 'error' && (
            <div className='grid gap-3 sm:grid-cols-2'>
              <Field>
                <FieldLabel htmlFor={`${id}-status`}>
                  {t('Error HTTP status')}
                </FieldLabel>
                <Input
                  id={`${id}-status`}
                  type='number'
                  min={400}
                  max={599}
                  value={value.response.status ?? 429}
                  onChange={(e) =>
                    setResponse({ status: Number(e.target.value) })
                  }
                />
              </Field>
              <Field>
                <FieldLabel htmlFor={`${id}-code`}>
                  {t('Error code')}
                </FieldLabel>
                <Input
                  id={`${id}-code`}
                  value={value.response.error_code ?? ''}
                  maxLength={128}
                  onChange={(e) => setResponse({ error_code: e.target.value })}
                />
              </Field>
            </div>
          )}
        </>
      )}
    </FieldGroup>
  )
}
