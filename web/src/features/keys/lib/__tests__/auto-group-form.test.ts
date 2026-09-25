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
import type { TFunction } from 'i18next'
import { describe, expect, test } from 'vitest'

import { apiKeySchema, type ApiKey } from '../../types'
import {
  getApiKeyFormDefaultValues,
  getApiKeyFormSchema,
  transformApiKeyToFormDefaults,
  transformFormDataToPayload,
} from '../api-key-form'

const t = ((key: string, options?: Record<string, unknown>) => {
  if (options?.max !== undefined) {
    return key.replace('{{max}}', String(options.max))
  }
  return key
}) as TFunction

const baseApiKey: ApiKey = {
  id: 1,
  name: 'test',
  key: 'sk-test',
  status: 1,
  remain_quota: 0,
  used_quota: 0,
  unlimited_quota: true,
  expired_time: -1,
  created_time: 1,
  accessed_time: 0,
  group: 'auto',
  auto_groups: null,
  cross_group_retry: true,
  model_limits_enabled: false,
  model_limits: '',
  allow_ips: '',
}

describe('API key Auto group form mapping', () => {
  test('treats legacy token responses without auto_groups as inheritance', () => {
    const legacyApiKey: Record<string, unknown> = { ...baseApiKey }
    delete legacyApiKey.auto_groups

    expect(apiKeySchema.parse(legacyApiKey).auto_groups).toBe(null)
  })

  test('creates an Auto token that inherits the global order', () => {
    const defaults = getApiKeyFormDefaultValues(true)

    expect(defaults.group).toBe('auto')
    expect(defaults.auto_groups_mode).toBe('inherit')
    expect(defaults.auto_groups).toEqual([])
    expect(transformFormDataToPayload(defaults).auto_groups).toEqual([])
  })

  test('maps omitted, null, and empty snapshots to inheritance on edit', () => {
    const legacyApiKey: Record<string, unknown> = { ...baseApiKey }
    delete legacyApiKey.auto_groups
    const inheritedApiKeys = [
      apiKeySchema.parse(legacyApiKey),
      baseApiKey,
      { ...baseApiKey, auto_groups: [] },
    ]

    for (const apiKey of inheritedApiKeys) {
      const defaults = transformApiKeyToFormDefaults(
        apiKey,
        ['default', 'vip'],
        2
      )

      expect(defaults.auto_groups_mode).toBe('inherit')
      expect(defaults.auto_groups).toEqual([])
    }
  })

  test('filters a stored snapshot before applying a lowered limit', () => {
    const defaults = transformApiKeyToFormDefaults(
      {
        ...baseApiKey,
        auto_groups: ['revoked', 'vip', 'default'],
      },
      ['default', 'vip'],
      2
    )

    expect(defaults.auto_groups_mode).toBe('custom')
    expect(defaults.auto_groups).toEqual(['vip', 'default'])
  })

  test('keeps a fully filtered snapshot custom and rejects it until resolved', () => {
    const defaults = transformApiKeyToFormDefaults(
      { ...baseApiKey, auto_groups: ['revoked'] },
      ['default'],
      2
    )

    expect(defaults.auto_groups_mode).toBe('custom')
    expect(defaults.auto_groups).toEqual([])

    const result = getApiKeyFormSchema(t, 2).safeParse(defaults)
    expect(result.success).toBe(false)
    if (result.success) return
    expect(result.error.issues[0]?.path).toEqual(['auto_groups'])
    expect(result.error.issues[0]?.message).toBe(
      'Select at least one Auto group or restore global Auto.'
    )
  })

  test('submits a valid custom snapshot in its configured order', () => {
    const custom = {
      ...getApiKeyFormDefaultValues(true),
      auto_groups_mode: 'custom' as const,
      auto_groups: ['vip', 'default'],
    }

    expect(transformFormDataToPayload(custom).auto_groups).toEqual([
      'vip',
      'default',
    ])
  })

  test('submits an empty array for inheritance and for non-Auto groups', () => {
    const inherited = getApiKeyFormDefaultValues(true)
    expect(transformFormDataToPayload(inherited).auto_groups).toEqual([])

    const nonAuto = {
      ...inherited,
      group: 'default',
      auto_groups_mode: 'custom' as const,
      auto_groups: ['vip'],
    }
    expect(transformFormDataToPayload(nonAuto).auto_groups).toEqual([])
    expect(transformFormDataToPayload(nonAuto).cross_group_retry).toBe(false)
  })

  test('rejects snapshots over the configured limit', () => {
    const result = getApiKeyFormSchema(t, 1).safeParse({
      ...getApiKeyFormDefaultValues(true),
      name: 'limited token',
      auto_groups_mode: 'custom',
      auto_groups: ['default', 'vip'],
    })

    expect(result.success).toBe(false)
    if (result.success) return
    expect(result.error.issues[0]?.path[0]).toBe('auto_groups')
    expect(result.error.issues[0]?.message).toBe('Select at most 1 Auto groups')
  })

  test('rejects duplicate custom groups', () => {
    const result = getApiKeyFormSchema(t).safeParse({
      ...getApiKeyFormDefaultValues(true),
      name: 'duplicate token',
      auto_groups_mode: 'custom',
      auto_groups: ['vip', 'vip'],
    })

    expect(result.success).toBe(false)
    if (result.success) return
    expect(result.error.issues[0]?.message).toBe(
      'Auto groups must not contain duplicates'
    )
  })
})

describe('API key fallback group form mapping', () => {
  const ordinaryDefaults = {
    ...getApiKeyFormDefaultValues(false),
    name: 'ordinary token',
    group: 'vip',
  }

  test('submits ordered fallback groups for an ordinary group', () => {
    const payload = transformFormDataToPayload({
      ...ordinaryDefaults,
      fallback_groups: ['default', 'svip'],
      cross_group_retry: true,
    })

    expect(payload.group).toBe('vip')
    expect(payload.auto_groups).toEqual(['default', 'svip'])
    expect(payload.cross_group_retry).toBe(true)
  })

  test('never sends fallback groups without a concrete primary group', () => {
    const followUser = transformFormDataToPayload({
      ...ordinaryDefaults,
      group: '',
      fallback_groups: ['default'],
      cross_group_retry: true,
    })
    expect(followUser.auto_groups).toEqual([])
    expect(followUser.cross_group_retry).toBe(false)

    const auto = transformFormDataToPayload({
      ...ordinaryDefaults,
      group: 'auto',
      fallback_groups: ['default'],
      cross_group_retry: true,
    })
    expect(auto.auto_groups).toEqual([])
    expect(auto.cross_group_retry).toBe(true)
  })

  test('disables cross-group retry when no fallback groups remain', () => {
    const payload = transformFormDataToPayload({
      ...ordinaryDefaults,
      fallback_groups: ['vip'],
      cross_group_retry: true,
    })

    expect(payload.auto_groups).toEqual([])
    expect(payload.cross_group_retry).toBe(false)
  })

  test('maps a stored fallback snapshot onto the form and strips the primary', () => {
    const defaults = transformApiKeyToFormDefaults(
      {
        ...baseApiKey,
        group: 'vip',
        auto_groups: ['vip', 'revoked', 'default', 'svip'],
        cross_group_retry: true,
      },
      ['default', 'vip', 'svip'],
      3
    )

    expect(defaults.group).toBe('vip')
    expect(defaults.fallback_groups).toEqual(['default', 'svip'])
    expect(defaults.auto_groups_mode).toBe('inherit')
    expect(defaults.auto_groups).toEqual([])
    expect(defaults.cross_group_retry).toBe(true)
  })

  test('keeps the Auto snapshot out of the fallback list', () => {
    const defaults = transformApiKeyToFormDefaults(
      { ...baseApiKey, auto_groups: ['vip', 'default'] },
      ['default', 'vip'],
      5
    )

    expect(defaults.group).toBe('auto')
    expect(defaults.auto_groups_mode).toBe('custom')
    expect(defaults.auto_groups).toEqual(['vip', 'default'])
    expect(defaults.fallback_groups).toEqual([])
  })

  test('rejects more fallback groups than the shared limit allows', () => {
    const result = getApiKeyFormSchema(t, 2).safeParse({
      ...ordinaryDefaults,
      fallback_groups: ['default', 'svip'],
    })

    expect(result.success).toBe(false)
    if (result.success) return
    expect(result.error.issues[0]?.path).toEqual(['fallback_groups'])
    expect(result.error.issues[0]?.message).toBe(
      'Select at most 1 fallback groups'
    )
  })

  test('rejects fallback groups that repeat or include the primary group', () => {
    const duplicate = getApiKeyFormSchema(t).safeParse({
      ...ordinaryDefaults,
      fallback_groups: ['default', 'default'],
    })
    expect(duplicate.success).toBe(false)
    if (!duplicate.success) {
      expect(duplicate.error.issues[0]?.message).toBe(
        'Fallback groups must not contain duplicates'
      )
    }

    const primary = getApiKeyFormSchema(t).safeParse({
      ...ordinaryDefaults,
      fallback_groups: ['vip'],
    })
    expect(primary.success).toBe(false)
    if (!primary.success) {
      expect(primary.error.issues[0]?.message).toBe(
        'Fallback groups must not include the primary group'
      )
    }
  })

  test('accepts an ordinary group without fallback groups', () => {
    const result = getApiKeyFormSchema(t, 1).safeParse(ordinaryDefaults)
    expect(result.success).toBe(true)
  })
})
