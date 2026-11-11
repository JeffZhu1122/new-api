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
import * as z from 'zod'

import { isObjectArray } from '../utils/json-validators'

// One outbound fingerprint profile as the gateway stores it. Every part
// besides the name is optional; an absent part keeps the Go default.
export type FingerprintProfile = {
  name: string
  tls_preset?: string
  tls_client_hello?: string
  header_order?: string[]
  user_agent?: string
  http2_fingerprint?: string
  http2_header_priority?: string
}

export const MAX_FINGERPRINT_PROFILES = 32

export const TLS_SOURCE_DEFAULT = 'default'
export const TLS_SOURCE_CUSTOM = 'custom'
const TLS_SOURCE_PRESET_PREFIX = 'preset:'

export const tlsPresetSource = (preset: string): string =>
  `${TLS_SOURCE_PRESET_PREFIX}${preset}`

export const isFingerprintProfileList = (
  data: unknown
): data is FingerprintProfile[] =>
  isObjectArray(data) && data.every((item) => typeof item.name === 'string')

const PROFILE_NAME_PATTERN = /^[A-Za-z0-9._-]{1,64}$/
const HEADER_NAME_PATTERN = /^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/
const CLIENT_HELLO_SEPARATORS = /[\s:]/g
const MAX_CLIENT_HELLO_LENGTH = 32 * 1024
const MAX_HEADER_ORDER_LENGTH = 64
const MAX_USER_AGENT_BYTES = 512
const MAX_HTTP2_FINGERPRINT_LENGTH = 512

const headerOrderLines = (value: string): string[] =>
  value
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line !== '')

// takenNames holds the names of the other stored profiles.
export const createFingerprintProfileSchema = (
  t: (key: string) => string,
  takenNames: string[]
) =>
  z
    .object({
      name: z
        .string()
        .regex(
          PROFILE_NAME_PATTERN,
          t(
            'Profile name must be 1 to 64 letters, digits, dots, underscores or hyphens'
          )
        )
        .refine(
          (value) => !takenNames.includes(value),
          t('A profile with this name already exists')
        ),
      tls_source: z.string(),
      tls_client_hello: z
        .string()
        .max(MAX_CLIENT_HELLO_LENGTH, t('The ClientHello capture is too long')),
      header_order: z.string().superRefine((value, ctx) => {
        const headers = headerOrderLines(value)
        if (headers.length > MAX_HEADER_ORDER_LENGTH) {
          ctx.addIssue({
            code: 'custom',
            message: t('Header order lists at most 64 headers'),
          })
          return
        }
        if (headers.some((header) => !HEADER_NAME_PATTERN.test(header))) {
          ctx.addIssue({
            code: 'custom',
            message: t('Enter one header name per line, without values'),
          })
          return
        }
        const lowered = headers.map((header) => header.toLowerCase())
        if (new Set(lowered).size !== lowered.length) {
          ctx.addIssue({
            code: 'custom',
            message: t('Header order names a header more than once'),
          })
        }
      }),
      user_agent: z
        .string()
        .refine(
          (value) =>
            new TextEncoder().encode(value).length <= MAX_USER_AGENT_BYTES,
          t('User-Agent must not exceed 512 bytes')
        )
        .refine(
          (value) =>
            [...value].every((char) => {
              const code = char.charCodeAt(0)
              return code >= 0x20 && code !== 0x7f
            }),
          t('User-Agent must be a single line without control characters')
        ),
      http2_fingerprint: z
        .string()
        .trim()
        .max(
          MAX_HTTP2_FINGERPRINT_LENGTH,
          t('HTTP/2 fingerprint must not exceed 512 characters')
        )
        .refine(
          (value) => value === '' || value.split('|').length === 4,
          t(
            'HTTP/2 fingerprint must be SETTINGS|WINDOW_UPDATE|PRIORITY|pseudo-header order'
          )
        ),
      http2_header_priority: z
        .string()
        .trim()
        .refine((value) => {
          if (value === '') return true
          const match = /^([01]):(\d{1,10}):(\d{1,3})$/.exec(value)
          if (!match) return false
          const weight = Number(match[3])
          return weight >= 1 && weight <= 256
        }, t('Header priority must be exclusive:dependency:weight, with a weight from 1 to 256')),
    })
    .superRefine((values, ctx) => {
      if (values.tls_source !== TLS_SOURCE_CUSTOM) return
      const compact = values.tls_client_hello.replace(
        CLIENT_HELLO_SEPARATORS,
        ''
      )
      if (
        compact === '' ||
        compact.length % 2 !== 0 ||
        !/^[0-9a-fA-F]+$/.test(compact)
      ) {
        ctx.addIssue({
          code: 'custom',
          path: ['tls_client_hello'],
          message: t('Paste the hex dump of a captured ClientHello'),
        })
      }
    })

export type FingerprintProfileFormValues = z.infer<
  ReturnType<typeof createFingerprintProfileSchema>
>

export function fingerprintProfileFormValues(
  profile: FingerprintProfile | null
): FingerprintProfileFormValues {
  let tlsSource = TLS_SOURCE_DEFAULT
  if (profile?.tls_preset) {
    tlsSource = tlsPresetSource(profile.tls_preset)
  } else if (profile?.tls_client_hello) {
    tlsSource = TLS_SOURCE_CUSTOM
  }
  return {
    name: profile?.name ?? '',
    tls_source: tlsSource,
    tls_client_hello: profile?.tls_client_hello ?? '',
    header_order: (profile?.header_order ?? []).join('\n'),
    user_agent: profile?.user_agent ?? '',
    http2_fingerprint: profile?.http2_fingerprint ?? '',
    http2_header_priority: profile?.http2_header_priority ?? '',
  }
}

// The stored form of a profile: parts left empty are omitted, so they keep the
// Go default.
export function fingerprintProfileFromForm(
  values: FingerprintProfileFormValues
): FingerprintProfile {
  const profile: FingerprintProfile = { name: values.name }
  if (values.tls_source === TLS_SOURCE_CUSTOM) {
    profile.tls_client_hello = values.tls_client_hello.trim()
  } else if (values.tls_source.startsWith(TLS_SOURCE_PRESET_PREFIX)) {
    profile.tls_preset = values.tls_source.slice(
      TLS_SOURCE_PRESET_PREFIX.length
    )
  }
  const headers = headerOrderLines(values.header_order)
  if (headers.length > 0) profile.header_order = headers
  if (values.user_agent !== '') profile.user_agent = values.user_agent
  if (values.http2_fingerprint !== '') {
    profile.http2_fingerprint = values.http2_fingerprint
  }
  if (values.http2_header_priority !== '') {
    profile.http2_header_priority = values.http2_header_priority
  }
  return profile
}
