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
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { describe, expect, it } from 'vitest'

import { ForbiddenError } from '../forbidden'
import { GeneralError } from '../general-error'
import { MaintenanceError } from '../maintenance-error'
import { NotFoundError } from '../not-found-error'
import { UnauthorisedError } from '../unauthorized-error'

async function renderPage(page: ReactNode) {
  const router = createRouter({
    routeTree: createRootRoute({ component: () => page }),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  await router.load()
  return render(<RouterProvider router={router} />)
}

describe('error page status heading', () => {
  it.each([
    ['401', <UnauthorisedError key='401' />],
    ['403', <ForbiddenError key='403' />],
    ['404', <NotFoundError key='404' />],
    ['500', <GeneralError key='500' />],
    ['503', <MaintenanceError key='503' />],
  ])(
    'shows %s as the only page heading with a decorative halo behind it',
    async (code, page) => {
      const { container } = await renderPage(page)

      const heading = await screen.findByRole('heading', { level: 1 })
      expect(heading).toHaveTextContent(code)
      expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
      expect(container.querySelectorAll('.brand-glow')).toHaveLength(1)
      expect(container.querySelector('.brand-glow')).toHaveAttribute(
        'aria-hidden',
        'true'
      )
    }
  )

  it('renders no status code, halo or backdrop in minimal mode', async () => {
    const { container } = await renderPage(<GeneralError minimal />)

    expect(
      await screen.findByText(/Oops! Something went wrong/)
    ).toBeInTheDocument()
    expect(screen.queryByRole('heading', { level: 1 })).toBeNull()
    expect(container.querySelector('.brand-glow')).toBeNull()
    expect(container.querySelector('[data-brand-backdrop]')).toBeNull()
  })
})
