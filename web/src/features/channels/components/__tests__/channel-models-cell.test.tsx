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
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { channelSchema, type Channel } from '../../types'
import { useChannelsColumns } from '../channels-columns'
import { ChannelsProvider } from '../channels-provider'
import { ChannelsTable } from '../channels-table'

afterEach(() => {
  localStorage.clear()
  vi.restoreAllMocks()
})

function ModelsCell(props: { channel: Channel }) {
  const table = useReactTable({
    data: [props.channel],
    columns: useChannelsColumns({ enableSelection: false }),
    getCoreRowModel: getCoreRowModel(),
  })
  const cell = table
    .getRowModel()
    .rows[0]?.getAllCells()
    .find((item) => item.column.id === 'models')

  return cell ? flexRender(cell.column.columnDef.cell, cell.getContext()) : null
}

function channelWithModels(models: string) {
  return channelSchema.parse({
    id: 1,
    type: 1,
    key: 'test-key',
    name: 'Upstream A',
    status: 1,
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
    models,
  })
}

function renderModelsCell(models: string) {
  const channel = channelWithModels(models)
  const { container } = render(
    <QueryClientProvider client={new QueryClient()}>
      <ChannelsProvider>
        <ModelsCell channel={channel} />
      </ChannelsProvider>
    </QueryClientProvider>
  )
  return container
}

it('shows how many models a channel has and lists and copies them on hover', async () => {
  const user = userEvent.setup()
  const copy = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()
  renderModelsCell('gpt-4o, gpt-4o-mini,,o3')

  const trigger = screen.getByRole('button', { name: 'Models: 3 models' })
  // The cell offset sits on the trigger: on the badge it would shrink the
  // trigger and cut the count short.
  expect(trigger).toHaveClass('-ml-1.5')
  expect(within(trigger).getByText('3 models')).not.toHaveClass('-ml-1.5')
  await user.hover(trigger)

  const list = await screen.findByRole('list', { name: 'Models: 3' })
  expect(
    within(list)
      .getAllByRole('listitem')
      .map((item) => item.textContent)
  ).toEqual(['gpt-4o', 'gpt-4o-mini', 'o3'])
  await user.click(screen.getByRole('button', { name: 'Copy all models' }))
  expect(copy).toHaveBeenCalledWith('gpt-4o,gpt-4o-mini,o3')
})

it('shows a dash and no list for a channel without models', () => {
  const container = renderModelsCell('')

  expect(container).toHaveTextContent('-')
  expect(screen.queryByRole('button')).not.toBeInTheDocument()
})

it('shows the model count column in the channel table by default', async () => {
  localStorage.setItem('channels:view-mode', 'table')
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/channel') {
      return {
        data: {
          success: true,
          data: {
            items: [channelWithModels('gpt-4o,o3')],
            total: 1,
            type_counts: {},
          },
        },
      }
    }
    return { data: { success: true, data: [] } }
  })
  const root = createRootRoute()
  const auth = createRoute({ getParentRoute: () => root, id: '_authenticated' })
  const channelsRoute = createRoute({
    getParentRoute: () => auth,
    path: 'channels/',
    component: () => (
      <ChannelsProvider>
        <ChannelsTable />
      </ChannelsProvider>
    ),
  })
  const router = createRouter({
    routeTree: root.addChildren([auth.addChildren([channelsRoute])]),
    history: createMemoryHistory({ initialEntries: ['/channels/'] }),
  })
  await router.load()
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <RouterProvider router={router} />
    </QueryClientProvider>
  )

  const count = await screen.findByRole('button', {
    name: 'Models: 2 models',
  })
  expect(count).toHaveTextContent('2 models')
  expect(
    screen.getAllByRole('columnheader').map((header) => header.textContent)
  ).toContain('Models')
})
