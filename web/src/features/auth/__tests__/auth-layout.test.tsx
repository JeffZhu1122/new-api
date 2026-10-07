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
import { cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { useSystemConfigStore } from '@/stores/system-config-store'

import { AuthLayout } from '../auth-layout'

let client: QueryClient

function AuthScreenFixture() {
  return (
    <AuthLayout>
      <h2>Sign in</h2>
      <button type='button'>Submit sign-in</button>
    </AuthLayout>
  )
}

async function renderAuthLayout() {
  const router = createRouter({
    routeTree: createRootRoute({ component: AuthScreenFixture }),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  await router.load()
  const view = render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  await screen.findByRole('heading', { level: 2, name: 'Sign in' })
  return view
}

function getAuthCard(): HTMLElement {
  const card = document.querySelector<HTMLElement>("[data-slot='auth-card']")
  if (!card) throw new Error('auth card was not rendered')
  return card
}

function getBrandPanel(): HTMLElement {
  const panel = document.querySelector<HTMLElement>(
    "[data-slot='auth-brand-panel']"
  )
  if (!panel) throw new Error('brand panel was not rendered')
  return panel
}

function seedLoadedSystemConfig() {
  useSystemConfigStore.setState({
    loading: false,
    config: {
      ...useSystemConfigStore.getState().config,
      systemName: 'Aurora Gateway',
      logo: '/brand-logo.png',
    },
  })
}

beforeEach(() => {
  window.localStorage.clear()
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
})

afterEach(() => {
  cleanup()
  client.clear()
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  window.localStorage.clear()
})

describe('AuthLayout', () => {
  it('renders the auth screen inside the auth card', async () => {
    await renderAuthLayout()

    const card = getAuthCard()
    expect(
      within(card).getByRole('heading', { level: 2, name: 'Sign in' })
    ).toBeInTheDocument()
    expect(
      within(card).getByRole('button', { name: 'Submit sign-in' })
    ).toBeInTheDocument()
  })

  it('links the loaded logo and system name back to the home page', async () => {
    seedLoadedSystemConfig()
    await renderAuthLayout()

    const home = screen.getByRole('link', { name: /Aurora Gateway/ })
    expect(home).toHaveAttribute('href', '/')
    expect(
      within(home).getByRole('heading', { level: 1, name: 'Aurora Gateway' })
    ).toBeInTheDocument()
    expect(within(home).getByRole('img', { name: 'Logo' })).toHaveAttribute(
      'src',
      '/brand-logo.png'
    )
  })

  it('shows skeletons instead of the logo and name while config loads', async () => {
    await renderAuthLayout()

    const home = screen.getByRole('link')
    expect(home).toHaveAttribute('href', '/')
    expect(home.querySelectorAll("[data-slot='skeleton']")).toHaveLength(2)
    expect(screen.queryByRole('img', { name: 'Logo' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { level: 1 })).not.toBeInTheDocument()
  })

  it('places the brand panel after the auth card so the form is reached first', async () => {
    await renderAuthLayout()

    const position = getAuthCard().compareDocumentPosition(getBrandPanel())
    expect(position & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(position & Node.DOCUMENT_POSITION_CONTAINED_BY).toBe(0)
  })

  it('keeps the brand panel heading-free and shown only from the lg breakpoint', async () => {
    seedLoadedSystemConfig()
    await renderAuthLayout()

    const panel = getBrandPanel()
    expect(panel).toHaveClass('hidden', 'lg:flex')
    expect(
      within(panel).queryAllByRole('heading', { hidden: true })
    ).toHaveLength(0)
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
  })

  it('hides the decorative brand panel from assistive technology without adding a landmark', async () => {
    seedLoadedSystemConfig()
    await renderAuthLayout()

    const panel = getBrandPanel()
    expect(panel).toHaveAttribute('aria-hidden', 'true')
    expect(
      panel.querySelectorAll('a, button, input, select, textarea, [tabindex]')
    ).toHaveLength(0)
    expect(screen.queryByRole('complementary')).not.toBeInTheDocument()
  })

  it('caps the auth card at the 480px measure and pretty-wraps its paragraphs', async () => {
    await renderAuthLayout()

    const card = getAuthCard()
    expect(card).toHaveClass('w-full', 'max-w-[480px]', 'sm:p-8')
    expect(card).toHaveClass('[:where(&_p)]:text-pretty')
  })

  it('hides every decorative brand backdrop from assistive technology', async () => {
    const { container } = await renderAuthLayout()

    // One static page backdrop behind the card, one inside the brand panel.
    const backdrops = container.querySelectorAll('[data-brand-backdrop]')
    expect(backdrops).toHaveLength(2)
    for (const backdrop of backdrops) {
      expect(backdrop).toHaveAttribute('aria-hidden', 'true')
    }
  })
})
