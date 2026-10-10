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
  RouterProvider,
} from '@tanstack/react-router'
import type { Row } from '@tanstack/react-table'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import type { User } from '../../types'
import { DataTableRowActions } from '../data-table-row-actions'
import { UsersProvider } from '../users-provider'

const target: User = {
  id: 2,
  username: 'managed-user',
  display_name: 'Managed user',
  role: 1,
  status: 1,
  quota: 0,
  used_quota: 0,
  request_count: 0,
  group: 'default',
}

async function renderRowActions(viewerRole: number) {
  useAuthStore
    .getState()
    .auth.setUser({ id: 1, username: 'operator', role: viewerRole })
  const root = createRootRoute()
  const usersRoute = createRoute({
    getParentRoute: () => root,
    path: 'users/',
    component: () => (
      <UsersProvider>
        <DataTableRowActions row={{ original: target } as Row<User>} />
      </UsersProvider>
    ),
  })
  const keysRoute = createRoute({
    getParentRoute: () => root,
    path: 'users/$userId/keys',
    component: () => <p>managed keys page</p>,
  })
  const router = createRouter({
    routeTree: root.addChildren([usersRoute, keysRoute]),
    history: createMemoryHistory({ initialEntries: ['/users/'] }),
  })
  await router.load()
  render(
    <QueryClientProvider client={new QueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  await userEvent.click(
    await screen.findByRole('button', { name: 'Open menu' })
  )
  return router
}

afterEach(() => {
  cleanup()
  useAuthStore.getState().auth.reset()
})

it('lets the root user open the API keys of a user from the row menu', async () => {
  const router = await renderRowActions(100)

  await userEvent.click(
    await screen.findByRole('menuitem', { name: 'Manage API Keys' })
  )

  await waitFor(() =>
    expect(router.state.location.pathname).toBe('/users/2/keys')
  )
})

it('does not offer API key management to an administrator', async () => {
  await renderRowActions(10)

  expect(
    await screen.findByRole('menuitem', { name: 'Manage Bindings' })
  ).toBeInTheDocument()
  expect(screen.queryByRole('menuitem', { name: 'Manage API Keys' })).toBeNull()
})
