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

import { channelSchema } from '../../types'
import { getChannelConfigurationState } from '../channel-configuration'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
} from '../channel-form'

function retryForm(forceRetry: boolean) {
  return {
    ...CHANNEL_FORM_DEFAULT_VALUES,
    name: 'flaky upstream',
    type: 1,
    key: 'test-key',
    models: 'gpt-test',
    force_retry: forceRetry,
  }
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
    used_quota: 0,
    models: 'gpt-test',
    group: 'default',
    extend_config: extendConfig,
  })
}

describe('channel always-retry payload', () => {
  test('an enabled switch is sent in extend_config', () => {
    const { channel } = transformFormDataToCreatePayload(retryForm(true))
    expect(channel.extend_config?.force_retry).toBe(true)
  })

  test('a disabled switch is sent as false so the backend clears it', () => {
    const { channel } = transformFormDataToCreatePayload(retryForm(false))
    expect(channel.extend_config?.force_retry).toBe(false)
  })
})

describe('channel always-retry form defaults', () => {
  test('a stored switch is restored as enabled', () => {
    const defaults = transformChannelToFormDefaults(
      storedChannel({ force_retry: true })
    )
    expect(defaults.force_retry).toBe(true)
  })

  test('a channel without extend_config restores the switch as disabled', () => {
    const defaults = transformChannelToFormDefaults(storedChannel(undefined))
    expect(defaults.force_retry).toBe(false)
  })
})

describe('channel always-retry configuration state', () => {
  test('an enabled switch marks the extra settings block as configured', () => {
    expect(
      getChannelConfigurationState(retryForm(true), {}, true).blocks
        .extraSettings
    ).toBe('configured')
  })
})
