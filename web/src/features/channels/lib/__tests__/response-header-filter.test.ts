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
import { describe, expect, test } from 'vitest'

import { ERROR_MESSAGES } from '../../constants'
import { channelSchema } from '../../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  parseResponseHeaderNames,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
} from '../channel-form'

type FilterMode = 'off' | 'blacklist' | 'whitelist'

function filterForm(mode: FilterMode, headers: string) {
  return {
    ...CHANNEL_FORM_DEFAULT_VALUES,
    name: 'filtered upstream',
    type: 1,
    key: 'test-key',
    models: 'gpt-test',
    response_header_mode: mode,
    response_headers: headers,
  }
}

function responseHeaderIssues(values: ReturnType<typeof filterForm>): string[] {
  const result = channelFormSchema.safeParse(values)
  if (result.success) return []
  return result.error.issues
    .filter((issue) => issue.path[0] === 'response_headers')
    .map((issue) => issue.message)
}

function storedChannel(extendConfig: Record<string, unknown> | undefined) {
  return channelSchema.parse({
    id: 9,
    name: 'stored',
    type: 1,
    key: '',
    status: 1,
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
    models: 'gpt-test',
    group: 'default',
    extend_config: extendConfig,
  })
}

describe('parseResponseHeaderNames', () => {
  test('splits on newlines and commas, trims whitespace and drops blank entries', () => {
    expect(
      parseResponseHeaderNames(
        ' OpenAI-Organization \n\nSet-Cookie, X-Request-Id ,\n'
      )
    ).toEqual(['OpenAI-Organization', 'Set-Cookie', 'X-Request-Id'])
  })

  test('returns no names for undefined or blank input', () => {
    expect(parseResponseHeaderNames(undefined)).toEqual([])
    expect(parseResponseHeaderNames(' \n , ')).toEqual([])
  })
})

describe('response header filter validation', () => {
  test('off mode accepts an empty header list', () => {
    expect(responseHeaderIssues(filterForm('off', ''))).toEqual([])
  })

  test('blacklist mode with valid names passes', () => {
    expect(
      responseHeaderIssues(
        filterForm(
          'blacklist',
          'OpenAI-Organization\nx-ratelimit-limit-requests'
        )
      )
    ).toEqual([])
  })

  test('blacklist mode without any name reports the empty-list error', () => {
    expect(responseHeaderIssues(filterForm('blacklist', ' \n'))).toEqual([
      ERROR_MESSAGES.INVALID_CHANNEL_RESPONSE_HEADERS_EMPTY,
    ])
  })

  test('a header name containing a space is rejected', () => {
    expect(
      responseHeaderIssues(filterForm('whitelist', 'X Request Id'))
    ).toEqual([ERROR_MESSAGES.INVALID_CHANNEL_RESPONSE_HEADER_NAME])
  })

  test('duplicate names are rejected regardless of case', () => {
    expect(
      responseHeaderIssues(filterForm('whitelist', 'X-Foo\nx-foo'))
    ).toEqual([ERROR_MESSAGES.INVALID_CHANNEL_RESPONSE_HEADERS_DUPLICATE])
  })

  test('more than 64 names are rejected', () => {
    const names = Array.from({ length: 65 }, (_, i) => `X-Rule-${i}`).join('\n')
    expect(responseHeaderIssues(filterForm('blacklist', names))).toEqual([
      ERROR_MESSAGES.INVALID_CHANNEL_RESPONSE_HEADERS_TOO_MANY,
    ])
  })
})

describe('response header filter payload', () => {
  test('off mode omits the filter from extend_config even when names were typed', () => {
    const { channel } = transformFormDataToCreatePayload(
      filterForm('off', 'OpenAI-Organization')
    )
    expect(channel.extend_config?.response_header_mode).toBeUndefined()
    expect(channel.extend_config?.response_headers).toBeUndefined()
  })

  test('blacklist mode sends the mode together with the parsed header names', () => {
    const { channel } = transformFormDataToCreatePayload(
      filterForm('blacklist', 'OpenAI-Organization, Set-Cookie\n')
    )
    expect(channel.extend_config).toMatchObject({
      response_header_mode: 'blacklist',
      response_headers: ['OpenAI-Organization', 'Set-Cookie'],
    })
  })

  test('editing a filtered channel restores the mode and one name per line', () => {
    const defaults = transformChannelToFormDefaults(
      storedChannel({
        response_header_mode: 'whitelist',
        response_headers: ['X-Request-Id', 'Content-Type'],
      })
    )
    expect(defaults.response_header_mode).toBe('whitelist')
    expect(defaults.response_headers).toBe('X-Request-Id\nContent-Type')
  })

  test('editing a channel without a filter shows the filter as off', () => {
    const defaults = transformChannelToFormDefaults(storedChannel(undefined))
    expect(defaults.response_header_mode).toBe('off')
    expect(defaults.response_headers).toBe('')
  })
})
