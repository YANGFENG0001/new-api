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
import { Loader2, Plus, RefreshCw, Trash2, Users } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState, useId } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog as FormDialog } from '@/components/dialog'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
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
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { getAdminPlans } from '@/features/subscriptions/api'
import {
  createControlList,
  deleteControlList,
  deletePlanBinding,
  deletePolicy,
  deleteUserOverride,
  getCapabilities,
  listAudit,
  listControlLists,
  listOverrides,
  listPlanBindings,
  listPolicies,
  putPlanBinding,
  updateControlList,
} from '@/features/user-control/api'
import { LimitsSummary } from '@/features/user-control/limits-summary'
import { MembersDialog } from '@/features/user-control/members-dialog'
import { OverrideDialog } from '@/features/user-control/override-dialog'
import { PolicyEditorDialog } from '@/features/user-control/policy-editor'
import { RuntimeAuditDetails } from '@/features/user-control/runtime-audit-details'
import { Simulator } from '@/features/user-control/simulator'
import type {
  AuditLogItem,
  Capabilities,
  ControlList,
  PlanBinding,
  PolicyView,
  UserOverride,
} from '@/features/user-control/types'
import { UserPicker } from '@/features/user-control/user-picker'
import { toIntlLocale } from '@/i18n/languages'
import { handleServerError } from '@/lib/handle-server-error'

function ListFormDialog({
  open,
  list,
  policies,
  onClose,
  onSaved,
}: {
  open: boolean
  list: ControlList | null
  policies: PolicyView[]
  onClose: () => void
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const formId = useId()
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [policyId, setPolicyId] = useState(0)
  const [enabled, setEnabled] = useState(true)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!open) return
    setName(list?.name ?? '')
    setDescription(list?.description ?? '')
    setPolicyId(list?.policy_id ?? 0)
    setEnabled(list?.enabled ?? true)
  }, [open, list])

  const handleSave = async () => {
    if (!name.trim()) {
      toast.error(t('List name is required'))
      return
    }
    setSaving(true)
    try {
      const payload = {
        name: name.trim(),
        description,
        policy_id: policyId,
        enabled,
      }
      const result = list
        ? await updateControlList(list.id, payload)
        : await createControlList(payload)
      if (!result.success) {
        handleServerError(result, t('Failed to save list'))
        return
      }
      onSaved()
      onClose()
    } catch (err) {
      handleServerError(err, t('Failed to save list'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <FormDialog
      open={open}
      onOpenChange={(value) => {
        if (!value && !saving) onClose()
      }}
      title={list ? t('Edit control list') : t('New control list')}
      contentClassName='sm:max-w-lg'
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
        className='min-w-0 space-y-3'
        inert={saving}
        onSubmit={(event) => {
          event.preventDefault()
          void handleSave()
        }}
      >
        <div className='space-y-1'>
          <Label htmlFor={`${formId}-name`}>{t('Name')}</Label>
          <Input
            id={`${formId}-name`}
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </div>
        <div className='space-y-1'>
          <Label htmlFor={`${formId}-description`}>{t('Description')}</Label>
          <Textarea
            id={`${formId}-description`}
            rows={2}
            value={description}
            onChange={(event) => setDescription(event.target.value)}
          />
        </div>
        <div className='space-y-1'>
          <Label htmlFor={`${formId}-policy`}>{t('Policy template')}</Label>
          <Select
            items={[
              { value: '0', label: t('None') },
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
                  .name ?? t('None')}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                <SelectItem value='0'>{t('None')}</SelectItem>
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
        <label className='flex items-center gap-2 text-sm'>
          <Switch checked={enabled} onCheckedChange={setEnabled} />
          {t('Enabled')}
        </label>
      </form>
    </FormDialog>
  )
}

export function UserControlSection() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [activeTab, setActiveTab] = useState('policies')
  const [capabilities, setCapabilities] = useState<Capabilities | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [policies, setPolicies] = useState<PolicyView[]>([])
  const [lists, setLists] = useState<ControlList[]>([])
  const [overrides, setOverrides] = useState<UserOverride[]>([])
  const [bindings, setBindings] = useState<PlanBinding[]>([])
  const [plans, setPlans] = useState<{ id: number; title: string }[]>([])
  const [audit, setAudit] = useState<AuditLogItem[]>([])
  const [auditTotal, setAuditTotal] = useState(0)
  const [auditPage, setAuditPage] = useState(1)

  const [policyDialogOpen, setPolicyDialogOpen] = useState(false)
  const [editingPolicy, setEditingPolicy] = useState<PolicyView | null>(null)
  const [listDialogOpen, setListDialogOpen] = useState(false)
  const [editingList, setEditingList] = useState<ControlList | null>(null)
  const [membersList, setMembersList] = useState<ControlList | null>(null)
  const [overrideUserId, setOverrideUserId] = useState<number | null>(null)
  const [editingOverride, setEditingOverride] = useState<UserOverride | null>(
    null
  )
  const [overridePickerOpen, setOverridePickerOpen] = useState(false)
  const [overrideSelection, setOverrideSelection] = useState<number[]>([])
  const loadAll = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const caps = await getCapabilities()
      if (!caps.success || !caps.data) {
        setError(
          caps.message ||
            t('You do not have permission to manage user controls.')
        )
        setCapabilities(null)
        return
      }
      setCapabilities(caps.data)
      const [policyRes, listRes, overrideRes, bindingRes, planRes] =
        await Promise.all([
          listPolicies(),
          listControlLists(),
          listOverrides(),
          listPlanBindings(),
          getAdminPlans().catch(() => null),
        ])
      if (policyRes.success && policyRes.data) setPolicies(policyRes.data.items)
      if (listRes.success && listRes.data) setLists(listRes.data.items)
      if (overrideRes.success && overrideRes.data) {
        setOverrides(overrideRes.data.items)
      }
      if (bindingRes.success && bindingRes.data) {
        setBindings(bindingRes.data.items)
      }
      if (planRes?.success && planRes.data) {
        setPlans(
          planRes.data.map((item) => ({
            id: item.plan.id,
            title: item.plan.title,
          }))
        )
      }
    } catch (err) {
      handleServerError(err, t('Failed to load user control settings'))
    } finally {
      setLoading(false)
    }
  }, [t])

  const loadAudit = useCallback(
    async (page = 1) => {
      try {
        const result = await listAudit({ p: page, page_size: 20 })
        if (result.success && result.data) {
          setAudit(result.data.items)
          setAuditTotal(result.data.total)
          setAuditPage(result.data.page)
        }
      } catch (err) {
        handleServerError(err, t('Failed to load audit log'))
      }
    },
    [t]
  )

  useEffect(() => {
    void loadAll()
  }, [loadAll])

  useEffect(() => {
    if (capabilities?.audit_read) void loadAudit(1)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [capabilities?.audit_read])

  const policyName = useMemo(() => {
    const map = new Map<number, string>()
    for (const item of policies) map.set(item.policy.id, item.policy.name)
    return map
  }, [policies])

  const handleDeletePolicy = async (policy: PolicyView) => {
    if (
      !window.confirm(
        t('Delete policy "{{name}}"?', { name: policy.policy.name })
      )
    ) {
      return
    }
    try {
      const result = await deletePolicy(policy.policy.id)
      if (result.success) {
        toast.success(t('Policy deleted'))
        await loadAll()
      } else {
        handleServerError(result, t('Failed to delete policy'))
      }
    } catch (err) {
      handleServerError(err, t('Failed to delete policy'))
    }
  }

  const handleDeleteList = async (list: ControlList) => {
    if (
      !window.confirm(t('Delete control list "{{name}}"?', { name: list.name }))
    ) {
      return
    }
    try {
      const result = await deleteControlList(list.id)
      if (result.success) {
        toast.success(t('List deleted'))
        await loadAll()
      } else {
        handleServerError(result, t('Failed to delete list'))
      }
    } catch (err) {
      handleServerError(err, t('Failed to delete list'))
    }
  }

  const handleDeleteOverride = async (override: UserOverride) => {
    if (
      !window.confirm(
        t('Remove the override for user #{{id}}?', { id: override.user_id })
      )
    ) {
      return
    }
    try {
      const result = await deleteUserOverride(override.user_id)
      if (result.success) {
        toast.success(t('Override removed'))
        await loadAll()
      } else {
        handleServerError(result, t('Failed to remove override'))
      }
    } catch (err) {
      handleServerError(err, t('Failed to remove override'))
    }
  }

  const handleBind = async (planId: number, policyId: number) => {
    try {
      if (policyId === 0) {
        const result = await deletePlanBinding(planId)
        if (!result.success) {
          handleServerError(result, t('Failed to unbind policy'))
          return
        }
      } else {
        const result = await putPlanBinding(planId, policyId)
        if (!result.success) {
          handleServerError(result, t('Failed to bind policy'))
          return
        }
      }
      toast.success(t('Plan binding updated'))
      await loadAll()
    } catch (err) {
      handleServerError(err, t('Failed to update plan binding'))
    }
  }

  if (loading) {
    return (
      <div className='flex min-h-40 items-center justify-center'>
        <Loader2 className='size-6 animate-spin' />
      </div>
    )
  }

  if (!capabilities || error) {
    return (
      <Alert variant='destructive'>
        <AlertTitle>{t('Access denied')}</AlertTitle>
        <AlertDescription>
          {error || t('You do not have permission to manage user controls.')}
        </AlertDescription>
      </Alert>
    )
  }

  return (
    <div className='space-y-4'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div>
          <h2 className='text-lg font-semibold'>{t('User control')}</h2>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Apply rate, quota, request, model, channel, API key and time-window limits to users, lists and subscription plans.'
            )}
          </p>
        </div>
        <Button variant='outline' size='sm' onClick={() => void loadAll()}>
          <RefreshCw className='size-4' />
          {t('Refresh')}
        </Button>
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList className='flex-wrap'>
          <TabsTrigger value='policies'>{t('Policies')}</TabsTrigger>
          <TabsTrigger value='pools'>{t('Strategy pools')}</TabsTrigger>
          <TabsTrigger value='lists'>{t('Lists')}</TabsTrigger>
          <TabsTrigger value='overrides'>{t('User overrides')}</TabsTrigger>
          <TabsTrigger value='plans'>{t('Subscription plans')}</TabsTrigger>
          <TabsTrigger value='simulator'>{t('Simulator')}</TabsTrigger>
          {capabilities.audit_read && (
            <TabsTrigger value='audit'>{t('Audit')}</TabsTrigger>
          )}
        </TabsList>

        <TabsContent
          value={activeTab === 'pools' ? 'pools' : 'policies'}
          className='space-y-3'
        >
          <div className='flex justify-end'>
            {capabilities.policy_write && (
              <Button
                size='sm'
                onClick={() => {
                  setEditingPolicy(null)
                  setPolicyDialogOpen(true)
                }}
              >
                <Plus className='size-4' />
                {activeTab === 'pools'
                  ? t('New strategy pool')
                  : t('New policy')}
              </Button>
            )}
          </div>
          <div className='grid gap-3 md:grid-cols-2'>
            {policies
              .filter(
                (item) =>
                  Boolean(item.limits.random_pool) === (activeTab === 'pools')
              )
              .map((item) => (
                <div
                  key={item.policy.id}
                  className='space-y-2 rounded-lg border p-3'
                >
                  <div className='flex flex-wrap items-center gap-2'>
                    <span className='font-medium'>{item.policy.name}</span>
                    <Badge
                      variant={item.policy.enabled ? 'secondary' : 'outline'}
                    >
                      {item.policy.enabled ? t('Enabled') : t('Disabled')}
                    </Badge>
                    <Badge variant='outline'>
                      {t('Priority')} {item.policy.priority}
                    </Badge>
                    <div className='ml-auto flex gap-1'>
                      {capabilities.policy_write && (
                        <>
                          <Button
                            size='sm'
                            variant='ghost'
                            onClick={() => {
                              setEditingPolicy(item)
                              setPolicyDialogOpen(true)
                            }}
                          >
                            {t('Edit')}
                          </Button>
                          <Button
                            size='icon'
                            variant='ghost'
                            className='text-destructive size-8'
                            onClick={() => void handleDeletePolicy(item)}
                            aria-label={t('Delete')}
                          >
                            <Trash2 className='size-4' />
                          </Button>
                        </>
                      )}
                    </div>
                  </div>
                  {item.policy.description && (
                    <p className='text-muted-foreground text-sm'>
                      {item.policy.description}
                    </p>
                  )}
                  <p className='text-muted-foreground text-xs'>
                    {t('Base limits')}:{' '}
                    <LimitsSummary value={item.limits} compact />
                  </p>
                  <p className='text-muted-foreground text-xs'>
                    {t('Time windows')}: {item.schedule?.rules?.length ?? 0} ·{' '}
                    {item.schedule?.timezone ?? capabilities.default_timezone}
                  </p>
                </div>
              ))}
            {policies.length === 0 && (
              <p className='text-muted-foreground text-sm'>
                {t('No policies yet')}
              </p>
            )}
          </div>
        </TabsContent>

        <TabsContent value='lists' className='space-y-3'>
          <div className='flex justify-end'>
            {capabilities.list_write && (
              <Button
                size='sm'
                onClick={() => {
                  setEditingList(null)
                  setListDialogOpen(true)
                }}
              >
                <Plus className='size-4' />
                {t('New list')}
              </Button>
            )}
          </div>
          <div className='grid gap-3 md:grid-cols-2'>
            {lists.map((list) => (
              <div key={list.id} className='space-y-2 rounded-lg border p-3'>
                <div className='flex flex-wrap items-center gap-2'>
                  <span className='font-medium'>{list.name}</span>
                  <Badge variant={list.enabled ? 'secondary' : 'outline'}>
                    {list.enabled ? t('Enabled') : t('Disabled')}
                  </Badge>
                  <Badge variant='outline'>
                    {t('{{count}} members', { count: list.members })}
                  </Badge>
                  <div className='ml-auto flex gap-1'>
                    <Button
                      size='sm'
                      variant='ghost'
                      onClick={() => setMembersList(list)}
                    >
                      <Users className='size-4' />
                      {t('Members')}
                    </Button>
                    {capabilities.list_write && (
                      <>
                        <Button
                          size='sm'
                          variant='ghost'
                          onClick={() => {
                            setEditingList(list)
                            setListDialogOpen(true)
                          }}
                        >
                          {t('Edit')}
                        </Button>
                        <Button
                          size='icon'
                          variant='ghost'
                          className='text-destructive size-8'
                          onClick={() => void handleDeleteList(list)}
                          aria-label={t('Delete')}
                        >
                          <Trash2 className='size-4' />
                        </Button>
                      </>
                    )}
                  </div>
                </div>
                <p className='text-muted-foreground text-xs'>
                  {t('Policy')}: {policyName.get(list.policy_id) ?? t('None')}
                </p>
              </div>
            ))}
            {lists.length === 0 && (
              <p className='text-muted-foreground text-sm'>
                {t('No lists yet')}
              </p>
            )}
          </div>
        </TabsContent>

        <TabsContent value='overrides' className='space-y-3'>
          <div className='flex flex-wrap items-end justify-end gap-2'>
            {capabilities.override_write && (
              <Button
                size='sm'
                onClick={() => {
                  setOverrideSelection([])
                  setOverridePickerOpen(true)
                }}
              >
                <Plus className='size-4' />
                {t('Override for user')}
              </Button>
            )}
          </div>
          <div className='min-w-0 overflow-x-auto rounded-md border'>
            <table className='w-full text-sm'>
              <thead className='bg-muted/50 text-left'>
                <tr>
                  <th className='p-2'>{t('User')}</th>
                  <th className='p-2'>{t('Policy')}</th>
                  <th className='p-2'>{t('Mode')}</th>
                  <th className='p-2'>{t('Limits')}</th>
                  <th className='p-2'>{t('Expires')}</th>
                  <th className='p-2' />
                </tr>
              </thead>
              <tbody>
                {overrides.map((item) => (
                  <tr key={item.id} className='border-t'>
                    <td className='p-2'>
                      {item.username || '-'} #{item.user_id}
                    </td>
                    <td className='p-2'>
                      {policyName.get(item.policy_id) ?? t('None')}
                    </td>
                    <td className='p-2'>
                      {item.mode === 'replace'
                        ? t('Replace (relax inherited limits)')
                        : t('Merge (tighten only)')}
                    </td>
                    <td className='text-muted-foreground p-2'>
                      <LimitsSummary value={item.limits_view} compact />
                    </td>
                    <td className='p-2'>
                      {item.expires_at
                        ? new Date(item.expires_at * 1000).toLocaleString(
                            locale
                          )
                        : t('Never')}
                    </td>
                    <td className='p-2 text-right'>
                      {capabilities.override_write && (
                        <>
                          <Button
                            size='sm'
                            variant='ghost'
                            onClick={() => {
                              setEditingOverride(item)
                              setOverrideUserId(item.user_id)
                            }}
                          >
                            {t('Edit')}
                          </Button>
                          <Button
                            size='icon'
                            variant='ghost'
                            className='text-destructive size-8'
                            onClick={() => void handleDeleteOverride(item)}
                            aria-label={t('Delete')}
                          >
                            <Trash2 className='size-4' />
                          </Button>
                        </>
                      )}
                    </td>
                  </tr>
                ))}
                {overrides.length === 0 && (
                  <tr>
                    <td
                      colSpan={6}
                      className='text-muted-foreground p-4 text-center'
                    >
                      {t('No user overrides')}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </TabsContent>

        <TabsContent value='plans' className='space-y-3'>
          <p className='text-muted-foreground text-sm'>
            {t('Assign a control policy to every active subscriber of a plan.')}
          </p>
          <div className='min-w-0 overflow-x-auto rounded-md border'>
            <table className='w-full text-sm'>
              <thead className='bg-muted/50 text-left'>
                <tr>
                  <th className='p-2'>{t('Plan')}</th>
                  <th className='p-2'>{t('Policy')}</th>
                </tr>
              </thead>
              <tbody>
                {plans.map((plan) => {
                  const binding = bindings.find(
                    (item) => item.plan_id === plan.id
                  )
                  return (
                    <tr key={plan.id} className='border-t'>
                      <td className='p-2'>
                        {plan.title} #{plan.id}
                      </td>
                      <td className='p-2'>
                        <Select
                          items={[
                            { value: '0', label: t('None') },
                            ...policies.map((item) => ({
                              value: String(item.policy.id),
                              label: item.policy.name,
                            })),
                          ]}
                          value={String(binding?.policy_id ?? 0)}
                          onValueChange={(value) =>
                            void handleBind(plan.id, Number(value))
                          }
                          disabled={!capabilities.policy_write}
                        >
                          <SelectTrigger
                            aria-label={t('Policy for plan {{name}}', {
                              name: plan.title,
                            })}
                            className='w-full max-w-64 min-w-40'
                          >
                            <SelectValue>
                              {policyName.get(binding?.policy_id ?? 0) ??
                                t('None')}
                            </SelectValue>
                          </SelectTrigger>
                          <SelectContent>
                            <SelectGroup>
                              <SelectItem value='0'>{t('None')}</SelectItem>
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
                      </td>
                    </tr>
                  )
                })}
                {plans.length === 0 && (
                  <tr>
                    <td
                      colSpan={2}
                      className='text-muted-foreground p-4 text-center'
                    >
                      {t('No subscription plans')}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </TabsContent>

        <TabsContent value='simulator' className='min-w-0 space-y-3'>
          <Simulator />
        </TabsContent>

        {capabilities.audit_read && (
          <TabsContent value='audit' className='space-y-3'>
            <div className='min-w-0 overflow-x-auto rounded-md border'>
              <table className='w-full text-sm'>
                <thead className='bg-muted/50 text-left'>
                  <tr>
                    <th className='p-2'>{t('Time')}</th>
                    <th className='p-2'>{t('Actor')}</th>
                    <th className='p-2'>{t('Action')}</th>
                    <th className='p-2'>{t('Result')}</th>
                    <th className='p-2'>{t('Details')}</th>
                  </tr>
                </thead>
                <tbody>
                  {audit.map((item, index) => (
                    <tr key={item.event_id ?? index} className='border-t'>
                      <td className='p-2'>
                        {item.created_at
                          ? new Date(item.created_at * 1000).toLocaleString(
                              locale
                            )
                          : '-'}
                      </td>
                      <td className='p-2'>
                        {item.username || item.user_id || '-'}
                      </td>
                      <td className='p-2'>
                        {item.action ? t(item.action) : '-'}
                      </td>
                      <td className='p-2'>
                        {item.success === false ? t('Failed') : t('Success')}
                      </td>
                      <td className='max-w-md p-2'>
                        <RuntimeAuditDetails item={item} />
                      </td>
                    </tr>
                  ))}
                  {audit.length === 0 && (
                    <tr>
                      <td
                        colSpan={5}
                        className='text-muted-foreground p-4 text-center'
                      >
                        {t('No audit entries')}
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
            <div className='flex items-center justify-between'>
              <Button
                size='sm'
                variant='outline'
                disabled={auditPage <= 1}
                onClick={() => void loadAudit(auditPage - 1)}
              >
                {t('Previous')}
              </Button>
              <span className='text-muted-foreground text-sm'>
                {auditPage} / {Math.max(1, Math.ceil(auditTotal / 20))}
              </span>
              <Button
                size='sm'
                variant='outline'
                disabled={auditPage >= Math.ceil(auditTotal / 20)}
                onClick={() => void loadAudit(auditPage + 1)}
              >
                {t('Next')}
              </Button>
            </div>
          </TabsContent>
        )}
      </Tabs>

      <FormDialog
        open={overridePickerOpen}
        onOpenChange={setOverridePickerOpen}
        title={t('Select a user for override')}
        showCloseButton={false}
        footer={
          <>
            <Button
              type='button'
              variant='outline'
              onClick={() => setOverridePickerOpen(false)}
            >
              {t('Cancel')}
            </Button>
            <Button
              type='button'
              disabled={overrideSelection.length !== 1}
              onClick={() => {
                const id = overrideSelection[0]
                if (!id) return
                setEditingOverride(
                  overrides.find((item) => item.user_id === id) ?? null
                )
                setOverrideUserId(id)
                setOverridePickerOpen(false)
              }}
            >
              {t('Continue')}
            </Button>
          </>
        }
      >
        {overridePickerOpen && (
          <UserPicker
            selected={overrideSelection}
            onChange={setOverrideSelection}
          />
        )}
      </FormDialog>
      <PolicyEditorDialog
        open={policyDialogOpen}
        policy={editingPolicy}
        policies={policies}
        createPool={activeTab === 'pools'}
        defaultTimezone={capabilities.default_timezone}
        onOpenChange={setPolicyDialogOpen}
        onSaved={() => void loadAll()}
      />
      <ListFormDialog
        open={listDialogOpen}
        list={editingList}
        policies={policies}
        onClose={() => setListDialogOpen(false)}
        onSaved={() => void loadAll()}
      />
      <MembersDialog
        list={membersList}
        canWrite={capabilities.list_write}
        onClose={() => {
          setMembersList(null)
          void loadAll()
        }}
      />
      <OverrideDialog
        userId={overrideUserId}
        existing={editingOverride}
        policies={policies}
        defaultTimezone={capabilities.default_timezone}
        canRelax={capabilities.override_relax}
        onClose={() => {
          setOverrideUserId(null)
          setEditingOverride(null)
          setOverrideSelection([])
        }}
        onSaved={() => void loadAll()}
      />
    </div>
  )
}
