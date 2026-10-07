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
import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { LoadingSkeleton } from '../components/loading-skeleton'
import { SearchBar } from '../components/search-bar'
import { VIEW_MODES } from '../constants'

describe('pricing loading skeleton', () => {
  it('reserves one slot with the search bar height and radius so the header does not shift', () => {
    render(<SearchBar value='' onChange={vi.fn()} onClear={vi.fn()} />)
    const searchInput = screen.getByRole('textbox', { name: 'Search models' })
    const { container } = render(<LoadingSkeleton />)

    const searchSlots = container.querySelectorAll(
      '[data-slot="skeleton"].h-12'
    )

    expect(searchInput).toHaveClass('h-12', 'rounded-2xl')
    expect(searchSlots).toHaveLength(1)
    expect(searchSlots[0]).toHaveClass('rounded-2xl')
  })

  it('marks the region busy and shows six model card placeholders in card view', () => {
    const { container } = render(<LoadingSkeleton viewMode={VIEW_MODES.CARD} />)

    expect(container.firstElementChild).toHaveAttribute('aria-busy', 'true')
    expect(container.querySelectorAll('[data-slot="card"]')).toHaveLength(6)
  })

  it('shows row placeholders instead of model cards in table view', () => {
    const { container } = render(
      <LoadingSkeleton viewMode={VIEW_MODES.TABLE} />
    )

    expect(container.firstElementChild).toHaveAttribute('aria-busy', 'true')
    expect(container.querySelectorAll('[data-slot="card"]')).toHaveLength(0)
  })
})
