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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, Plus, RefreshCw, Trash2, Users } from 'lucide-react'
import { useEffect, useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog as FormDialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import {
  createControlList,
  updateControlList,
  deleteControlList,
  deletePolicy,
  deletePool,
  getCapabilities,
  listPolicies,
  listPools,
  listApplications,
  listControlLists,
  listOverrides,
  listPlanBindings,
  listAudit,
} from '@/features/user-control/api'
import { ApplicationEditor } from '@/features/user-control/application-editor'
import { parseTargets } from '@/features/user-control/application-targets'
import { LimitsSummary } from '@/features/user-control/limits-summary'
import { MembersDialog } from '@/features/user-control/members-dialog'
import { PolicyEditorDialog } from '@/features/user-control/policy-editor'
import { useResourceCatalog } from '@/features/user-control/resource-catalog'
import { ResourceCatalogProvider } from '@/features/user-control/resource-selector'
import { RuntimeAuditDetails } from '@/features/user-control/runtime-audit-details'
import { ScheduledPoolEditor } from '@/features/user-control/scheduled-pool-editor'
import { Simulator } from '@/features/user-control/simulator'
import {
  isBehaviorPolicy,
  type ControlList,
  type OwnerKind,
  type PolicyView,
  type PoolView,
} from '@/features/user-control/types'
import { UserPicker } from '@/features/user-control/user-picker'
import { toIntlLocale } from '@/i18n/languages'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

function ListFormDialog({
  open,
  list,
  onClose,
  onSaved,
}: {
  open: boolean
  list: ControlList | null
  onClose: () => void
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const formId = useId()
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [enabled, setEnabled] = useState(true)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!open) return
    setName(list?.name ?? '')
    setDescription(list?.description ?? '')
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
        policy_id: list?.policy_id ?? 0,
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
  const client = useQueryClient()
  const [activeTab, setActiveTab] = useState('policies')
  const [policyOpen, setPolicyOpen] = useState(false)
  const [editingPolicy, setEditingPolicy] = useState<PolicyView | null>(null)
  const [poolOpen, setPoolOpen] = useState(false)
  const [editingPool, setEditingPool] = useState<PoolView | null>(null)
  const [listOpen, setListOpen] = useState(false)
  const [editingList, setEditingList] = useState<ControlList | null>(null)
  const [membersList, setMembersList] = useState<ControlList | null>(null)
  const [owner, setOwner] = useState<{
    kind: OwnerKind
    id: number
    name: string
  } | null>(null)
  const [bulkUsers, setBulkUsers] = useState<number[]>([])
  const [pickerOpen, setPickerOpen] = useState(false)
  const [selected, setSelected] = useState<number[]>([])
  const [deleting, setDeleting] = useState<{
    kind: 'policy' | 'pool' | 'list'
    id: number
    name: string
  } | null>(null)
  const [auditPage, setAuditPage] = useState(1)
  const capabilities = useQuery({
    queryKey: ['user-control', 'capabilities'],
    queryFn: async () => requireServerSuccess(await getCapabilities()).data,
  })
  const caps = capabilities.data
  const catalog = useResourceCatalog(!!caps?.read)
  const settings = useQuery({
    queryKey: ['user-control', 'settings'],
    enabled: !!caps?.read,
    queryFn: async () => {
      const [policies, pools, applications, lists, overrides, bindings] =
        await Promise.all([
          listPolicies(),
          listPools(),
          listApplications(),
          listControlLists(),
          listOverrides(),
          listPlanBindings(),
        ])
      return {
        policies: requireServerSuccess(policies).data?.items ?? [],
        pools: requireServerSuccess(pools).data?.items ?? [],
        applications: requireServerSuccess(applications).data?.items ?? [],
        lists: requireServerSuccess(lists).data?.items ?? [],
        overrides: requireServerSuccess(overrides).data?.items ?? [],
        bindings: requireServerSuccess(bindings).data?.items ?? [],
      }
    },
  })
  const audit = useQuery({
    queryKey: ['user-control', 'audit', auditPage],
    enabled: !!caps?.audit_read && activeTab === 'audit',
    queryFn: async () =>
      requireServerSuccess(await listAudit({ p: auditPage, page_size: 20 }))
        .data,
  })
  const reload = () =>
    void client.invalidateQueries({ queryKey: ['user-control'] })
  const removal = useMutation({
    mutationFn: async () => {
      if (!deleting) return
      const fn = {
        policy: deletePolicy,
        pool: deletePool,
        list: deleteControlList,
      }[deleting.kind]
      requireServerSuccess(await fn(deleting.id))
    },
    onSuccess: () => {
      setDeleting(null)
      reload()
    },
  })
  if (capabilities.isPending || (!!caps?.read && settings.isPending)) {
    return <LoadingState />
  }
  if (capabilities.isError || settings.isError) {
    return <ErrorState onRetry={reload} />
  }
  if (!caps?.read || !settings.data) {
    return (
      <p role='alert'>
        {t('You do not have permission to manage user controls.')}
      </p>
    )
  }
  const data = settings.data
  const appFor = (kind: OwnerKind, id: number) =>
    data.applications.find((a) => a.owner_kind === kind && a.owner_id === id)
  const names = new Map(data.policies.map((p) => [p.policy.id, p.policy.name]))
  const targetSummary = (kind: OwnerKind, id: number) => {
    const app = appFor(kind, id)
    const empty =
      kind === 'user'
        ? t('No personal binding; inherit other controls')
        : t('None')
    if (!app || app.use_legacy) {
      let hasLegacy = false
      if (kind === 'user') {
        hasLegacy = data.overrides.some((o) => o.user_id === id)
      } else if (kind === 'list') {
        hasLegacy = data.lists.some((l) => l.id === id && l.policy_id > 0)
      } else hasLegacy = data.bindings.some((b) => b.plan_id === id)
      return hasLegacy ? t('Legacy configuration') : empty
    }
    return (
      parseTargets(app.targets)
        .map((target) =>
          target.kind === 'policy'
            ? `${t('Policy')}: ${names.get(target.id) ?? `#${target.id}`} (${t('All day')})`
            : `${t('Strategy pool')}: ${data.pools.find((p) => p.pool.id === target.id)?.pool.name ?? `#${target.id}`}`
        )
        .join(' · ') || empty
    )
  }
  const legacyFor = () => {
    if (!owner) return undefined
    let legacy: unknown
    if (owner.kind === 'user') {
      legacy = data.overrides.find((o) => o.user_id === owner.id)
    }
    if (owner.kind === 'list') {
      const list = data.lists.find((l) => l.id === owner.id)
      legacy = data.policies.find((p) => p.policy.id === list?.policy_id)
    }
    if (owner.kind === 'plan') {
      const binding = data.bindings.find((b) => b.plan_id === owner.id)
      legacy = data.policies.find((p) => p.policy.id === binding?.policy_id)
    }
    return legacy ? JSON.stringify(legacy, null, 2) : undefined
  }
  const userIDs = [
    ...new Set([
      ...data.overrides.map((o) => o.user_id),
      ...data.applications
        .filter((a) => a.owner_kind === 'user')
        .map((a) => a.owner_id),
    ]),
  ]
  return (
    <ResourceCatalogProvider>
      <div className='min-w-0 space-y-4'>
        <header className='flex flex-wrap items-center justify-between gap-2'>
          <div>
            <h2 className='text-lg font-semibold'>{t('User control')}</h2>
            <p className='text-muted-foreground text-sm'>
              {t(
                'Policies define behavior, pools define scheduling, applications define who is controlled.'
              )}
            </p>
          </div>
          <Button variant='outline' onClick={reload}>
            <RefreshCw className='size-4' />
            {t('Refresh')}
          </Button>
        </header>
        <Tabs value={activeTab} onValueChange={setActiveTab}>
          <TabsList className='h-auto flex-wrap'>
            <TabsTrigger value='policies'>{t('Policies')}</TabsTrigger>
            <TabsTrigger value='pools'>{t('Strategy pools')}</TabsTrigger>
            <TabsTrigger value='lists'>{t('Lists')}</TabsTrigger>
            <TabsTrigger value='users'>{t('Personal settings')}</TabsTrigger>
            <TabsTrigger value='plans'>{t('Subscription plans')}</TabsTrigger>
            <TabsTrigger value='legacy'>
              {t('Legacy configuration')}
            </TabsTrigger>
            <TabsTrigger value='simulator'>{t('Simulator')}</TabsTrigger>
            {caps.audit_read && (
              <TabsTrigger value='audit'>{t('Audit')}</TabsTrigger>
            )}
          </TabsList>
          <TabsContent value='policies' className='space-y-3'>
            {caps.policy_write && (
              <Button
                onClick={() => {
                  setEditingPolicy(null)
                  setPolicyOpen(true)
                }}
              >
                <Plus className='size-4' />
                {t('New policy')}
              </Button>
            )}
            <div className='grid gap-3 md:grid-cols-2'>
              {data.policies.filter(isBehaviorPolicy).map((p) => (
                <section
                  key={p.policy.id}
                  className='min-w-0 space-y-2 rounded border p-3'
                >
                  <div className='flex flex-wrap items-center gap-2'>
                    <h3 className='font-medium'>{p.policy.name}</h3>
                    <Badge variant='outline'>
                      {p.policy.enabled ? t('Enabled') : t('Disabled')}
                    </Badge>
                    <Badge variant='outline'>
                      {t('Priority')} {p.policy.priority}
                    </Badge>
                    {caps.policy_write && (
                      <>
                        <Button
                          variant='ghost'
                          onClick={() => {
                            setEditingPolicy(p)
                            setPolicyOpen(true)
                          }}
                        >
                          {t('Edit')}
                        </Button>
                        <Button
                          variant='ghost'
                          aria-label={t('Delete')}
                          onClick={() =>
                            setDeleting({
                              kind: 'policy',
                              id: p.policy.id,
                              name: p.policy.name,
                            })
                          }
                        >
                          <Trash2 className='size-4' />
                        </Button>
                      </>
                    )}
                  </div>
                  <LimitsSummary value={p.limits} />
                </section>
              ))}
            </div>
          </TabsContent>
          <TabsContent value='pools' className='space-y-3'>
            {caps.policy_write && (
              <Button
                onClick={() => {
                  setEditingPool(null)
                  setPoolOpen(true)
                }}
              >
                <Plus className='size-4' />
                {t('New strategy pool')}
              </Button>
            )}
            <div className='grid gap-3 md:grid-cols-2'>
              {data.pools.map((p) => (
                <section
                  key={p.pool.id}
                  className='min-w-0 space-y-2 rounded border p-3'
                >
                  <h3 className='font-medium'>{p.pool.name}</h3>
                  <Badge variant='outline'>
                    {p.pool.enabled ? t('Enabled') : t('Disabled')}
                  </Badge>
                  <p>
                    {t('Pool activation probability (%)')}:{' '}
                    {p.config.probability / 100}
                  </p>
                  {p.config.members.map((m) => (
                    <p key={m.policy_id} className='text-sm'>
                      {names.get(m.policy_id) ?? `#${m.policy_id}`}:{' '}
                      {m.probability / 100}%
                    </p>
                  ))}
                  <p className='text-muted-foreground text-sm'>
                    {p.config.schedule
                      ? t('Controlled by pool schedule and probability')
                      : t('All day')}
                  </p>
                  {caps.policy_write && (
                    <div className='flex gap-2'>
                      <Button
                        variant='outline'
                        onClick={() => {
                          setEditingPool(p)
                          setPoolOpen(true)
                        }}
                      >
                        {t('Edit')}
                      </Button>
                      <Button
                        variant='ghost'
                        aria-label={t('Delete')}
                        onClick={() =>
                          setDeleting({
                            kind: 'pool',
                            id: p.pool.id,
                            name: p.pool.name,
                          })
                        }
                      >
                        <Trash2 className='size-4' />
                      </Button>
                    </div>
                  )}
                </section>
              ))}
            </div>
          </TabsContent>
          <TabsContent value='lists' className='space-y-3'>
            {caps.list_write && (
              <Button
                onClick={() => {
                  setEditingList(null)
                  setListOpen(true)
                }}
              >
                <Plus className='size-4' />
                {t('New list')}
              </Button>
            )}
            <div className='grid gap-3 md:grid-cols-2'>
              {data.lists.map((list) => (
                <section
                  key={list.id}
                  className='min-w-0 space-y-2 rounded border p-3'
                >
                  <h3 className='font-medium'>{list.name}</h3>
                  <Badge variant='outline'>
                    {t('{{count}} members', { count: list.members })}
                  </Badge>
                  <p className='text-muted-foreground text-sm'>
                    {targetSummary('list', list.id)}
                  </p>
                  <div className='flex flex-wrap gap-2'>
                    <Button
                      variant='outline'
                      onClick={() => setMembersList(list)}
                    >
                      <Users className='size-4' />
                      {t('Members')}
                    </Button>
                    {caps.list_write && (
                      <>
                        <Button
                          variant='outline'
                          onClick={() =>
                            setOwner({
                              kind: 'list',
                              id: list.id,
                              name: list.name,
                            })
                          }
                        >
                          {t('Manage applications')}
                        </Button>
                        <Button
                          variant='ghost'
                          onClick={() => {
                            setEditingList(list)
                            setListOpen(true)
                          }}
                        >
                          {t('Edit')}
                        </Button>
                        <Button
                          variant='ghost'
                          aria-label={t('Delete')}
                          onClick={() =>
                            setDeleting({
                              kind: 'list',
                              id: list.id,
                              name: list.name,
                            })
                          }
                        >
                          <Trash2 className='size-4' />
                        </Button>
                      </>
                    )}
                  </div>
                </section>
              ))}
            </div>
          </TabsContent>
          <TabsContent value='users' className='space-y-3'>
            {caps.override_write && (
              <Button
                onClick={() => {
                  setSelected([])
                  setPickerOpen(true)
                }}
              >
                {t('Select users')}
              </Button>
            )}
            {userIDs.map((id) => (
              <section key={id} className='space-y-2 rounded border p-3'>
                <h3>
                  {data.overrides.find((o) => o.user_id === id)?.username ??
                    t('User')}{' '}
                  #{id}
                </h3>
                <p>{targetSummary('user', id)}</p>
                {caps.override_write && (
                  <Button
                    variant='outline'
                    onClick={() =>
                      setOwner({
                        kind: 'user',
                        id,
                        name: `${t('User')} #${id}`,
                      })
                    }
                  >
                    {t('Manage applications')}
                  </Button>
                )}
              </section>
            ))}
          </TabsContent>
          <TabsContent value='plans' className='space-y-3'>
            <p>
              {t(
                'Assign policies or pools to active subscribers. Subscription billing is unchanged.'
              )}
            </p>
            {(catalog.data?.plans ?? []).map((plan) => (
              <section key={plan.id} className='space-y-2 rounded border p-3'>
                <h3>{plan.title}</h3>
                <p>{targetSummary('plan', plan.id)}</p>
                {caps.policy_write && (
                  <Button
                    variant='outline'
                    onClick={() =>
                      setOwner({ kind: 'plan', id: plan.id, name: plan.title })
                    }
                  >
                    {t('Manage applications')}
                  </Button>
                )}
              </section>
            ))}
          </TabsContent>
          <TabsContent value='legacy' className='space-y-3'>
            <p>
              {t(
                'Legacy scheduled policies and pools keep their original behavior. Review bindings before switching; no automatic destructive migration is performed.'
              )}
            </p>
            {data.policies
              .filter((p) => !isBehaviorPolicy(p))
              .map((p) => (
                <section
                  key={p.policy.id}
                  className='min-w-0 space-y-2 rounded border p-3'
                >
                  <h3>{p.policy.name}</h3>
                  <pre className='max-h-64 overflow-auto text-xs break-all whitespace-pre-wrap'>
                    {JSON.stringify(p, null, 2)}
                  </pre>
                  {caps.policy_write && (
                    <Button
                      variant='outline'
                      onClick={() => {
                        setEditingPolicy(null)
                        setPolicyOpen(true)
                      }}
                    >
                      {t('Create a behavior-only replacement')}
                    </Button>
                  )}
                </section>
              ))}
          </TabsContent>
          <TabsContent value='simulator'>
            <Simulator />
          </TabsContent>
          {caps.audit_read && (
            <TabsContent value='audit' className='space-y-3'>
              {audit.isPending && <LoadingState />}
              {audit.isError && (
                <ErrorState onRetry={() => void audit.refetch()} />
              )}
              {audit.data?.items.map((item, index) => (
                <section
                  key={item.event_id ?? index}
                  className='space-y-1 rounded border p-3'
                >
                  <p className='text-muted-foreground text-xs'>
                    {item.created_at
                      ? new Date(item.created_at * 1000).toLocaleString(locale)
                      : ''}{' '}
                    · {item.action ? t(item.action) : ''}
                  </p>
                  <RuntimeAuditDetails item={item} />
                </section>
              ))}
              <div className='flex justify-between'>
                <Button
                  variant='outline'
                  disabled={auditPage <= 1}
                  onClick={() => setAuditPage(auditPage - 1)}
                >
                  {t('Previous')}
                </Button>
                <Button
                  variant='outline'
                  disabled={auditPage * 20 >= (audit.data?.total ?? 0)}
                  onClick={() => setAuditPage(auditPage + 1)}
                >
                  {t('Next')}
                </Button>
              </div>
            </TabsContent>
          )}
        </Tabs>
        <PolicyEditorDialog
          open={policyOpen}
          policy={editingPolicy}
          defaultTimezone={caps.default_timezone}
          onOpenChange={setPolicyOpen}
          onSaved={reload}
        />
        <ScheduledPoolEditor
          open={poolOpen}
          pool={editingPool}
          policies={data.policies}
          defaultTimezone={caps.default_timezone}
          onOpenChange={setPoolOpen}
          onSaved={reload}
        />
        <ListFormDialog
          open={listOpen}
          list={editingList}
          onClose={() => setListOpen(false)}
          onSaved={reload}
        />
        <MembersDialog
          list={membersList}
          canWrite={caps.list_write}
          onClose={() => {
            setMembersList(null)
            reload()
          }}
        />
        <ApplicationEditor
          owner={owner}
          bulkUsers={bulkUsers}
          applications={data.applications}
          application={
            owner && !bulkUsers.length
              ? appFor(owner.kind, owner.id)
              : undefined
          }
          legacy={bulkUsers.length ? undefined : legacyFor()}
          policies={data.policies}
          pools={data.pools}
          canRestore={caps.root}
          canRelax={caps.override_relax}
          onClose={() => {
            setOwner(null)
            setBulkUsers([])
          }}
          onSaved={reload}
        />
        <FormDialog
          open={pickerOpen}
          onOpenChange={setPickerOpen}
          title={t('Select users')}
          showCloseButton={false}
          footer={
            <Button
              disabled={selected.length === 0}
              onClick={() => {
                const id = selected[0]
                setBulkUsers(selected.length > 1 ? selected : [])
                setOwner({ kind: 'user', id, name: `${t('User')} #${id}` })
                setPickerOpen(false)
              }}
            >
              {t('Continue')}
            </Button>
          }
        >
          {pickerOpen && (
            <UserPicker selected={selected} onChange={setSelected} />
          )}
        </FormDialog>
        <ConfirmDialog
          open={!!deleting}
          onOpenChange={(open) => {
            if (!open) setDeleting(null)
          }}
          title={t('Delete')}
          desc={t(
            'Delete {{name}}? Referenced policies and pools cannot be deleted.',
            { name: deleting?.name }
          )}
          destructive
          isLoading={removal.isPending}
          handleConfirm={() => removal.mutate()}
        />
      </div>
    </ResourceCatalogProvider>
  )
}
