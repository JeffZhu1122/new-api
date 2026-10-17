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

import { buildSearchParams, getTokenRangeError } from '../filter'
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

  it('sends the input and output token bounds, a 0 bound included', () => {
    expect(
      params({ inputTokensMin: 0, inputTokensMax: 2000, outputTokensMin: 50 })
    ).toMatchObject({
      prompt_tokens_min: 0,
      prompt_tokens_max: 2000,
      completion_tokens_min: 50,
    })
    expect(params({ outputTokensMax: 10 }, { isAdmin: false })).toMatchObject({
      completion_tokens_max: 10,
    })
    expect(params({})).not.toHaveProperty('prompt_tokens_min')
  })

  it('turns the typed token bounds into URL numbers and drops empty boxes', () => {
    expect(
      buildSearchParams(
        {
          inputTokensMin: '1000',
          inputTokensMax: '',
          outputTokensMin: '0',
          outputTokensMax: '500',
        },
        'common'
      )
    ).toEqual({
      inputTokensMin: 1000,
      outputTokensMin: 0,
      outputTokensMax: 500,
    })
  })

  it.each([
    ['open bounds', {}, null],
    ['a full range', { inputTokensMin: '0', inputTokensMax: '0' }, null],
    [
      'input minimum above maximum',
      { inputTokensMin: '500', inputTokensMax: '100' },
      'The minimum token count cannot be greater than the maximum',
    ],
    [
      'output minimum above maximum',
      { outputTokensMin: '9', outputTokensMax: '8' },
      'The minimum token count cannot be greater than the maximum',
    ],
    [
      'a negative bound',
      { outputTokensMin: '-1' },
      'Token counts must be whole numbers of 0 or more',
    ],
    [
      'a fraction',
      { inputTokensMax: '1.5' },
      'Token counts must be whole numbers of 0 or more',
    ],
  ])('checks token bounds before searching: %s', (_, filters, expected) => {
    expect(getTokenRangeError(filters)).toBe(expected)
  })
})
