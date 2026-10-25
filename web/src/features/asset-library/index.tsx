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
import { Lock } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { EmptyState } from '@/components/empty-state'
import { SectionPageLayout } from '@/components/layout'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { NotFoundError } from '@/features/errors/not-found-error'
import { useStatus } from '@/hooks/use-status'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { useAuthStore } from '@/stores/auth-store'

import { getAssetLibraryConfig } from './api'
import { AdminGroupsPanel } from './components/admin-groups-panel'
import { GroupsPanel } from './components/groups-panel'

export function AssetLibrary() {
  const { t } = useTranslation()
  const { status } = useStatus()
  const user = useAuthStore((state) => state.auth.user)
  const [tab, setTab] = useState('AIGC')
  const enabled = Boolean(status?.asset_library_enabled)
  const configQuery = useQuery({
    queryKey: ['asset-library', 'config'],
    queryFn: getAssetLibraryConfig,
    enabled,
  })
  const canViewAll = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.ASSET_LIBRARY,
    ADMIN_PERMISSION_ACTIONS.READ
  )
  const canManageAll = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.ASSET_LIBRARY,
    ADMIN_PERMISSION_ACTIONS.WRITE
  )

  // While the library is off the page does not exist.
  if (status && !enabled) return <NotFoundError />

  const config = configQuery.data
  const endpoint = `${window.location.origin}/volc?Action=CreateAsset&Version=2024-01-01`

  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>{t('Asset Library')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='flex h-full min-h-0 flex-col gap-4'>
          <div className='grid gap-3 rounded-lg border p-4 text-sm md:grid-cols-[1fr_auto]'>
            <div className='min-w-0 space-y-1.5'>
              <p className='font-medium'>
                {t('Same API as Volcengine Ark assets')}
              </p>
              <p className='text-muted-foreground'>
                {t(
                  'Call the Ark asset actions with the same Action, Version and JSON body at this address, using your API key as Bearer token. Use an Active asset in Seedance requests as asset://<asset ID>.'
                )}
              </p>
              <div className='flex min-w-0 items-center gap-1'>
                <code className='bg-muted min-w-0 rounded px-1.5 py-0.5 font-mono text-xs break-all'>
                  {endpoint}
                </code>
                <CopyButton
                  value={endpoint}
                  className='size-7'
                  iconClassName='size-3.5'
                  aria-label={t('Copy API address')}
                />
              </div>
            </div>
            {config && (
              <div className='text-muted-foreground space-y-1 text-xs md:text-right'>
                <p>
                  {t('Asset groups: {{used}} / {{limit}}', {
                    used: config.group_count,
                    limit: config.max_groups_per_user || '∞',
                  })}
                </p>
                <p>
                  {t('Assets: {{used}} / {{limit}}', {
                    used: config.asset_count,
                    limit: config.max_assets_per_user || '∞',
                  })}
                </p>
              </div>
            )}
          </div>
          {config && !config.allowed && !canViewAll && (
            <EmptyState
              icon={Lock}
              title={t('The asset library is not available to your user group')}
              bordered
            />
          )}
          {config && (config.allowed || canViewAll) && (
            <Tabs
              value={config.allowed ? tab : 'all'}
              onValueChange={setTab}
              className='flex min-h-0 flex-1 flex-col gap-3'
            >
              <TabsList className='max-w-full flex-wrap justify-start group-data-horizontal/tabs:h-auto'>
                {config.allowed && (
                  <>
                    <TabsTrigger value='AIGC'>
                      {t('Virtual avatars')}
                    </TabsTrigger>
                    <TabsTrigger value='LivenessFace'>
                      {t('Real people')}
                    </TabsTrigger>
                  </>
                )}
                {canViewAll && (
                  <TabsTrigger value='all'>{t('All users')}</TabsTrigger>
                )}
              </TabsList>
              {config.allowed && (
                <>
                  <TabsContent value='AIGC' className='min-h-0 flex-1'>
                    <GroupsPanel groupType='AIGC' config={config} />
                  </TabsContent>
                  <TabsContent value='LivenessFace' className='min-h-0 flex-1'>
                    <GroupsPanel groupType='LivenessFace' config={config} />
                  </TabsContent>
                </>
              )}
              {canViewAll && (
                <TabsContent value='all' className='min-h-0 flex-1'>
                  <AdminGroupsPanel canManage={canManageAll} />
                </TabsContent>
              )}
            </Tabs>
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
