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
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { useSystemConfigStore } from '@/stores/system-config-store'

import { AuthBrandPanel } from '../components/auth-brand-panel'

// jsdom has no layout, so these tests pin the CSS contracts that keep the
// orbit hub clear of the copy block; pixel checks happen in browser QA.
const DESCRIPTION_KEY =
  'Access a vast selection of models via a standard, unified API protocol. Power AI applications, manage digital assets, and connect the Future.'

let client: QueryClient

function renderPanel() {
  return render(
    <QueryClientProvider client={client}>
      <AuthBrandPanel />
    </QueryClientProvider>
  )
}

function getSlot(container: HTMLElement, slot: string): HTMLElement {
  const element = container.querySelector<HTMLElement>(`[data-slot='${slot}']`)
  if (!element) throw new Error(`${slot} was not rendered`)
  return element
}

beforeEach(() => {
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
})

afterEach(() => {
  cleanup()
  client.clear()
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
})

describe('AuthBrandPanel', () => {
  it('lays the orbit hub out in the free space above the copy instead of over it', () => {
    const { container } = renderPanel()

    const hub = getSlot(container, 'auth-brand-hub')
    const copy = getSlot(container, 'auth-brand-copy')
    expect(hub).toHaveAttribute('aria-hidden', 'true')
    expect(hub).toHaveClass('@container-size', 'flex-1', 'min-h-0')
    expect(hub).not.toHaveClass('absolute')
    expect(hub.contains(copy)).toBe(false)
    expect(
      hub.compareDocumentPosition(copy) & Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
  })

  it('drops the orbit when the free height above the copy is too short', () => {
    const { container } = renderPanel()

    const orbit = getSlot(container, 'auth-brand-orbit')
    expect(orbit).toHaveClass('[@container(max-height:255px)]:hidden')
    expect(orbit).toHaveTextContent('Gemini')
  })

  it('gives the display line the wide copy column while the description keeps its reading measure', () => {
    const { container } = renderPanel()

    expect(getSlot(container, 'auth-brand-copy')).toHaveClass('max-w-xl')
    expect(screen.getByText(DESCRIPTION_KEY)).toHaveClass('max-w-md')
  })

  it('wraps the description at CJK punctuation and spaces with an overflow fallback', () => {
    renderPanel()

    expect(screen.getByText(DESCRIPTION_KEY)).toHaveClass(
      'break-keep',
      'wrap-anywhere',
      'text-pretty'
    )
  })
})
