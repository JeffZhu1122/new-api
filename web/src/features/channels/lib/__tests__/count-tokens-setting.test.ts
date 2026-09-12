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

import {
  CHANNEL_FORM_DEFAULT_VALUES,
  transformFormDataToCreatePayload,
} from '../channel-form'

const CHANNEL_TYPE_OPENAI = 1
const CHANNEL_TYPE_AZURE = 3
const CHANNEL_TYPE_ANTHROPIC = 14

function countTokensForm(type: number, enabled: boolean) {
  return {
    ...CHANNEL_FORM_DEFAULT_VALUES,
    name: 'count tokens channel',
    type,
    key: 'test-key',
    models: 'test-model',
    count_tokens_enabled: enabled,
  }
}

function parseSettings(payload: {
  channel: { settings?: string | null }
}): Record<string, unknown> {
  return JSON.parse(payload.channel.settings ?? '{}') as Record<string, unknown>
}

describe('count_tokens_enabled channel setting', () => {
  test('keeps count_tokens_enabled for an OpenAI channel when switched on', () => {
    const payload = transformFormDataToCreatePayload(
      countTokensForm(CHANNEL_TYPE_OPENAI, true)
    )

    expect(parseSettings(payload).count_tokens_enabled).toBe(true)
  })

  test('keeps count_tokens_enabled for an Anthropic channel when switched on', () => {
    const payload = transformFormDataToCreatePayload(
      countTokensForm(CHANNEL_TYPE_ANTHROPIC, true)
    )

    expect(parseSettings(payload).count_tokens_enabled).toBe(true)
  })

  test('drops count_tokens_enabled for a channel type without a token counting endpoint', () => {
    const payload = transformFormDataToCreatePayload(
      countTokensForm(CHANNEL_TYPE_AZURE, true)
    )

    expect(parseSettings(payload)).not.toHaveProperty('count_tokens_enabled')
  })
})
