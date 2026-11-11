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
import { assert, describe, expect, test } from 'vitest'

import { ERROR_MESSAGES } from '../../constants'
import { channelSchema } from '../../types'
import {
  buildSettingJSON,
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  transformChannelToFormDefaults,
  transformFormDataToUpdatePayload,
  type ChannelFormValues,
} from '../channel-form'

const validForm: ChannelFormValues = {
  ...CHANNEL_FORM_DEFAULT_VALUES,
  name: 'upstream',
  type: 1,
  key: 'sk-test',
  models: 'gpt-test',
  group: ['default'],
}

function storedChannel(setting: Record<string, unknown>) {
  return channelSchema.parse({
    id: 9,
    name: 'upstream',
    key: '',
    type: 1,
    status: 1,
    created_time: 0,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
    setting: JSON.stringify(setting),
  })
}

function issuesOf(values: Partial<ChannelFormValues>) {
  const result = channelFormSchema.safeParse({ ...validForm, ...values })
  if (result.success) return []
  return result.error.issues.map((issue) => ({
    field: issue.path.join('.'),
    message: issue.message,
  }))
}

describe('channel fingerprint, HTTP/3 and test request settings', () => {
  test('a channel that sets none of them saves a setting without them', () => {
    expect(issuesOf({})).toEqual([])
    const setting = JSON.parse(buildSettingJSON(validForm))
    expect(setting).not.toHaveProperty('http_protocol')
    expect(setting).not.toHaveProperty('fingerprint_profile')
    expect(setting).not.toHaveProperty('test_message')
    expect(setting).not.toHaveProperty('test_max_tokens')
  })

  test('opening and saving a channel keeps its stored fingerprint profile and test request', () => {
    const channel = storedChannel({
      fingerprint_profile: 'chrome-133',
      test_message: 'Reply with the word pong.',
      test_max_tokens: 64,
    })

    const values = transformChannelToFormDefaults(channel)
    expect(values.fingerprint_profile).toBe('chrome-133')
    expect(values.test_message).toBe('Reply with the word pong.')
    expect(values.test_max_tokens).toBe(64)

    const payload = transformFormDataToUpdatePayload(values, channel.id)
    assert(typeof payload.setting === 'string')
    expect(JSON.parse(payload.setting)).toMatchObject({
      fingerprint_profile: 'chrome-133',
      test_message: 'Reply with the word pong.',
      test_max_tokens: 64,
    })
  })

  test('a channel stored with HTTP/3 opens as HTTP/3 and saves it back with one connection shard', () => {
    const channel = storedChannel({
      http_protocol: 'http3',
      http2_connection_shards: 4,
    })

    const values = transformChannelToFormDefaults(channel)
    expect(values.http_protocol).toBe('http3')
    expect(values.http2_connection_shards).toBe(1)

    const setting = JSON.parse(buildSettingJSON(values))
    expect(setting.http_protocol).toBe('http3')
    expect(setting).not.toHaveProperty('http2_connection_shards')
  })

  test('a test message of only whitespace is saved as no test message', () => {
    const setting = JSON.parse(
      buildSettingJSON({ ...validForm, test_message: '  \n ' })
    )
    expect(setting).not.toHaveProperty('test_message')
  })

  test('HTTP/3 is refused together with a proxy, a fingerprint profile or several connection shards', () => {
    expect(
      issuesOf({ http_protocol: 'http3', proxy: 'http://127.0.0.1:8080' })
    ).toEqual([
      {
        field: 'http_protocol',
        message: ERROR_MESSAGES.INVALID_HTTP3_WITH_PROXY,
      },
    ])
    expect(
      issuesOf({ http_protocol: 'http3', fingerprint_profile: 'chrome-133' })
    ).toEqual([
      {
        field: 'fingerprint_profile',
        message: ERROR_MESSAGES.INVALID_HTTP3_WITH_FINGERPRINT,
      },
    ])
    expect(
      issuesOf({ http_protocol: 'http3', http2_connection_shards: 2 })
    ).toEqual([
      {
        field: 'http2_connection_shards',
        message: ERROR_MESSAGES.INVALID_HTTP3_WITH_SHARDS,
      },
    ])
    expect(issuesOf({ http_protocol: 'http3' })).toEqual([])
  })

  test('a fingerprint profile works with a proxy and with HTTP/1.1', () => {
    expect(
      issuesOf({
        http_protocol: 'http1',
        proxy: 'http://127.0.0.1:8080',
        fingerprint_profile: 'chrome-133',
      })
    ).toEqual([])
  })

  test('the test request is bounded like the gateway bounds it', () => {
    expect(issuesOf({ test_message: 'a'.repeat(4096) })).toEqual([])
    // 1366 three-byte characters are 4098 bytes.
    expect(issuesOf({ test_message: '测'.repeat(1366) })).toEqual([
      {
        field: 'test_message',
        message: ERROR_MESSAGES.INVALID_TEST_MESSAGE_LENGTH,
      },
    ])
    expect(issuesOf({ test_max_tokens: 65536 })).toEqual([])
    for (const value of [-1, 65537]) {
      expect(issuesOf({ test_max_tokens: value })).toEqual([
        {
          field: 'test_max_tokens',
          message: ERROR_MESSAGES.INVALID_TEST_MAX_TOKENS,
        },
      ])
    }
  })
})
