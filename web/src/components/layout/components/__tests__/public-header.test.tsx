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
  Outlet,
  RouterProvider,
} from '@tanstack/react-router'
import { fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ThemeProvider } from '@/context/theme-provider'
import { api } from '@/lib/api'
import { STATUS_QUERY_KEY } from '@/lib/status-query'
import { useAuthStore } from '@/stores/auth-store'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { PublicHeader } from '../public-header'

const ROUTE_PATHS = ['/', '/dashboard', '/docs', '/about', '/sign-in']

let client: QueryClient

beforeEach(() => {
  window.localStorage.clear()
  useAuthStore.setState(useAuthStore.getInitialState(), true)
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  useSystemConfigStore.setState({ loading: false })
  client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/notice') return { data: { success: true, data: '' } }
    throw new Error(`Unexpected header request: ${url}`)
  })
})

afterEach(() => {
  vi.restoreAllMocks()
  client.clear()
  useAuthStore.setState(useAuthStore.getInitialState(), true)
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  window.localStorage.clear()
  document.body.style.overflow = ''
  Object.defineProperty(window, 'scrollY', { configurable: true, value: 0 })
})

async function renderHeader(initialPath: string) {
  // An empty status keeps the default module set: Home, Console, Docs, About.
  client.setQueryData(STATUS_QUERY_KEY, {})
  const rootRoute = createRootRoute({
    component: () => (
      <>
        <PublicHeader />
        <Outlet />
      </>
    ),
  })
  const router = createRouter({
    routeTree: rootRoute.addChildren(
      ROUTE_PATHS.map((path) =>
        createRoute({ getParentRoute: () => rootRoute, path })
      )
    ),
    history: createMemoryHistory({ initialEntries: [initialPath] }),
  })
  await router.load()
  render(
    <QueryClientProvider client={client}>
      <ThemeProvider>
        <RouterProvider router={router} />
      </ThemeProvider>
    </QueryClientProvider>
  )
  return router
}

function getMobileOverlay(): HTMLElement {
  const backdrop = document.querySelector("[data-brand-backdrop='dawn']")
  if (!(backdrop?.parentElement instanceof HTMLElement)) {
    throw new Error('Missing mobile overlay backdrop')
  }
  return backdrop.parentElement
}

describe('PublicHeader', () => {
  it('renders the desktop nav links with their targets', async () => {
    await renderHeader('/')

    const banner = await screen.findByRole('banner')
    const hrefs = ['Home', 'Console', 'Docs', 'About'].map((name) =>
      within(banner).getByRole('link', { name }).getAttribute('href')
    )
    expect(hrefs).toEqual(['/', '/dashboard', '/docs', '/about'])
  })

  it('on the current path marks only that desktop link as active', async () => {
    await renderHeader('/about')

    const banner = await screen.findByRole('banner')
    const about = within(banner).getByRole('link', { name: 'About' })
    expect(about).toHaveClass('text-foreground')
    expect(about).not.toHaveClass('text-muted-foreground')
    for (const name of ['Home', 'Console', 'Docs']) {
      expect(within(banner).getByRole('link', { name })).toHaveClass(
        'text-muted-foreground'
      )
    }
  })

  it('when signed out links the desktop sign-in action to the sign-in page', async () => {
    await renderHeader('/')

    const banner = await screen.findByRole('banner')
    expect(
      within(banner).getByRole('button', { name: 'Sign in' })
    ).toHaveAttribute('href', '/sign-in')
  })

  it('after scrolling uses the strong glass token for the floating pill', async () => {
    await renderHeader('/')
    const banner = await screen.findByRole('banner')
    const nav = within(banner).getByRole('navigation')
    expect(nav).not.toHaveClass('brand-glass')

    Object.defineProperty(window, 'scrollY', { configurable: true, value: 40 })
    fireEvent.scroll(window)

    expect(nav).toHaveClass(
      'brand-glass',
      '[--glass-bg:var(--glass-bg-strong)]'
    )
  })

  it('opening the mobile menu makes the overlay interactive and locks page scroll', async () => {
    const user = userEvent.setup()
    await renderHeader('/')
    const overlay = getMobileOverlay()
    expect(overlay).toHaveClass('pointer-events-none')

    await user.click(
      await screen.findByRole('button', { name: 'Toggle navigation menu' })
    )

    expect(overlay).toHaveClass('pointer-events-auto')
    expect(document.body.style.overflow).toBe('hidden')
  })

  it('when signed out the mobile CTA closes the menu and goes to sign-in', async () => {
    const user = userEvent.setup()
    const router = await renderHeader('/')
    await user.click(
      await screen.findByRole('button', { name: 'Toggle navigation menu' })
    )

    const cta = within(getMobileOverlay()).getByRole('link', {
      name: 'Sign in',
    })
    expect(cta).toHaveAttribute('href', '/sign-in')
    await user.click(cta)

    expect(router.state.location.pathname).toBe('/sign-in')
    expect(getMobileOverlay()).toHaveClass('pointer-events-none')
    expect(document.body.style.overflow).toBe('')
  })

  it('keeps the mobile overlay backdrop hidden from assistive technology', async () => {
    await renderHeader('/')
    await screen.findByRole('banner')

    expect(
      document.querySelector("[data-brand-backdrop='dawn']")
    ).toHaveAttribute('aria-hidden', 'true')
  })
})
