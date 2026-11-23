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
import { useTranslation } from 'react-i18next'

import { Combobox } from '@/components/ui/combobox'
import { Label } from '@/components/ui/label'

import type { SyncAccount, SyncSource } from './sync-api'

// Shared metadata-only selector; selecting an account never contacts upstream.
export function SyncAccountPicker(props: {
  id: string
  sources: SyncSource[]
  accounts: SyncAccount[]
  sourceId: number
  accountId: string
  sourceDisabled?: boolean
  accountDisabled?: boolean
  onSourceChange: (id: number) => void
  onAccountChange: (id: string) => void
}) {
  const { t } = useTranslation()
  return (
    <div className='grid gap-4 sm:grid-cols-2'>
      <div className='space-y-2'>
        <Label htmlFor={`${props.id}-source`}>
          {t('Upstream synchronization source')}
        </Label>
        <Combobox
          id={`${props.id}-source`}
          disabled={props.sourceDisabled}
          value={props.sourceId ? String(props.sourceId) : ''}
          options={props.sources.map((source) => ({
            value: String(source.id),
            label: source.name,
          }))}
          onValueChange={(value) => props.onSourceChange(Number(value))}
        />
      </div>
      <div className='space-y-2'>
        <Label htmlFor={`${props.id}-account`}>{t('Upstream account')}</Label>
        <Combobox
          id={`${props.id}-account`}
          disabled={props.accountDisabled || !props.sourceId}
          value={props.accountId}
          options={props.accounts.map((account) => ({
            value: account.id,
            label: `${account.name || account.id} · ${account.id}`,
          }))}
          onValueChange={(value) => props.onAccountChange(value ?? '')}
        />
      </div>
    </div>
  )
}
