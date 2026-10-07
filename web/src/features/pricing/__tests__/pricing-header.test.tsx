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

import { Pricing } from '../index'

vi.mock('@visactor/react-vchart', () => ({ VChart: () => null }))
vi.mock('@visactor/vchart', () => ({
  ThemeManager: { setCurrentTheme: vi.fn() },
}))

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

// Only /api/pricing varies per test; the public header's own requests get
// empty successful payloads.
function mockPricingResponse(pricing: () => Promise<unknown>) {
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/pricing') return pricing()
    return { data: { success: true, data: '' } }
  })
}

async function renderPricing() {
  const rootRoute = createRootRoute()
  const pricingRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/pricing/',
    component: Pricing,
  })
  const router = createRouter({
    routeTree: rootRoute.addChildren([pricingRoute]),
    history: createMemoryHistory({ initialEntries: ['/pricing'] }),
  })
  await router.load()
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

describe('pricing page header', () => {
  it('shows the busy skeleton instead of the header while pricing loads', async () => {
    mockPricingResponse(() => new Promise(() => undefined))

    const { container } = await renderPricing()

    expect(container.querySelector('[aria-busy="true"]')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Model Square' })).toBeNull()
  })

  it('shrinks the gradient title to its text and balances the description lines', async () => {
    mockPricingResponse(async () => ({
      data: {
        success: true,
        data: [],
        vendors: [],
        group_ratio: {},
        usable_group: {},
      },
    }))

    await renderPricing()

    expect(
      await screen.findByRole('heading', { level: 1, name: 'Model Square' })
    ).toHaveClass('brand-text-aurora', 'mx-auto', 'w-fit')
    expect(
      screen.getByText(/^Discover curated AI models/, { selector: 'p' })
    ).toHaveClass('text-balance')
  })
})
