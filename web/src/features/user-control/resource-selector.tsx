import { useContext, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { MultiSelect, type Option } from '@/components/multi-select'
import { Button } from '@/components/ui/button'

import { ResourceCatalogContext, useResourceCatalog } from './resource-catalog'

export function ResourceCatalogProvider(props: { children: ReactNode }) {
  const { t } = useTranslation()
  const query = useResourceCatalog()
  return (
    <ResourceCatalogContext.Provider
      value={query.data ?? { channels: [], models: [] }}
    >
      {query.isError && (
        <Button variant='outline' onClick={() => void query.refetch()}>
          {t('Reload resource catalog')}
        </Button>
      )}
      {props.children}
    </ResourceCatalogContext.Provider>
  )
}

export function ResourceSelector(props: {
  kind: 'model' | 'channel' | 'group' | 'channel-type'
  id?: string
  value: string[]
  onChange: (value: string[]) => void
  placeholder?: string
  single?: boolean
}) {
  const { t } = useTranslation()
  const catalog = useContext(ResourceCatalogContext)
  const channels = catalog.channels
  let options: Option[] = []
  if (props.kind === 'channel') {
    options = channels.map((c) => ({
      value: String(c.id),
      label: `${c.name} #${c.id}`,
      hint: c.status === 1 ? `${t('Channel type')}: ${c.type}` : t('Disabled'),
    }))
  } else {
    let values: string[]
    if (props.kind === 'channel-type') {
      values = channels.map((c) => String(c.type))
    } else if (props.kind === 'group') {
      values = channels.flatMap((c) => c.group.split(','))
    } else {
      values = [
        ...catalog.models,
        ...channels.flatMap((c) => c.models.split(',')),
      ]
    }
    options = [...new Set(values.map((v) => v.trim()).filter(Boolean))]
      .sort()
      .map((value) => ({ value, label: value }))
  }
  for (const selected of props.value) {
    if (!options.some((o) => o.value === selected)) {
      options.push({
        value: selected,
        label: selected,
        hint: t('Not in current resource catalog; reference preserved'),
      })
    }
  }
  return (
    <MultiSelect
      id={props.id}
      options={options}
      selected={props.value}
      allowCreate={props.kind === 'model'}
      placeholder={props.placeholder}
      maxVisibleChips={6}
      onChange={(values) =>
        props.onChange(props.single ? values.slice(-1) : values)
      }
    />
  )
}
