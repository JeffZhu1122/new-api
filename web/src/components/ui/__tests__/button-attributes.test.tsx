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
import { describe, expect, test } from 'vitest'

import { Button } from '../button'

// data-variant / data-size are the CSS hook contract used by
// styles/brand.css (tactile default buttons, auth card control sizing).
describe('Button data attributes', () => {
  test('a button without variant or size exposes the default hooks', () => {
    render(<Button>Save</Button>)

    const button = screen.getByRole('button', { name: 'Save' })
    expect(button).toHaveAttribute('data-slot', 'button')
    expect(button).toHaveAttribute('data-variant', 'default')
    expect(button).toHaveAttribute('data-size', 'default')
  })

  test('an explicit variant and size are reflected in the hooks', () => {
    render(
      <Button variant='outline' size='sm'>
        Filter
      </Button>
    )

    const button = screen.getByRole('button', { name: 'Filter' })
    expect(button).toHaveAttribute('data-variant', 'outline')
    expect(button).toHaveAttribute('data-size', 'sm')
  })

  test('a button rendered as a link keeps the button role and the hooks', () => {
    render(<Button render={<a href='/x' />}>Docs</Button>)

    const link = screen.getByRole('button', { name: 'Docs' })
    expect(link.tagName).toBe('A')
    expect(link).toHaveAttribute('href', '/x')
    expect(link).toHaveAttribute('data-variant', 'default')
    expect(link).toHaveAttribute('data-size', 'default')
  })
})
