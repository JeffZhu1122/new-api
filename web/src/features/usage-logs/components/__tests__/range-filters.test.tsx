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
import { getCoreRowModel, useReactTable } from '@tanstack/react-table'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { toast } from 'sonner'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { CommonLogsFilterBar } from '../common-logs-filter-bar'
import { UsageLogsProvider } from '../usage-logs-provider'

function FilterFixture() {
  const table = useReactTable({
    data: [],
    columns: [],
    getCoreRowModel: getCoreRowModel(),
  })
  return (
    <UsageLogsProvider>
      <CommonLogsFilterBar table={table} />
    </UsageLogsProvider>
  )
}

async function renderFilter(initialEntry = '/usage-logs/common') {
  vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: {
      success: true,
      data: url === '/api/user/self/groups' ? {} : { quota: 0, rpm: 0, tpm: 0 },
    },
  }))
  const root = createRootRoute()
  const auth = createRoute({ getParentRoute: () => root, id: '_authenticated' })
  const logs = createRoute({
    getParentRoute: () => auth,
    path: '/usage-logs/$section',
    component: FilterFixture,
    validateSearch: (search: Record<string, unknown>) => search,
  })
  const router = createRouter({
    routeTree: root.addChildren([auth.addChildren([logs])]),
    history: createMemoryHistory({ initialEntries: [initialEntry] }),
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  await screen.findByRole('combobox', { name: 'Type' })
  return router
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

it('searches by input and output token bounds and counts each range once', async () => {
  const router = await renderFilter()
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: /Expand/ }))

  await user.type(screen.getByLabelText('Min input tokens'), '1000')
  await user.type(screen.getByLabelText('Max input tokens'), '20000')
  await user.type(screen.getByLabelText('Max output tokens'), '500')
  await user.click(screen.getByRole('button', { name: 'Search' }))

  await waitFor(() =>
    expect(router.state.location.search).toMatchObject({
      page: 1,
      inputTokensMin: 1000,
      inputTokensMax: 20000,
      outputTokensMax: 500,
    })
  )
  expect(router.state.location.search).not.toHaveProperty('outputTokensMin')
  // Two ranges are two active filters, not four.
  expect(screen.getByRole('button', { name: /Collapse/ })).toHaveTextContent(
    '2'
  )
})

it('fills the token boxes from the URL', async () => {
  await renderFilter(
    '/usage-logs/common?inputTokensMin=100&outputTokensMin=0&outputTokensMax=50'
  )
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: /Expand/ }))

  expect(screen.getByLabelText('Min input tokens')).toHaveValue(100)
  expect(screen.getByLabelText('Max input tokens')).toHaveValue(null)
  expect(screen.getByLabelText('Min output tokens')).toHaveValue(0)
  expect(screen.getByLabelText('Max output tokens')).toHaveValue(50)
  expect(screen.getByRole('group', { name: 'Input tokens' })).toBeVisible()
})

it('refuses a minimum above the maximum before searching', async () => {
  const error = vi.spyOn(toast, 'error')
  const router = await renderFilter()
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: /Expand/ }))

  await user.type(screen.getByLabelText('Min output tokens'), '900')
  await user.type(screen.getByLabelText('Max output tokens'), '100')
  await user.click(screen.getByRole('button', { name: 'Search' }))

  expect(error).toHaveBeenCalledWith(
    'The minimum token count cannot be greater than the maximum'
  )
  expect(router.state.location.search).not.toHaveProperty('outputTokensMin')
})

it('searches by use time, a fraction of a second of TTFT and picked or typed status codes', async () => {
  const router = await renderFilter()
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: /Expand/ }))

  await user.type(screen.getByLabelText('Min use time (s)'), '5')
  await user.type(screen.getByLabelText('Max TTFT (s)'), '2.5')
  const statusCodes = screen.getByRole('combobox', { name: 'Status codes' })
  await user.click(statusCodes)
  await user.click(await screen.findByRole('option', { name: /^429/ }))
  await user.type(statusCodes, '418{Enter}')
  await user.click(screen.getByRole('button', { name: 'Search' }))

  await waitFor(() =>
    expect(router.state.location.search).toMatchObject({
      page: 1,
      useTimeMin: 5,
      ttftMax: 2.5,
      statusCodes: [429, 418],
    })
  )
  expect(screen.getByRole('button', { name: /Collapse/ })).toHaveTextContent(
    '3'
  )
})

it('shows the status codes of the URL as chosen', async () => {
  await renderFilter(
    `/usage-logs/common?statusCodes=${encodeURIComponent('[429,502]')}&ttftMin=1.5`
  )
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: /Expand/ }))

  expect(screen.getByLabelText('Min TTFT (s)')).toHaveValue(1.5)
  for (const code of ['429', '502']) {
    expect(screen.getByText(code)).toBeVisible()
  }
  expect(screen.getByRole('button', { name: /Collapse/ })).toHaveTextContent(
    '2'
  )
})

it('searches by stream mode and counts it as an expanded filter', async () => {
  const router = await renderFilter()
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: /Expand/ }))

  await user.click(screen.getByRole('combobox', { name: 'Stream mode' }))
  await user.click(
    await screen.findByRole('option', { name: 'Non-stream requests' })
  )
  await user.click(screen.getByRole('button', { name: 'Search' }))

  await waitFor(() =>
    expect(router.state.location.search).toMatchObject({
      page: 1,
      stream: 'non_stream',
    })
  )
  expect(screen.getByRole('button', { name: /Collapse/ })).toHaveTextContent(
    '1'
  )
})

it('shows the stream mode of the URL as chosen', async () => {
  await renderFilter('/usage-logs/common?stream=stream')
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: /Expand/ }))

  expect(
    screen.getByRole('combobox', { name: 'Stream mode' })
  ).toHaveTextContent('Stream requests')
  expect(screen.getByRole('button', { name: /Collapse/ })).toHaveTextContent(
    '1'
  )
})

it('searches every log again after choosing all stream modes', async () => {
  const router = await renderFilter('/usage-logs/common?stream=non_stream')
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: /Expand/ }))

  await user.click(screen.getByRole('combobox', { name: 'Stream mode' }))
  await user.click(await screen.findByRole('option', { name: 'Stream: All' }))
  await user.click(screen.getByRole('button', { name: 'Search' }))

  await waitFor(() =>
    expect(router.state.location.search).not.toHaveProperty('stream')
  )
  expect(
    screen.getByRole('combobox', { name: 'Stream mode' })
  ).toHaveTextContent('Stream: All')
})
