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
import { useQuery } from '@tanstack/react-query'
import { Info } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'

import { SubscriptionsDialogs } from './components/subscriptions-dialogs'
import { SubscriptionsPrimaryButtons } from './components/subscriptions-primary-buttons'
import {
  SubscriptionsProvider,
  useSubscriptions,
} from './components/subscriptions-provider'
import { SubscriptionsTable } from './components/subscriptions-table'
import { poolAPI } from './pools/api'
import { PoolPanel } from './pools/pool-panel'
import { SyncSourcesPanel } from './pools/sync-sources-panel'

function SubscriptionsContent() {
  const { t } = useTranslation()
  const { complianceConfirmed } = useSubscriptions()
  const caps = useQuery({
    queryKey: ['subscription-pools', 'capabilities'],
    queryFn: poolAPI.capabilities,
  })

  return (
    <>
      <SectionPageLayout fixedContent>
        <SectionPageLayout.Title>
          {t('Subscription Management')}
        </SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <div className='flex items-center gap-2'>
            <Alert variant='default' className='hidden px-3 py-2 sm:flex'>
              <Info className='h-4 w-4' />
              <AlertDescription className='text-xs'>
                {t(
                  'Stripe/Creem requires creating products on the third-party platform and entering the ID'
                )}
              </AlertDescription>
            </Alert>
            <SubscriptionsPrimaryButtons />
          </div>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='flex h-full min-h-0 flex-col gap-4'>
            {!complianceConfirmed ? (
              <Alert variant='destructive' className='shrink-0'>
                <AlertDescription>
                  {t(
                    'Subscription plan creation and changes are locked until the administrator confirms compliance terms in Payment Gateway settings.'
                  )}
                </AlertDescription>
              </Alert>
            ) : null}
            <Tabs defaultValue='plans' className='min-h-0 flex-1'>
              <TabsList className='max-w-full overflow-x-auto'>
                <TabsTrigger value='plans'>
                  {t('Subscription Plans')}
                </TabsTrigger>
                {caps.data?.sync_read && (
                  <TabsTrigger value='sync'>
                    {t('Upstream synchronization sources')}
                  </TabsTrigger>
                )}
                {caps.data?.read && (
                  <TabsTrigger value='pools'>
                    {t('Upstream carpool pools')}
                  </TabsTrigger>
                )}
              </TabsList>
              {caps.data?.sync_read && (
                <TabsContent value='sync' className='min-h-0 flex-1'>
                  <SyncSourcesPanel caps={caps.data} />
                </TabsContent>
              )}
              <TabsContent value='plans' className='min-h-0 flex-1'>
                <SubscriptionsTable />
              </TabsContent>
              {caps.data?.read && (
                <TabsContent value='pools' className='min-h-0 flex-1'>
                  <PoolPanel caps={caps.data} />
                </TabsContent>
              )}
            </Tabs>
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <SubscriptionsDialogs />
    </>
  )
}

export function Subscriptions() {
  return (
    <SubscriptionsProvider>
      <SubscriptionsContent />
    </SubscriptionsProvider>
  )
}
