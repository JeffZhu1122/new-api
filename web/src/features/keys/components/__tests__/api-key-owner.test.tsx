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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  getRouteApi,
  RouterProvider,
} from '@tanstack/react-router'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { ApiKeysPage } from '../..'
import { apiKeySchema } from '../../types'

const OWNER = { id: 42, username: 'alice' }
const KEY = apiKeySchema.parse({
  id: 7,
  name: 'alice-prod',
  key: 'demo********1234',
  status: 1,
  remain_quota: 0,
  used_quota: 0,
  unlimited_quota: true,
  expired_time: -1,
  created_time: 0,
  accessed_time: 0,
  group: 'default',
  model_limits_enabled: false,
})

const ownerRouteApi = getRouteApi('/_authenticated/users/$userId/keys')
const clients: QueryClient[] = []

function OwnerPage() {
  return (
    <ApiKeysPage
      owner={OWNER}
      search={ownerRouteApi.useSearch()}
      navigate={ownerRouteApi.useNavigate()}
    />
  )
}

// Answers the owner list, groups and the verification method lookup.
function mockOwnerApi() {
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url.startsWith('/api/user/42/tokens?')) {
      return { data: { success: true, data: { items: [KEY], total: 1 } } }
    }
    if (url === '/api/verify/methods') {
      return {
        data: {
          success: true,
          data: {
            scope: 'admin.user.token.read',
            methods: [{ method: '2fa', available: true }],
            oauth_providers: [],
            password_encryption_enabled: false,
          },
        },
      }
    }
    return { data: { success: true, data: { default: { ratio: 1 } } } }
  })
  const post = vi.spyOn(api, 'post').mockImplementation(async (url) => {
    if (url === '/api/verify') {
      return {
        data: {
          success: true,
          data: {
            proof_token: 'token-read-proof',
            method: '2fa',
            scope: 'admin.user.token.read',
            expires_at: Math.floor(Date.now() / 1000) + 60,
          },
        },
      }
    }
    if (url === '/api/user/42/tokens/7/key') {
      return {
        data: { success: true, data: { key: 'fake-key-for-test-only' } },
      }
    }
    throw new Error(`Unexpected POST ${url}`)
  })
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true, data: KEY } })
  return { get, post, put }
}

async function renderOwnerPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(['status'], {})
  clients.push(client)
  const root = createRootRoute()
  const auth = createRoute({ getParentRoute: () => root, id: '_authenticated' })
  const ownerRoute = createRoute({
    getParentRoute: () => auth,
    path: 'users/$userId/keys',
    component: OwnerPage,
  })
  const router = createRouter({
    routeTree: root.addChildren([auth.addChildren([ownerRoute])]),
    history: createMemoryHistory({ initialEntries: ['/users/42/keys'] }),
  })
  await router.load()
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  await screen.findByText(KEY.name)
}

beforeEach(() => {
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  localStorage.clear()
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
})

afterEach(() => {
  cleanup()
  localStorage.clear()
  clients.splice(0).forEach((client) => client.clear())
  vi.restoreAllMocks()
})

it('lists the managed user keys from the root endpoint without batch selection or self-only actions', async () => {
  const { get } = mockOwnerApi()
  await renderOwnerPage()

  expect(get).toHaveBeenCalledWith('/api/user/42/tokens?p=1&size=20')
  expect(
    screen.getByRole('heading', { name: 'API Keys of alice' })
  ).toBeInTheDocument()
  expect(screen.queryByRole('checkbox', { name: 'Select all' })).toBeNull()
  expect(screen.queryByRole('button', { name: /API Addresses/ })).toBeNull()
  expect(screen.queryByRole('button', { name: /Groups & Models/ })).toBeNull()
  expect(
    screen.getByRole('button', { name: /Create API Key/ })
  ).toBeInTheDocument()
})

it('changes the status of a managed key through the root endpoint', async () => {
  const { put } = mockOwnerApi()
  await renderOwnerPage()

  await userEvent.click(screen.getByRole('button', { name: 'Disable' }))

  await waitFor(() =>
    expect(put).toHaveBeenCalledWith('/api/user/42/tokens?status_only=true', {
      id: 7,
      status: 2,
    })
  )
})

it('reveals a managed key only with a proof bound to that user and key', async () => {
  const user = userEvent.setup()
  const { post } = mockOwnerApi()
  const copy = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()
  await renderOwnerPage()

  await user.click(screen.getByRole('button', { name: 'Open menu' }))
  expect(screen.queryByRole('menuitem', { name: 'CC Switch' })).toBeNull()
  await user.click(screen.getByRole('menuitem', { name: 'Copy Key' }))
  expect(post).not.toHaveBeenCalledWith(
    '/api/user/42/tokens/7/key',
    undefined,
    expect.anything()
  )

  await user.type(
    await screen.findByLabelText('Authenticator code or backup code'),
    '123456'
  )
  await user.click(screen.getByRole('button', { name: 'Verify' }))

  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/user/42/tokens/7/key',
      undefined,
      expect.objectContaining({
        headers: { 'X-Security-Proof': 'token-read-proof' },
        singleUseAuthorization: true,
      })
    )
  )
  expect(post).toHaveBeenCalledWith(
    '/api/verify',
    expect.objectContaining({
      scope: 'admin.user.token.read',
      context: { user_id: 42, token_id: 7 },
    }),
    expect.anything()
  )
  await waitFor(() =>
    expect(copy).toHaveBeenCalledWith('sk-fake-key-for-test-only')
  )
})

it('does not request the key when verification is cancelled', async () => {
  const user = userEvent.setup()
  const { post } = mockOwnerApi()
  await renderOwnerPage()

  await user.click(screen.getByRole('button', { name: 'Open menu' }))
  await user.click(screen.getByRole('menuitem', { name: 'Copy Key' }))
  await screen.findByLabelText('Authenticator code or backup code')
  await user.click(screen.getByRole('button', { name: 'Cancel' }))

  await waitFor(() =>
    expect(
      screen.queryByLabelText('Authenticator code or backup code')
    ).toBeNull()
  )
  expect(post).not.toHaveBeenCalledWith(
    '/api/user/42/tokens/7/key',
    undefined,
    expect.anything()
  )
})
