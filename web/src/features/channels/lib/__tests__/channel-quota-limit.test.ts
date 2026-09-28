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
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
} from '../channel-form'

// The default currency config is USD with 500,000 quota units per dollar.
const QUOTA_PER_UNIT = 500000

function limitForm(overrides: {
  cost_ratio?: number
  quota_limit_amount?: number
}) {
  return {
    ...CHANNEL_FORM_DEFAULT_VALUES,
    name: 'prepaid upstream',
    type: 1,
    key: 'test-key',
    models: 'gpt-test',
    ...overrides,
  }
}

function issuesOn(
  values: ReturnType<typeof limitForm>,
  field: 'cost_ratio' | 'quota_limit_amount'
): string[] {
  const result = channelFormSchema.safeParse(values)
  if (result.success) return []
  return result.error.issues
    .filter((issue) => issue.path[0] === field)
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
    used_quota: 3 * QUOTA_PER_UNIT,
    models: 'gpt-test',
    group: 'default',
    extend_config: extendConfig,
  })
}

describe('channel cost ratio validation', () => {
  test('zero and fractional ratios pass', () => {
    expect(issuesOn(limitForm({ cost_ratio: 0 }), 'cost_ratio')).toEqual([])
    expect(issuesOn(limitForm({ cost_ratio: 0.2 }), 'cost_ratio')).toEqual([])
  })

  test('a negative ratio reports the cost ratio error', () => {
    expect(issuesOn(limitForm({ cost_ratio: -0.1 }), 'cost_ratio')).toEqual([
      ERROR_MESSAGES.INVALID_CHANNEL_COST_RATIO,
    ])
  })

  test('a ratio above 1000 reports the cost ratio error', () => {
    expect(issuesOn(limitForm({ cost_ratio: 1000.5 }), 'cost_ratio')).toEqual([
      ERROR_MESSAGES.INVALID_CHANNEL_COST_RATIO,
    ])
  })
})

describe('channel quota limit validation', () => {
  test('zero and positive amounts pass', () => {
    expect(
      issuesOn(limitForm({ quota_limit_amount: 0 }), 'quota_limit_amount')
    ).toEqual([])
    expect(
      issuesOn(limitForm({ quota_limit_amount: 12.5 }), 'quota_limit_amount')
    ).toEqual([])
  })

  test('a negative amount reports the quota limit error', () => {
    expect(
      issuesOn(limitForm({ quota_limit_amount: -1 }), 'quota_limit_amount')
    ).toEqual([ERROR_MESSAGES.INVALID_CHANNEL_QUOTA_LIMIT])
  })

  test('an amount beyond the safe integer range of quota units is rejected', () => {
    expect(
      issuesOn(
        limitForm({ quota_limit_amount: Number.MAX_SAFE_INTEGER }),
        'quota_limit_amount'
      )
    ).toEqual([ERROR_MESSAGES.INVALID_CHANNEL_QUOTA_LIMIT])
  })
})

describe('channel quota limit payload', () => {
  test('an amount entered in the display currency is sent as quota units', () => {
    const { channel } = transformFormDataToCreatePayload(
      limitForm({ cost_ratio: 0.2, quota_limit_amount: 10 })
    )
    expect(channel.extend_config).toMatchObject({
      cost_ratio: 0.2,
      quota_limit: 10 * QUOTA_PER_UNIT,
    })
  })

  test('unset fields are sent as zero so the backend clears them', () => {
    const { channel } = transformFormDataToCreatePayload(limitForm({}))
    expect(channel.extend_config?.cost_ratio).toBe(0)
    expect(channel.extend_config?.quota_limit).toBe(0)
  })
})

describe('channel quota limit form defaults', () => {
  test('stored quota units are restored as an editable display amount', () => {
    const defaults = transformChannelToFormDefaults(
      storedChannel({ cost_ratio: 0.2, quota_limit: 10 * QUOTA_PER_UNIT })
    )
    expect(defaults.cost_ratio).toBe(0.2)
    expect(defaults.quota_limit_amount).toBe(10)
  })

  test('a channel without extend config shows both fields as zero', () => {
    const defaults = transformChannelToFormDefaults(storedChannel(undefined))
    expect(defaults.cost_ratio).toBe(0)
    expect(defaults.quota_limit_amount).toBe(0)
  })
})
