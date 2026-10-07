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
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { STATUS_QUERY_KEY } from '@/lib/status-query'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { Footer } from '../footer'

let client: QueryClient

beforeEach(() => {
  window.localStorage.clear()
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  // The footer only reads the shared /api/status cache.
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    throw new Error(`Unexpected footer request: ${url}`)
  })
})

afterEach(() => {
  vi.restoreAllMocks()
  client.clear()
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  window.localStorage.clear()
})

async function renderFooter(status: Record<string, unknown> = {}) {
  client.setQueryData(STATUS_QUERY_KEY, status)
  const router = createRouter({
    routeTree: createRootRoute({ component: () => <Footer /> }),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  await router.load()
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

describe('Footer', () => {
  it('in the default footer keeps the project attribution link opening in a new tab', async () => {
    await renderFooter()

    const attribution = await screen.findByRole('link', { name: 'New API' })
    expect(attribution).toHaveAttribute(
      'href',
      'https://github.com/QuantumNous/new-api'
    )
    expect(attribution).toHaveAttribute('target', '_blank')
  })

  it('with legal pages enabled links both from the copyright row', async () => {
    await renderFooter({
      user_agreement_enabled: true,
      privacy_policy_enabled: true,
    })

    expect(
      await screen.findByRole('link', { name: 'User Agreement' })
    ).toHaveAttribute('href', '/user-agreement')
    expect(
      screen.getByRole('link', { name: 'Privacy Policy' })
    ).toHaveAttribute('href', '/privacy-policy')
  })

  it('with legal pages disabled renders no legal links', async () => {
    await renderFooter()

    await screen.findByRole('link', { name: 'New API' })
    expect(
      screen.queryByRole('link', { name: 'User Agreement' })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('link', { name: 'Privacy Policy' })
    ).not.toBeInTheDocument()
  })
})
