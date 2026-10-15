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
import { describe, expect, it } from 'vitest'

import { buildApiParams, toModelNameSearch } from '../utils'

function params(
  searchParams: Record<string, unknown>,
  options: {
    isAdmin?: boolean
    columnFilters?: Array<{ id: string; value: unknown }>
  } = {}
) {
  return buildApiParams({
    page: 1,
    pageSize: 20,
    searchParams,
    columnFilters: options.columnFilters,
    isAdmin: options.isAdmin ?? true,
  })
}

describe('usage log filter params', () => {
  it.each([
    ['plain text matches any model containing it', 'claude', '%claude%'],
    ['surrounding spaces are dropped', '  4o-mini ', '%4o-mini%'],
    ['a value with % stays the user pattern', 'gpt-4o%', 'gpt-4o%'],
    ['empty text adds no filter', '   ', ''],
  ])('model search: %s', (_, input, expected) => {
    expect(toModelNameSearch(input)).toBe(expected)
  })

  it('sends the model search box as a contains match from the URL and column filters', () => {
    expect(params({ model: 'sonnet' }).model_name).toBe('%sonnet%')
    expect(
      params({}, { columnFilters: [{ id: 'model_name', value: 'haiku' }] })
        .model_name
    ).toBe('%haiku%')
  })

  it('passes several channel IDs on to the server unchanged', () => {
    expect(params({ channel: ' 1, 3 7 ' }).channel).toBe('1, 3 7')
    expect(
      params({}, { columnFilters: [{ id: 'channel', value: '4,5' }] }).channel
    ).toBe('4,5')
  })

  it('never sends a channel filter from the self view', () => {
    expect(params({ channel: '1,2' }, { isAdmin: false })).not.toHaveProperty(
      'channel'
    )
  })
})
