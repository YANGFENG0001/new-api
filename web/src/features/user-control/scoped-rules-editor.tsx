import { useId, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion'
import { Button } from '@/components/ui/button'
import {
  Field,
  FieldError,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'

import { ActionsEditor } from './actions-editor'
import { ResourceSelector } from './resource-selector'
import type { Limits, ScopedRule } from './types'

export function ScopedRulesEditor(props: {
  value: ScopedRule[]
  onChange: (value: ScopedRule[]) => void
  renderLimits: (value: Limits, onChange: (value: Limits) => void) => ReactNode
}) {
  const { t } = useTranslation()
  const id = useId()
  const update = (index: number, patch: Partial<ScopedRule>) =>
    props.onChange(
      props.value.map((r, i) => (i === index ? { ...r, ...patch } : r))
    )
  return (
    <section
      className='min-w-0 space-y-3'
      aria-label={t('Model and channel rules')}
    >
      <h4 className='text-sm font-medium'>{t('Model and channel rules')}</h4>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Each rule has its own per-user quota bucket. Filters match the original model and actual channel together. Add the policy to a pool for scheduled control. Multiple models in one rule share its quota.'
        )}
      </p>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Scoped rules require HTTP JSON requests. Usage caps are checked before admission, so concurrent requests may exceed them. Higher-priority actions win within each routing stage.'
        )}
      </p>
      {props.value.map((rule, index) => (
        <section
          key={rule.id}
          aria-label={rule.name || `${t('Scoped rule')} ${index + 1}`}
          className='min-w-0 space-y-3 rounded-lg border p-3'
        >
          <FieldGroup className='gap-3'>
            <div className='flex flex-wrap items-end gap-3'>
              <Field className='min-w-0 flex-1'>
                <FieldLabel htmlFor={`${id}-${rule.id}-name`}>
                  {t('Rule name')}
                </FieldLabel>
                <Input
                  id={`${id}-${rule.id}-name`}
                  value={rule.name ?? ''}
                  maxLength={128}
                  onChange={(e) => update(index, { name: e.target.value })}
                />
              </Field>
              <Field className='w-24'>
                <FieldLabel htmlFor={`${id}-${rule.id}-priority`}>
                  {t('Priority')}
                </FieldLabel>
                <Input
                  id={`${id}-${rule.id}-priority`}
                  type='number'
                  value={rule.priority ?? 0}
                  onChange={(e) =>
                    update(index, { priority: Number(e.target.value) })
                  }
                />
              </Field>
              <Field orientation='horizontal' className='w-auto'>
                <Switch
                  id={`${id}-${rule.id}-enabled`}
                  checked={rule.enabled !== false}
                  onCheckedChange={(enabled) => update(index, { enabled })}
                />
                <FieldLabel htmlFor={`${id}-${rule.id}-enabled`}>
                  {t('Enabled')}
                </FieldLabel>
              </Field>
              <Button
                type='button'
                variant='outline'
                onClick={() =>
                  props.onChange(props.value.filter((_, i) => i !== index))
                }
              >
                {t('Remove scoped rule')}
              </Button>
            </div>
            <div className='grid min-w-0 gap-3 sm:grid-cols-2'>
              <Field>
                <FieldLabel htmlFor={`${id}-${rule.id}-models`}>
                  {t('Match original models')}
                </FieldLabel>
                <ResourceSelector
                  kind='model'
                  id={`${id}-${rule.id}-models`}
                  value={rule.models ?? []}
                  placeholder={t('Empty = all models')}
                  onChange={(models) => update(index, { models })}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor={`${id}-${rule.id}-channels`}>
                  {t('Match channel IDs')}
                </FieldLabel>
                <ResourceSelector
                  kind='channel'
                  id={`${id}-${rule.id}-channels`}
                  value={rule.channel_ids?.map(String) ?? []}
                  placeholder={t('Empty = all channels')}
                  onChange={(values) =>
                    update(index, {
                      // Invalid input must be rejected, never dropped into an
                      // empty filter that would silently match every channel.
                      channel_ids: values.map((value) => {
                        const n = Number(value)
                        return Number.isSafeInteger(n) && n > 0 ? n : 0
                      }),
                    })
                  }
                />
                {rule.channel_ids?.some((n) => n <= 0) && (
                  <FieldError id={`${id}-${rule.id}-channels-error`}>
                    {t('scoped channel IDs must be positive')}
                  </FieldError>
                )}
              </Field>
            </div>
          </FieldGroup>
          <Accordion multiple defaultValue={['limits']}>
            <AccordionItem value='limits'>
              <AccordionTrigger>{t('Scoped limits')}</AccordionTrigger>
              <AccordionContent>
                {props.renderLimits(rule.limits, (limits) =>
                  update(index, { limits })
                )}
              </AccordionContent>
            </AccordionItem>
            <AccordionItem value='actions'>
              <AccordionTrigger>
                {t('Request and response actions')}
              </AccordionTrigger>
              <AccordionContent>
                <ActionsEditor
                  value={rule.actions}
                  onChange={(actions) => update(index, { actions })}
                />
              </AccordionContent>
            </AccordionItem>
          </Accordion>
        </section>
      ))}
      <Button
        type='button'
        variant='outline'
        disabled={props.value.length >= 64}
        onClick={() =>
          props.onChange([
            ...props.value,
            { id: crypto.randomUUID(), enabled: true, limits: {} },
          ])
        }
      >
        {t('Add scoped rule')}
      </Button>
    </section>
  )
}
