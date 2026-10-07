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
import { render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { STATUS_QUERY_KEY } from '@/lib/status-query'

import { Rankings } from '../index'
import type { RankingsSnapshot } from '../types'

vi.mock('@visactor/react-vchart', () => ({ VChart: () => null }))
vi.mock('@visactor/vchart', () => ({
  ThemeManager: { setCurrentTheme: vi.fn() },
}))

const snapshot: RankingsSnapshot = {
  models: [
    {
      rank: 1,
      model_name: 'gpt-4o',
      vendor: 'OpenAI',
      category: 'all',
      total_tokens: 1200,
      share: 1,
      growth_pct: 5,
    },
  ],
  vendors: [
    {
      rank: 1,
      vendor: 'OpenAI',
      total_tokens: 1200,
      share: 1,
      growth_pct: 5,
      models_count: 1,
      top_model: 'gpt-4o',
    },
  ],
  top_movers: [],
  top_droppers: [],
  models_history: { points: [], models: [], buckets: 0 },
  vendor_share_history: { points: [], vendors: [], buckets: 0 },
}

let client: QueryClient

beforeEach(() => {
  window.localStorage.clear()
  client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(STATUS_QUERY_KEY, {})
})

afterEach(() => {
  client.clear()
  vi.restoreAllMocks()
  window.localStorage.clear()
})

// Only /api/rankings varies per test; the public header's own requests get
// empty successful payloads.
function mockRankingsResponse(rankings: () => Promise<unknown>) {
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/rankings') return rankings()
    return { data: { success: true, data: '' } }
  })
}

async function renderRankings() {
  const rootRoute = createRootRoute()
  const rankingsRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/rankings/',
    component: Rankings,
  })
  const router = createRouter({
    routeTree: rootRoute.addChildren([rankingsRoute]),
    history: createMemoryHistory({ initialEntries: ['/rankings'] }),
  })
  await router.load()
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

describe('rankings page states', () => {
  it('shows placeholders and neither data nor an error while rankings load', async () => {
    mockRankingsResponse(() => new Promise(() => undefined))

    await renderRankings()

    const periodTabs = await screen.findByRole('tablist', { name: 'Period' })
    const placeholders = periodTabs
      .closest('section')
      ?.nextElementSibling?.querySelectorAll('[data-slot="skeleton"]')
    expect(placeholders).toHaveLength(3)
    expect(screen.queryByRole('heading', { name: 'Top Models' })).toBeNull()
    expect(
      screen.queryByRole('heading', { name: 'Unable to load rankings' })
    ).toBeNull()
  })

  it('shows the server reason when rankings fail to load', async () => {
    mockRankingsResponse(async () => ({
      data: { success: false, message: 'Rankings are disabled' },
    }))

    await renderRankings()

    expect(
      await screen.findByRole('heading', { name: 'Unable to load rankings' })
    ).toBeVisible()
    expect(screen.getByText('Rankings are disabled')).toBeVisible()
    expect(screen.queryByRole('heading', { name: 'Top Models' })).toBeNull()
  })

  it('renders the model, market share and movement sections once data arrives', async () => {
    mockRankingsResponse(async () => ({
      data: { success: true, data: snapshot },
    }))

    await renderRankings()

    expect(
      await screen.findByRole('heading', { name: 'Top Models' })
    ).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Market Share' })).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Trending up' })).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Trending down' })).toBeVisible()
    expect(screen.getAllByRole('link', { name: 'gpt-4o' })).not.toHaveLength(0)
    expect(
      screen.queryByRole('heading', { name: 'Unable to load rankings' })
    ).toBeNull()
  })
})
