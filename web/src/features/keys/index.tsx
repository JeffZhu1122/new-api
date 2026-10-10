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
import { getRouteApi, Link } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { LoadingState } from '@/components/loading-state'
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from '@/components/ui/breadcrumb'
import { getUser } from '@/features/users/api'
import type { NavigateFn } from '@/hooks/use-table-url-state'
import { requireServerSuccess } from '@/lib/server-error-message'

import { ApiKeysDialogs } from './components/api-keys-dialogs'
import { ApiKeysPrimaryButtons } from './components/api-keys-primary-buttons'
import { ApiKeysProvider } from './components/api-keys-provider'
import { ApiKeysTable } from './components/api-keys-table'
import { ApiKeyOwnerContext, type ApiKeyOwner } from './hooks/use-api-key-owner'

const route = getRouteApi('/_authenticated/keys/')

export function ApiKeys() {
  return (
    <ApiKeysPage search={route.useSearch()} navigate={route.useNavigate()} />
  )
}

type ApiKeysPageProps = {
  search: Record<string, unknown>
  navigate: NavigateFn
  // Set when the root user manages another user's keys
  owner?: ApiKeyOwner
  breadcrumb?: ReactNode
}

export function ApiKeysPage(props: ApiKeysPageProps) {
  const { t } = useTranslation()
  return (
    <ApiKeyOwnerContext value={props.owner ?? null}>
      <ApiKeysProvider>
        <SectionPageLayout fixedContent>
          {props.breadcrumb != null && (
            <SectionPageLayout.Breadcrumb>
              {props.breadcrumb}
            </SectionPageLayout.Breadcrumb>
          )}
          <SectionPageLayout.Title>
            {props.owner
              ? t('API Keys of {{username}}', {
                  username: props.owner.username,
                })
              : t('API Keys')}
          </SectionPageLayout.Title>
          <SectionPageLayout.Actions>
            <ApiKeysPrimaryButtons />
          </SectionPageLayout.Actions>
          <SectionPageLayout.Content>
            <ApiKeysTable search={props.search} navigate={props.navigate} />
          </SectionPageLayout.Content>
        </SectionPageLayout>

        <ApiKeysDialogs />
      </ApiKeysProvider>
    </ApiKeyOwnerContext>
  )
}

type UserApiKeysProps = {
  userId: number
  search: Record<string, unknown>
  navigate: NavigateFn
}

/**
 * The root user's view of another user's API keys.
 */
export function UserApiKeys(props: UserApiKeysProps) {
  const { t } = useTranslation()
  const validId = Number.isInteger(props.userId) && props.userId > 0
  const query = useQuery({
    queryKey: ['user', props.userId],
    queryFn: async () => requireServerSuccess(await getUser(props.userId)),
    enabled: validId,
  })
  const user = query.data?.data

  if (!user) {
    let content = <LoadingState message={t('Loading...')} />
    if (!validId || query.isError) {
      content = (
        <ErrorState
          description={t('User does not exist')}
          onRetry={validId ? () => void query.refetch() : undefined}
        />
      )
    }
    return (
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('API Keys')}</SectionPageLayout.Title>
        <SectionPageLayout.Content>{content}</SectionPageLayout.Content>
      </SectionPageLayout>
    )
  }

  return (
    <ApiKeysPage
      owner={{ id: user.id, username: user.username }}
      search={props.search}
      navigate={props.navigate}
      breadcrumb={
        <Breadcrumb>
          <BreadcrumbList>
            <BreadcrumbItem>
              <BreadcrumbLink render={<Link to='/users' />}>
                {t('Users')}
              </BreadcrumbLink>
            </BreadcrumbItem>
            <BreadcrumbSeparator />
            <BreadcrumbItem>
              <BreadcrumbPage>{user.username}</BreadcrumbPage>
            </BreadcrumbItem>
          </BreadcrumbList>
        </Breadcrumb>
      }
    />
  )
}
