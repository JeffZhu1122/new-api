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
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { SearchBar } from '../components/search-bar'

describe('pricing search bar', () => {
  it('focuses the search input on Ctrl+K and Meta+K', async () => {
    const user = userEvent.setup()
    render(<SearchBar value='' onChange={vi.fn()} onClear={vi.fn()} />)
    const input = screen.getByRole('textbox', { name: 'Search models' })

    await user.keyboard('{Control>}k{/Control}')
    expect(input).toHaveFocus()

    input.blur()
    await user.keyboard('{Meta>}k{/Meta}')
    expect(input).toHaveFocus()
  })

  it('blurs the focused search input on Escape', async () => {
    const user = userEvent.setup()
    render(<SearchBar value='' onChange={vi.fn()} onClear={vi.fn()} />)
    const input = screen.getByRole('textbox', { name: 'Search models' })

    await user.click(input)
    await user.keyboard('{Escape}')

    expect(input).not.toHaveFocus()
  })

  it('reports typed text through onChange', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(<SearchBar value='' onChange={onChange} onClear={vi.fn()} />)
    const input = screen.getByRole('textbox', { name: 'Search models' })

    await user.type(input, 'g')

    expect(onChange).toHaveBeenCalledWith('g')
  })

  it('shows the shortcut hint instead of a clear button when empty', () => {
    render(<SearchBar value='' onChange={vi.fn()} onClear={vi.fn()} />)

    expect(screen.getByText('⌘K')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Clear search' })).toBeNull()
  })

  it('reserves the wide shortcut-hint padding only from sm, where the hint is shown', () => {
    render(<SearchBar value='' onChange={vi.fn()} onClear={vi.fn()} />)
    const input = screen.getByRole('textbox', { name: 'Search models' })

    expect(screen.getByText('⌘K')).toHaveClass('hidden', 'sm:inline-block')
    expect(input).toHaveClass('pr-11', 'sm:pr-16')
    expect(input).not.toHaveClass('pr-16')
  })

  it('clears the search through the clear button when it has a value', async () => {
    const user = userEvent.setup()
    const onClear = vi.fn()
    render(<SearchBar value='gpt' onChange={vi.fn()} onClear={onClear} />)

    expect(screen.queryByText('⌘K')).toBeNull()
    await user.click(screen.getByRole('button', { name: 'Clear search' }))

    expect(onClear).toHaveBeenCalledTimes(1)
  })
})
