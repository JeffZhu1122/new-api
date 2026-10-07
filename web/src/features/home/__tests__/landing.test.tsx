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
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { STATUS_QUERY_KEY } from '@/lib/status-query'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { CTA, Features, Hero, HowItWorks, Stats } from '../components'

let client: QueryClient

beforeEach(() => {
  window.localStorage.clear()
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  // The landing only reads the shared /api/status cache; any other request
  // would mean a section started fetching data, which a restyle must not do.
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    throw new Error(`Unexpected landing request: ${url}`)
  })
})

afterEach(() => {
  client.clear()
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  window.localStorage.clear()
})

async function renderWithRouter(
  ui: ReactNode,
  status: Record<string, unknown> = {}
) {
  client.setQueryData(STATUS_QUERY_KEY, status)
  const router = createRouter({
    routeTree: createRootRoute({ component: () => ui }),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  await router.load()
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

function renderLanding(
  isAuthenticated: boolean,
  status?: Record<string, unknown>
) {
  return renderWithRouter(
    <>
      <Hero isAuthenticated={isAuthenticated} />
      <CTA isAuthenticated={isAuthenticated} />
    </>,
    status
  )
}

describe('landing sections', () => {
  it('links the signed-out hero and CTA actions to sign-up and pricing', async () => {
    await renderLanding(false)

    const getStarted = await screen.findAllByRole('button', {
      name: 'Get Started',
    })
    expect(getStarted.map((el) => el.getAttribute('href'))).toEqual([
      '/sign-up',
      '/sign-up',
    ])
    const viewPricing = screen.getAllByRole('button', { name: 'View Pricing' })
    expect(viewPricing.map((el) => el.getAttribute('href'))).toEqual([
      '/pricing',
      '/pricing',
    ])
  })

  it('opens an http docs_link in a new tab', async () => {
    await renderLanding(false, { docs_link: 'https://docs.example.com' })

    const docs = await screen.findByRole('button', { name: 'Docs' })
    expect(docs).toHaveAttribute('href', 'https://docs.example.com')
    expect(docs).toHaveAttribute('target', '_blank')
    expect(docs).toHaveAttribute('rel', 'noopener noreferrer')
  })

  it('renders a relative docs_link as an in-app router link', async () => {
    await renderLanding(false, { docs_link: '/docs' })

    const docs = await screen.findByRole('button', { name: 'Docs' })
    expect(docs).toHaveAttribute('href', '/docs')
    expect(docs).not.toHaveAttribute('target')
  })

  it('shows the dashboard action and hides the CTA band when signed in', async () => {
    await renderLanding(true)

    const dashboard = await screen.findByRole('button', {
      name: 'Go to Dashboard',
    })
    expect(dashboard).toHaveAttribute('href', '/dashboard')
    expect(
      screen.queryByRole('button', { name: 'Get Started' })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('heading', { name: /Ready to simplify/ })
    ).not.toBeInTheDocument()
    expect(
      document.querySelector("[data-brand-backdrop='band']")
    ).not.toBeInTheDocument()
  })

  it('keeps the hero and CTA backdrops hidden from assistive technology', async () => {
    await renderLanding(false)
    await screen.findByRole('heading', { level: 1 })

    const backdrops = [...document.querySelectorAll('[data-brand-backdrop]')]
    expect(
      backdrops.map((el) => el.getAttribute('data-brand-backdrop'))
    ).toEqual(['hero-center', 'band'])
    for (const backdrop of backdrops) {
      expect(backdrop).toHaveAttribute('aria-hidden', 'true')
    }
  })

  it('exposes the four terminal protocol tabs and switches the endpoint on click', async () => {
    const user = userEvent.setup()
    await renderWithRouter(<HowItWorks />)

    for (const name of ['Chat', 'Responses', 'Claude', 'Gemini']) {
      expect(await screen.findByRole('button', { name })).toBeVisible()
    }
    expect(screen.getByText('/v1/chat/completions')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Claude' }))
    expect(await screen.findByText('/v1/messages')).toBeInTheDocument()
  })

  it('centres the hero headline stack and keeps the stats readout inside the hero', async () => {
    await renderLanding(false)

    const heading = await screen.findByRole('heading', { level: 1 })
    expect(heading.parentElement).toHaveClass('items-center', 'text-center')

    const hero = heading.closest('section')
    if (!hero) throw new Error('Hero heading is not inside a section')
    for (const label of [
      'upstream services integrated',
      'model billing support',
      'compatible API routes',
      'scheduling controls',
    ]) {
      expect(within(hero).getByText(label)).toBeInTheDocument()
    }
    expect(hero.querySelector('.brand-horizon')).toHaveAttribute(
      'aria-hidden',
      'true'
    )
  })

  it('shows the API terminal beside the steps in the how-it-works section', async () => {
    await renderWithRouter(<HowItWorks />)

    const heading = await screen.findByRole('heading', {
      name: 'Three steps to get started',
    })
    const section = heading.closest('section')
    if (!section) {
      throw new Error('How-it-works heading is not inside a section')
    }
    expect(
      within(section).getByRole('button', { name: 'Chat' })
    ).toBeInTheDocument()
    for (const step of ['Configure', 'Connect', 'Monitor']) {
      expect(
        within(section).getByRole('heading', { level: 3, name: step })
      ).toBeInTheDocument()
    }
  })

  it('keeps CJK words together in the hero subtitle while still wrapping overlong runs', async () => {
    await renderLanding(false)

    const subtitle = await screen.findByText(
      'Access a vast selection of models via a standard, unified API protocol. Power AI applications, manage digital assets, and connect the Future.'
    )
    expect(subtitle).toHaveClass('break-keep', 'wrap-break-word')
  })

  it('lets the signed-out CTA actions wrap so long locales are not clipped by the band', async () => {
    await renderLanding(false)

    const ctaHeading = await screen.findByRole('heading', {
      name: /Ready to simplify/,
    })
    const band = ctaHeading.closest('section')
    if (!band) throw new Error('CTA heading is not inside a section')
    const getStarted = within(band).getByRole('button', { name: 'Get Started' })
    const viewPricing = within(band).getByRole('button', {
      name: 'View Pricing',
    })
    expect(getStarted.parentElement).toBe(viewPricing.parentElement)
    expect(getStarted.parentElement).toHaveClass('flex', 'flex-wrap')
  })

  it('lets the terminal footer metrics wrap and keeps the stream label on one line', async () => {
    await renderWithRouter(<HowItWorks />)

    const stream = await screen.findByText('stream · sse')
    expect(stream).toHaveClass('whitespace-nowrap')
    expect(stream.parentElement).toHaveClass('flex', 'flex-wrap')
  })

  it('sets the stat labels in the sans face so mixed CJK and Latin labels keep even spacing', async () => {
    await renderWithRouter(<Stats />)

    for (const name of [
      'upstream services integrated',
      'model billing support',
      'compatible API routes',
      'scheduling controls',
    ]) {
      expect(await screen.findByText(name)).toHaveClass('font-sans', 'text-xs')
    }
  })

  it('wraps the short centred step and feature captions at phrase boundaries so CJK words are not split', async () => {
    await renderWithRouter(
      <>
        <Features />
        <HowItWorks />
      </>
    )

    for (const caption of [
      'Pay-as-you-go with real-time usage monitoring',
      'Community driven, self-hosted, and extensible',
      'Add your API keys, set up channels and configure access permissions',
    ]) {
      const element = await screen.findByText(caption)
      // keep-all avoids mid-word CJK breaks; break-word is the overflow
      // fallback; pretty avoids a one-character last line.
      expect(element).toHaveClass(
        'break-keep',
        'wrap-break-word',
        'text-pretty'
      )
      expect(element).not.toHaveClass('text-balance')
    }
  })

  it('keeps each feature number before its title in reading order', async () => {
    await renderWithRouter(<Features />)

    const title = await screen.findByRole('heading', {
      level: 3,
      name: 'Lightning Fast',
    })
    const number = screen.getByText('01')

    expect(
      number.compareDocumentPosition(title) & Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
  })
})
