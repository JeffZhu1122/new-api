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
import { z } from 'zod'

import { CHANNEL_STATUS, CHANNEL_TYPE_VERTEX_AI } from '../constants'
import { channelSchema, type Channel } from '../types'
import {
  type ChannelFormValues,
  transformChannelToFormDefaults,
} from './channel-form'

// A channel configuration copied from one site and pasted into the create form
// of another. It carries the keys, so it is produced only after 2FA.
export const CHANNEL_CONFIG_TRANSFER_TYPE = 'newapi_channel_config'
const CHANNEL_CONFIG_TRANSFER_VERSION = 1

// Everything that configures a channel. Identity, status, health checks,
// balance and usage belong to the source site and are left out.
const transferredChannelSchema = channelSchema.pick({
  type: true,
  name: true,
  key: true,
  openai_organization: true,
  test_model: true,
  weight: true,
  base_url: true,
  other: true,
  models: true,
  group: true,
  model_mapping: true,
  status_code_mapping: true,
  priority: true,
  auto_ban: true,
  tag: true,
  setting: true,
  param_override: true,
  header_override: true,
  remark: true,
  channel_info: true,
  settings: true,
  extend_config: true,
})

export type TransferredChannel = z.infer<typeof transferredChannelSchema>

const channelConfigTransferSchema = z.object({
  _type: z.literal(CHANNEL_CONFIG_TRANSFER_TYPE),
  version: z.literal(CHANNEL_CONFIG_TRANSFER_VERSION),
  channel: transferredChannelSchema,
})

/**
 * Encode the channel the config endpoint returned. The response is the raw
 * Go model, so its zero values (null remark, empty multi-key mode, empty
 * settings) are normalized here, and the result is validated with the same
 * schema the paste side uses, so a copy that succeeds always pastes.
 */
export function encodeChannelConfig(channel: Channel): string {
  const schedule = channel.extend_config?.schedule
  const transferred = transferredChannelSchema.parse({
    type: channel.type,
    name: channel.name,
    key: channel.key,
    openai_organization: channel.openai_organization,
    test_model: channel.test_model,
    weight: channel.weight,
    base_url: channel.base_url,
    other: channel.other ?? '',
    models: channel.models ?? '',
    group: channel.group || 'default',
    model_mapping: channel.model_mapping,
    status_code_mapping: channel.status_code_mapping,
    priority: channel.priority,
    auto_ban: channel.auto_ban,
    tag: channel.tag,
    setting: channel.setting,
    param_override: channel.param_override,
    header_override: channel.header_override,
    remark: channel.remark ?? '',
    // Per-key health is the source site's; the target starts every key fresh.
    channel_info: {
      is_multi_key: Boolean(channel.channel_info?.is_multi_key),
      multi_key_size: channel.channel_info?.multi_key_size ?? 0,
      multi_key_polling_index: 0,
      multi_key_mode:
        channel.channel_info?.multi_key_mode === 'polling'
          ? 'polling'
          : 'random',
    },
    settings: channel.settings || '{}',
    extend_config: channel.extend_config && {
      ...channel.extend_config,
      schedule: schedule && { ...schedule, windows: schedule.windows ?? [] },
    },
  })
  return JSON.stringify(
    {
      _type: CHANNEL_CONFIG_TRANSFER_TYPE,
      version: CHANNEL_CONFIG_TRANSFER_VERSION,
      channel: transferred,
    },
    null,
    2
  )
}

export function parseChannelConfig(
  text: string | null | undefined
): TransferredChannel | null {
  if (!text?.trim()) return null
  try {
    const parsed = channelConfigTransferSchema.safeParse(
      JSON.parse(text.trim())
    )
    return parsed.success ? parsed.data.channel : null
  } catch {
    return null
  }
}

/**
 * Turn a pasted configuration into create form values. The new channel starts
 * manually disabled, like every new channel, until it has been tested.
 */
export function channelConfigToFormValues(
  channel: TransferredChannel
): ChannelFormValues {
  const values = transformChannelToFormDefaults({
    ...channel,
    id: 0,
    status: CHANNEL_STATUS.MANUAL_DISABLED,
    created_time: 0,
    test_time: 0,
    response_time: 0,
    balance: 0,
    balance_updated_time: 0,
    used_quota: 0,
    other_info: '',
    max_input_tokens: 0,
  })
  if (!channel.channel_info.is_multi_key) {
    return { ...values, key: channel.key }
  }
  // A multi-key channel stores one key per line; the create form takes Vertex
  // AI service account keys as one JSON array instead.
  let key = channel.key
  if (
    channel.type === CHANNEL_TYPE_VERTEX_AI &&
    values.vertex_key_type === 'json'
  ) {
    const lines = key.split('\n').map((line) => line.trim())
    key = `[${lines.filter(Boolean).join(',')}]`
  }
  return {
    ...values,
    key,
    multi_key_mode: 'multi_to_single',
    multi_key_type: channel.channel_info.multi_key_mode,
  }
}
