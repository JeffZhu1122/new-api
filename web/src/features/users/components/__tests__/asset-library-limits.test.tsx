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
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import type { User } from '../../types'
import { UsersMutateDrawer } from '../users-mutate-drawer'
import { UsersProvider } from '../users-provider'

const target: User = {
  id: 7,
  username: 'creator',
  display_name: 'Creator',
  role: 1,
  status: 1,
  quota: 0,
  used_quota: 0,
  request_count: 0,
  group: 'default',
}

function renderDrawer(limits: User['asset_library_limits']) {
  useAuthStore.getState().auth.setUser({ id: 1, username: 'root', role: 100 })
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/authz/catalog') {
      return { data: { success: true, data: { resources: [], roles: [] } } }
    }
    if (url === '/api/group/') {
      return { data: { success: true, data: ['default'] } }
    }
    return {
      data: {
        success: true,
        data: { ...target, asset_library_limits: limits },
      },
    }
  })
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <UsersProvider>
        <UsersMutateDrawer
          open
          onOpenChange={() => undefined}
          currentRow={target}
        />
      </UsersProvider>
    </QueryClientProvider>
  )
  return put
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  useAuthStore.getState().auth.reset()
})

it('shows the saved override and sends 0 for an unlimited asset count', async () => {
  const put = renderDrawer({ max_groups_per_user: 5 })
  await screen.findByDisplayValue('Creator')
  const groups = screen.getByLabelText('Max asset groups')
  const assets = screen.getByLabelText('Max assets')
  await waitFor(() => expect(groups).toHaveValue('5'))
  expect(assets).toHaveValue('')
  expect(assets).toHaveAttribute('placeholder', 'Inherit')

  await userEvent.type(assets, '0')
  await userEvent.click(screen.getByRole('button', { name: 'Save changes' }))

  await waitFor(() =>
    expect(put).toHaveBeenCalledWith(
      '/api/user/',
      expect.objectContaining({
        id: 7,
        asset_library_limits: {
          max_groups_per_user: 5,
          max_assets_per_user: 0,
        },
      }),
      {}
    )
  )
})

it('clearing both limits sends an empty override so the user inherits again', async () => {
  const put = renderDrawer({ max_groups_per_user: 5, max_assets_per_user: 50 })
  await screen.findByDisplayValue('Creator')
  const groups = screen.getByLabelText('Max asset groups')
  await waitFor(() => expect(groups).toHaveValue('5'))

  await userEvent.clear(groups)
  await userEvent.clear(screen.getByLabelText('Max assets'))
  await userEvent.click(screen.getByRole('button', { name: 'Save changes' }))

  await waitFor(() => expect(put).toHaveBeenCalled())
  expect(put.mock.calls[0][1]).toMatchObject({ asset_library_limits: {} })
})

it('a negative limit is refused before saving', async () => {
  const put = renderDrawer(null)
  await screen.findByDisplayValue('Creator')
  const groups = screen.getByLabelText('Max asset groups')

  await userEvent.type(groups, '-1')
  await userEvent.click(screen.getByRole('button', { name: 'Save changes' }))

  await waitFor(() => expect(groups).toHaveAttribute('aria-invalid', 'true'))
  expect(
    screen.getByText('Must be an integer within [0, 2147483647]')
  ).toBeInTheDocument()
  expect(put).not.toHaveBeenCalled()
})
