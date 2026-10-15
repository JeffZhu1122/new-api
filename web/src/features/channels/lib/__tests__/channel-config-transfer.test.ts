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

import { encodeChannelConnectionInfo } from '@/lib/channel-connection-info'

import { CHANNEL_STATUS } from '../../constants'
import type { Channel } from '../../types'
import {
  channelConfigToFormValues,
  encodeChannelConfig,
  parseChannelConfig,
} from '../channel-config-transfer'

function sourceChannel(overrides: Partial<Channel> = {}): Channel {
  return {
    id: 42,
    type: 1,
    key: 'sk-source',
    openai_organization: null,
    test_model: 'gpt-4o-mini',
    status: CHANNEL_STATUS.ENABLED,
    name: 'Upstream A',
    weight: 3,
    created_time: 1700000000,
    test_time: 1700000100,
    response_time: 820,
    base_url: 'https://upstream.example.com',
    other: '',
    balance: 12.5,
    balance_updated_time: 1700000200,
    models: 'gpt-4o,gpt-4o-mini',
    group: 'default,vip',
    used_quota: 99999,
    model_mapping: '{"gpt-4o":"gpt-4o-2024-08-06"}',
    status_code_mapping: null,
    priority: 5,
    auto_ban: 1,
    other_info: '',
    tag: 'team-a',
    setting: '{"proxy":"http://proxy.internal:8080"}',
    param_override: null,
    header_override: null,
    remark: 'primary',
    max_input_tokens: 0,
    channel_info: {
      is_multi_key: false,
      multi_key_size: 0,
      multi_key_polling_index: 0,
      multi_key_mode: 'random',
    },
    settings: '{}',
    extend_config: { force_retry: true, rpm_limit: 60 },
    ...overrides,
  }
}

function roundTrip(channel: Channel) {
  const pasted = parseChannelConfig(encodeChannelConfig(channel))
  if (!pasted) throw new Error('a copied configuration must parse')
  return channelConfigToFormValues(pasted)
}

describe('channel configuration transfer', () => {
  it('fills the create form with every source setting and starts the copy disabled', () => {
    expect(roundTrip(sourceChannel())).toMatchObject({
      name: 'Upstream A',
      type: 1,
      key: 'sk-source',
      base_url: 'https://upstream.example.com',
      models: 'gpt-4o,gpt-4o-mini',
      group: ['default', 'vip'],
      model_mapping: '{"gpt-4o":"gpt-4o-2024-08-06"}',
      priority: 5,
      weight: 3,
      tag: 'team-a',
      remark: 'primary',
      test_model: 'gpt-4o-mini',
      proxy: 'http://proxy.internal:8080',
      force_retry: true,
      rpm_limit: 60,
      status: CHANNEL_STATUS.MANUAL_DISABLED,
      multi_key_mode: 'single',
    })
  })

  it('copies the raw Go model the config endpoint returns, zero values included', () => {
    // Shape of a real /api/channel/:id/config response for a single-key
    // channel: Go nil pointers and empty strings rather than frontend defaults.
    const fromServer = {
      ...sourceChannel(),
      remark: null,
      settings: '',
      status_code_mapping: '',
      tag: null,
      channel_info: {
        is_multi_key: false,
        multi_key_size: 0,
        multi_key_status_list: null,
        multi_key_polling_index: 0,
        multi_key_mode: '',
      },
      extend_config: {
        force_retry: true,
        schedule: { timezone: 'Asia/Shanghai', windows: null },
      },
    } as unknown as Channel
    expect(roundTrip(fromServer)).toMatchObject({
      name: 'Upstream A',
      key: 'sk-source',
      remark: '',
      force_retry: true,
      multi_key_mode: 'single',
      schedule_enabled: true,
      schedule_windows: [],
    })
  })

  it('leaves the source site identity, health, balance and key status out of the copied text', () => {
    const text = encodeChannelConfig(
      sourceChannel({
        channel_info: {
          is_multi_key: true,
          multi_key_size: 2,
          multi_key_status_list: { '1': 3 },
          multi_key_polling_index: 1,
          multi_key_mode: 'polling',
        },
      })
    )
    const copied = JSON.parse(text).channel
    for (const field of [
      'id',
      'status',
      'created_time',
      'test_time',
      'response_time',
      'balance',
      'balance_updated_time',
      'used_quota',
    ]) {
      expect(copied).not.toHaveProperty(field)
    }
    expect(copied.channel_info).toEqual({
      is_multi_key: true,
      multi_key_size: 2,
      multi_key_polling_index: 0,
      multi_key_mode: 'polling',
    })
  })

  it('recreates a multi-key channel as one multi-key channel holding every key', () => {
    const values = roundTrip(
      sourceChannel({
        key: 'sk-first\nsk-second',
        channel_info: {
          is_multi_key: true,
          multi_key_size: 2,
          multi_key_polling_index: 0,
          multi_key_mode: 'polling',
        },
      })
    )
    expect(values).toMatchObject({
      key: 'sk-first\nsk-second',
      multi_key_mode: 'multi_to_single',
      multi_key_type: 'polling',
    })
  })

  it('turns the Vertex AI service account keys of a multi-key channel into one JSON array', () => {
    const values = roundTrip(
      sourceChannel({
        type: 41,
        key: '{"project_id":"a"}\n{"project_id":"b"}',
        settings: '{"vertex_key_type":"json"}',
        channel_info: {
          is_multi_key: true,
          multi_key_size: 2,
          multi_key_polling_index: 0,
          multi_key_mode: 'random',
        },
      })
    )
    expect(JSON.parse(values.key)).toEqual([
      { project_id: 'a' },
      { project_id: 'b' },
    ])
  })

  it.each([
    ['empty text', ''],
    ['plain text', 'sk-not-a-config'],
    [
      'connection info',
      encodeChannelConnectionInfo('sk-x', 'https://example.com'),
    ],
    [
      'a newer format version',
      JSON.stringify({
        _type: 'newapi_channel_config',
        version: 2,
        channel: { type: 1, name: 'x', key: 'sk' },
      }),
    ],
    [
      'a configuration without a key',
      JSON.stringify({
        _type: 'newapi_channel_config',
        version: 1,
        channel: { type: 1, name: 'x' },
      }),
    ],
  ])('rejects %s', (_, text) => {
    expect(parseChannelConfig(text)).toBeNull()
  })
})
