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
  RouterContextProvider,
  createMemoryHistory,
  createRootRoute,
  createRouter,
} from '@tanstack/react-router'
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { AssetLibrary } from '..'

const originalAuth = useAuthStore.getState().auth
const config = {
  allowed: true,
  compliance_notice: 'I own the rights to everything I upload.',
  max_groups_per_user: 20,
  max_assets_per_user: 200,
  group_count: 1,
  asset_count: 2,
  group_types: ['AIGC'],
}
const group = {
  Id: 'group-1',
  Name: 'Hero',
  Description: 'main character',
  GroupType: 'AIGC',
  CreateTime: '2026-10-01T00:00:00Z',
}
const assets = [
  {
    Id: 'asset-ok',
    Name: 'front',
    AssetType: 'Image',
    GroupId: 'group-1',
    Status: 'Active',
    URL: 'https://tos.example/front.png',
    CreateTime: '2026-10-01T00:00:00Z',
  },
  {
    Id: 'asset-bad',
    Name: 'side',
    AssetType: 'Image',
    GroupId: 'group-1',
    Status: 'Failed',
    URL: '',
    CreateTime: '2026-10-01T00:00:00Z',
    Error: { Code: 'FaceMismatch', Message: 'face does not match' },
  },
]

function ark(result: unknown) {
  return { status: 200, data: { ResponseMetadata: {}, Result: result } }
}

function renderPage(statusEnabled: boolean) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  client.setQueryData(['status'], { asset_library_enabled: statusEnabled })
  const router = createRouter({
    routeTree: createRootRoute(),
    history: createMemoryHistory({ initialEntries: ['/asset-library'] }),
  })
  render(
    <QueryClientProvider client={client}>
      <RouterContextProvider router={router}>
        <AssetLibrary />
      </RouterContextProvider>
    </QueryClientProvider>
  )
}

function setUser(role: number) {
  useAuthStore.setState({
    auth: {
      ...originalAuth,
      user: { id: 1, username: 'alice', role },
    },
  })
}

beforeEach(() => {
  setUser(ROLE.USER)
  vi.spyOn(api, 'get').mockImplementation(async (url: string) => {
    if (url === '/api/asset-library/config') {
      return { data: { success: true, data: config } }
    }
    if (url === '/api/asset-library/admin/groups') {
      return {
        data: {
          success: true,
          data: {
            items: [
              {
                ...group,
                Id: 'group-bob',
                Name: 'Bob person',
                GroupType: 'LivenessFace',
                UserId: 2,
                Username: 'bob',
                AccountName: 'main',
                AssetCount: 3,
              },
            ],
            total: 1,
          },
        },
      }
    }
    return { data: { success: true, data: {} } }
  })
  vi.spyOn(api, 'post').mockImplementation(async (url: string) => {
    if (url.includes('Action=ListAssetGroups')) return ark({ Items: [group] })
    if (url.includes('Action=ListAssets')) return ark({ Items: assets })
    return ark({ Id: 'new-id' })
  })
})

afterEach(() => {
  vi.restoreAllMocks()
  useAuthStore.setState({ auth: originalAuth })
})

test('while the library is off the page is a 404', () => {
  renderPage(false)

  expect(screen.getByText('404')).toBeVisible()
  expect(api.get).not.toHaveBeenCalledWith('/api/asset-library/config')
})

test('a user sees their groups and cannot create a type no account offers', async () => {
  const user = userEvent.setup()
  renderPage(true)

  expect(await screen.findByText('Hero')).toBeVisible()
  expect(screen.getByText('Asset groups: 1 / 20')).toBeVisible()
  expect(
    screen.queryByRole('tab', { name: 'All users' })
  ).not.toBeInTheDocument()

  await user.click(screen.getByRole('tab', { name: 'Real people' }))
  expect(
    await screen.findByText('This type of asset group is not available to you.')
  ).toBeVisible()
  expect(screen.getByRole('button', { name: 'New asset group' })).toBeDisabled()
})

test('creating a group needs the compliance notice accepted', async () => {
  const user = userEvent.setup()
  renderPage(true)
  await user.click(
    await screen.findByRole('button', { name: 'New asset group' })
  )
  const dialog = await screen.findByRole('dialog')
  await user.type(within(dialog).getByLabelText('Name'), 'Villain')
  const create = within(dialog).getByRole('button', { name: 'Create' })
  expect(within(dialog).getByText(config.compliance_notice)).toBeVisible()
  expect(create).toBeDisabled()

  await user.click(
    within(dialog).getByRole('checkbox', {
      name: 'I have read and agree to the notice above',
    })
  )
  await user.click(create)

  await waitFor(() =>
    expect(api.post).toHaveBeenCalledWith(
      '/api/asset-library/openapi?Action=CreateAssetGroup&Version=2024-01-01',
      { Name: 'Villain', Description: '', GroupType: 'AIGC' },
      expect.anything()
    )
  )
})

test('a group shows its assets with status, error and the asset URI, and takes uploads', async () => {
  const user = userEvent.setup()
  renderPage(true)
  await user.click(await screen.findByRole('button', { name: /Hero/ }))

  const rows = await screen.findAllByTestId('asset-row')
  expect(rows).toHaveLength(2)
  expect(within(rows[0]).getByText('asset://asset-ok')).toBeVisible()
  expect(within(rows[0]).getByText('Active')).toBeVisible()
  expect(
    within(rows[1]).getByText('FaceMismatch: face does not match')
  ).toBeVisible()
  // An expired preview URL falls back to the placeholder.
  fireEvent.error(within(rows[0]).getByRole('img', { name: 'front' }))
  expect(within(rows[0]).queryByRole('img')).not.toBeInTheDocument()
  expect(within(rows[0]).getAllByText('Image')).toHaveLength(2)

  await user.click(screen.getByRole('button', { name: 'Upload asset' }))
  const dialog = await screen.findByRole('dialog', { name: 'Upload asset' })
  const upload = within(dialog).getByRole('button', { name: 'Upload' })
  expect(upload).toBeDisabled()
  await user.type(
    within(dialog).getByLabelText('File URL'),
    'https://cdn.example/back.png'
  )
  await user.click(upload)

  await waitFor(() =>
    expect(api.post).toHaveBeenCalledWith(
      '/api/asset-library/openapi?Action=CreateAsset&Version=2024-01-01',
      {
        GroupId: 'group-1',
        URL: 'https://cdn.example/back.png',
        AssetType: 'Image',
        Name: '',
      },
      expect.anything()
    )
  )
})

test('an administrator also sees every user’s groups', async () => {
  setUser(ROLE.SUPER_ADMIN)
  const user = userEvent.setup()
  renderPage(true)

  await user.click(await screen.findByRole('tab', { name: 'All users' }))

  expect(await screen.findByText('Bob person')).toBeVisible()
  expect(screen.getByText('bob (#2)')).toBeVisible()
  expect(
    screen.getByRole('button', { name: 'Delete asset group' })
  ).toBeVisible()
})
