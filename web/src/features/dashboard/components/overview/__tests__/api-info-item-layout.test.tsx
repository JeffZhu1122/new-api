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
import { describe, expect, it } from 'vitest'

import { TooltipProvider } from '@/components/ui/tooltip'

import { ApiInfoItemComponent } from '../api-info-item'

const item = {
  url: 'https://api.example.com/v1/a-very-long-endpoint-path-for-overflow',
  route: '主线路',
  description: '全球加速，适用于海外和跨境访问的长描述文本',
  color: 'blue',
}

function renderItem() {
  render(
    <TooltipProvider>
      <ApiInfoItemComponent
        item={item}
        status={{ latency: null, testing: false, error: false }}
        onTest={() => {}}
      />
    </TooltipProvider>
  )
}

describe('ApiInfoItemComponent layout', () => {
  it('keeps the route name whole instead of letting a long description squeeze it', () => {
    renderItem()

    const route = screen.getByText(item.route)

    expect(route).toHaveClass('shrink-0', 'max-w-full', 'break-keep')
    // Admin-configured route names must stay fully readable: they wrap at
    // word boundaries when too long, never get cut off with an ellipsis.
    expect(route).not.toHaveClass('truncate')
  })

  it('lets the description and URL absorb overflow by truncating', () => {
    renderItem()

    const description = screen.getByText(item.description)
    const url = screen.getByText(item.url)

    expect(description).toHaveClass('min-w-0', 'truncate')
    expect(description.parentElement).toHaveClass('min-w-0')
    expect(url).toHaveClass('truncate')
  })
})
