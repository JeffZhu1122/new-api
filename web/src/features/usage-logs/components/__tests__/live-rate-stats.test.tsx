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
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import type { LogStatistics } from '../../types'
import { CommonLogsStats } from '../common-logs-stats'
import { UsageLogsProvider } from '../usage-logs-provider'

const clients: QueryClient[] = []

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  for (const client of clients.splice(0)) client.clear()
})

function renderStats(rate: Partial<LogStatistics>) {
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
    const rateOnly = String(url).includes('rate_only=true')
    return {
      data: {
        success: true,
        data: rateOnly
          ? { quota: 0, rpm: 0, tpm: 0, ...rate }
          : { quota: 1000, rpm: 0, tpm: 0, rate_source: 'redis' },
      },
    }
  })
  const root = createRootRoute()
  const auth = createRoute({ getParentRoute: () => root, id: '_authenticated' })
  const logs = createRoute({
    getParentRoute: () => auth,
    path: '/usage-logs/$section',
    component: () => (
      <UsageLogsProvider>
        <CommonLogsStats />
      </UsageLogsProvider>
    ),
    validateSearch: (search: Record<string, unknown>) => search,
  })
  const router = createRouter({
    routeTree: root.addChildren([auth.addChildren([logs])]),
    history: createMemoryHistory({
      initialEntries: ['/usage-logs/common?model=gpt-4o'],
    }),
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  return get
}

function badgeValue(label: string) {
  return screen.getByText(label).parentElement
}

it('shows the live token split from a rate-only poll with the same filters', async () => {
  const get = renderStats({
    rpm: 12,
    tpm: 1500,
    tpm_input: 1000,
    tpm_cache_read: 300,
    tpm_cache_write: 100,
    tpm_output: 100,
    rate_source: 'redis',
  })

  expect(await screen.findByText('Input TPM')).toBeInTheDocument()
  await vi.waitFor(() => expect(badgeValue('RPM')).toHaveTextContent('12'))
  expect(badgeValue('TPM')).toHaveTextContent('1,500')
  expect(badgeValue('Input TPM')).toHaveTextContent('1,000')
  expect(badgeValue('Cache read TPM')).toHaveTextContent('300')
  expect(badgeValue('Cache write TPM')).toHaveTextContent('100')
  expect(badgeValue('Output TPM')).toHaveTextContent('100')

  const urls = get.mock.calls.map(([url]) => String(url))
  const ratePoll = urls.find((url) => url.includes('rate_only=true'))
  expect(ratePoll).toContain('model_name=gpt-4o')
  expect(urls.some((url) => !url.includes('rate_only'))).toBe(true)
})

it('shows dashes when the live statistics are unavailable', async () => {
  renderStats({ rpm: 0, tpm: 0, rate_source: 'unavailable' })

  expect(await screen.findByText('Output TPM')).toBeInTheDocument()
  await vi.waitFor(() => expect(badgeValue('RPM')).toHaveTextContent('-'))
  expect(badgeValue('Input TPM')).toHaveTextContent('-')
})

it('notes counts that cover this server instance only', async () => {
  renderStats({ rpm: 3, tpm: 30, tpm_input: 30, rate_source: 'memory' })

  expect(
    await screen.findByText('Counted on this server instance only.')
  ).toBeInTheDocument()
  expect(badgeValue('RPM')).toHaveTextContent('3')
})
