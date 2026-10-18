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

import { buildSearchParams, getMetricFilterError } from '../filter'
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
    ['a TTFT fraction of a second', { ttftMin: '0.5', ttftMax: '2.5' }, null],
    [
      'a fraction of a second of use time',
      { useTimeMin: '2.5' },
      'Use time must be whole seconds of 0 or more',
    ],
    [
      'use time minimum above maximum',
      { useTimeMin: '30', useTimeMax: '3' },
      'The minimum cannot be greater than the maximum',
    ],
    ['a negative TTFT', { ttftMax: '-0.1' }, 'TTFT must be 0 seconds or more'],
    [
      'TTFT minimum above maximum',
      { ttftMin: '3', ttftMax: '1.5' },
      'The minimum cannot be greater than the maximum',
    ],
    ['known status codes', { statusCodes: ['429', '502'] }, null],
    [
      'a status code outside 100-599',
      { statusCodes: ['429', '99'] },
      'Status codes must be numbers from 100 to 599',
    ],
    [
      'a status code that is not a number',
      { statusCodes: ['timeout'] },
      'Status codes must be numbers from 100 to 599',
    ],
  ])('checks metric filters before searching: %s', (_, filters, expected) => {
    expect(getMetricFilterError(filters)).toBe(expected)
  })

  it('sends use time in seconds, TTFT in milliseconds and status codes as a list', () => {
    expect(
      params({
        useTimeMin: 5,
        useTimeMax: 60,
        ttftMin: 1.5,
        ttftMax: 0.25,
        statusCodes: [429, 502],
      })
    ).toMatchObject({
      use_time_min: 5,
      use_time_max: 60,
      ttft_ms_min: 1500,
      ttft_ms_max: 250,
      status_codes: '429,502',
    })
    expect(params({ statusCodes: [] })).not.toHaveProperty('status_codes')
  })

  it('turns the typed durations and picked status codes into URL values', () => {
    expect(
      buildSearchParams(
        {
          useTimeMin: '0',
          useTimeMax: '',
          ttftMax: '2.5',
          statusCodes: ['429', '524'],
        },
        'common'
      )
    ).toEqual({ useTimeMin: 0, ttftMax: 2.5, statusCodes: [429, 524] })
    expect(buildSearchParams({ statusCodes: [] }, 'common')).toEqual({})
  })

  it.each([
    ['stream', 'true'],
    ['non_stream', 'false'],
  ])('sends the stream mode %s as is_stream=%s', (stream, expected) => {
    expect(params({ stream })).toMatchObject({ is_stream: expected })
  })

  it('sends no stream mode for either mode or an unknown value', () => {
    expect(params({})).not.toHaveProperty('is_stream')
    expect(params({ stream: 'sometimes' })).not.toHaveProperty('is_stream')
  })

  it('keeps the picked stream mode in the URL', () => {
    expect(buildSearchParams({ stream: 'non_stream' }, 'common')).toEqual({
      stream: 'non_stream',
    })
  })
})
