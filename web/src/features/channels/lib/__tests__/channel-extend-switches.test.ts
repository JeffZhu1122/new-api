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

type ExtendSwitch = 'force_retry' | 'empty_output_error'

function switchForm(name: ExtendSwitch, enabled: boolean) {
  return {
    ...CHANNEL_FORM_DEFAULT_VALUES,
    name: 'flaky upstream',
    type: 1,
    key: 'test-key',
    models: 'gpt-test',
    [name]: enabled,
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

// "Always Retry on Errors" and "Error on Empty Output" are boolean
// channel_extend switches with the same round trip.
describe.each<ExtendSwitch>(['force_retry', 'empty_output_error'])(
  'channel extend switch %s',
  (name) => {
    test('an enabled switch is sent in extend_config', () => {
      const { channel } = transformFormDataToCreatePayload(
        switchForm(name, true)
      )
      expect(channel.extend_config?.[name]).toBe(true)
    })

    test('a disabled switch is sent as false so the backend clears it', () => {
      const { channel } = transformFormDataToCreatePayload(
        switchForm(name, false)
      )
      expect(channel.extend_config?.[name]).toBe(false)
    })

    test('a stored switch is restored as enabled', () => {
      const defaults = transformChannelToFormDefaults(
        storedChannel({ [name]: true })
      )
      expect(defaults[name]).toBe(true)
    })

    test('a channel without extend_config restores the switch as disabled', () => {
      const defaults = transformChannelToFormDefaults(storedChannel(undefined))
      expect(defaults[name]).toBe(false)
    })

    test('an enabled switch marks the extra settings block as configured', () => {
      expect(
        getChannelConfigurationState(switchForm(name, true), {}, true).blocks
          .extraSettings
      ).toBe('configured')
    })
  }
)

// The asset library binding is a channel_extend account id; 0 means unbound.
describe('asset library account binding', () => {
  test('a bound account is sent in extend_config', () => {
    const { channel } = transformFormDataToCreatePayload({
      ...switchForm('force_retry', false),
      asset_library_account_id: 4,
    })
    expect(channel.extend_config?.asset_library_account_id).toBe(4)
  })

  test('an unbound channel sends 0 so the backend clears the binding', () => {
    const { channel } = transformFormDataToCreatePayload(
      switchForm('force_retry', false)
    )
    expect(channel.extend_config?.asset_library_account_id).toBe(0)
  })

  test('a stored binding is restored and marks the extra settings as configured', () => {
    const defaults = transformChannelToFormDefaults(
      storedChannel({ asset_library_account_id: 4 })
    )
    expect(defaults.asset_library_account_id).toBe(4)
    expect(
      getChannelConfigurationState(defaults, {}, true).blocks.extraSettings
    ).toBe('configured')
  })
})
