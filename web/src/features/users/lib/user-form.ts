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

import {
  type PermissionCatalog,
  type AdminPermissionMatrix,
  normalizeAdminPermissions,
} from '@/lib/admin-permissions'
import { quotaUnitsToDollars } from '@/lib/format'
import { ROLE } from '@/lib/roles'

import { DEFAULT_GROUP } from '../constants'
import type {
  RateLimitOverride,
  RateLimitValues,
  UserFormData,
  User,
} from '../types'

// ============================================================================
// Form Schema
// ============================================================================

const RATE_LIMIT_MAX = 2147483647

const isEmptyOrLimitNumber = (value?: string) => {
  if (!value || value.trim() === '') {
    return true
  }
  const trimmed = value.trim()
  if (!/^\d+$/.test(trimmed)) {
    return false
  }
  return Number(trimmed) <= RATE_LIMIT_MAX
}

const isLimitNumber = (value: unknown): value is number =>
  typeof value === 'number' &&
  Number.isInteger(value) &&
  value >= 0 &&
  value <= RATE_LIMIT_MAX

const isValidRateLimitModelsJSON = (value?: string) => {
  if (!value || value.trim() === '') {
    return true
  }
  try {
    const parsed = JSON.parse(value)
    if (
      typeof parsed !== 'object' ||
      parsed === null ||
      Array.isArray(parsed)
    ) {
      return false
    }
    for (const entry of Object.values(parsed)) {
      if (
        typeof entry !== 'object' ||
        entry === null ||
        Array.isArray(entry)
      ) {
        return false
      }
      const { rpm, tpm, ...rest } = entry as Record<string, unknown>
      if (Object.keys(rest).length > 0) {
        return false
      }
      if (rpm !== undefined && !isLimitNumber(rpm)) {
        return false
      }
      if (tpm !== undefined && !isLimitNumber(tpm)) {
        return false
      }
    }
    return true
  } catch {
    return false
  }
}

export const userFormSchema = z.object({
  username: z.string().min(1, 'Username is required'),
  display_name: z.string().optional(),
  password: z.string().optional(),
  role: z.number().optional(),
  quota_dollars: z.number().min(0).optional(),
  group: z.string().optional(),
  remark: z.string().optional(),
  admin_permissions: z
    .record(z.string(), z.record(z.string(), z.boolean()))
    .optional(),
  rate_limit_default_rpm: z
    .string()
    .optional()
    .refine(isEmptyOrLimitNumber, 'Must be an integer within [0, 2147483647]'),
  rate_limit_default_tpm: z
    .string()
    .optional()
    .refine(isEmptyOrLimitNumber, 'Must be an integer within [0, 2147483647]'),
  rate_limit_models: z
    .string()
    .optional()
    .refine(
      isValidRateLimitModelsJSON,
      'Invalid JSON format or values out of allowed range'
    ),
})

export type UserFormValues = z.infer<typeof userFormSchema>

// ============================================================================
// Form Defaults
// ============================================================================

export const USER_FORM_DEFAULT_VALUES: UserFormValues = {
  username: '',
  display_name: '',
  password: '',
  role: 1, // Default to common user
  quota_dollars: 0,
  group: DEFAULT_GROUP,
  remark: '',
  // Filled against the backend catalog at render time; see UsersMutateDrawer.
  admin_permissions: {},
  rate_limit_default_rpm: '',
  rate_limit_default_tpm: '',
  rate_limit_models: '',
}

// ============================================================================
// Rate Limit Override Transformation
// ============================================================================

function buildRateLimitOverride(data: UserFormValues): RateLimitOverride {
  const override: RateLimitOverride = {}
  const defaults: RateLimitValues = {}
  if (data.rate_limit_default_rpm?.trim()) {
    defaults.rpm = Number(data.rate_limit_default_rpm.trim())
  }
  if (data.rate_limit_default_tpm?.trim()) {
    defaults.tpm = Number(data.rate_limit_default_tpm.trim())
  }
  if (defaults.rpm !== undefined || defaults.tpm !== undefined) {
    override.default = defaults
  }
  if (data.rate_limit_models?.trim()) {
    try {
      const models = JSON.parse(data.rate_limit_models) as Record<
        string,
        RateLimitValues
      >
      if (Object.keys(models).length > 0) {
        override.models = models
      }
    } catch {
      // schema validation already rejects invalid JSON; ignore here
    }
  }
  return override
}

// ============================================================================
// Form Data Transformation
// ============================================================================

/**
 * Transform form data to API payload
 */
export function transformFormDataToPayload(
  data: UserFormValues,
  userId?: number,
  catalog?: PermissionCatalog
): UserFormData & { id?: number } {
  const payload: UserFormData & { id?: number } = {
    username: data.username,
    display_name: data.display_name || data.username,
    password: data.password || undefined,
  }

  const role = userId === undefined ? data.role || 1 : (data.role ?? 0)

  // Only send the permission matrix when the target is an admin and the catalog
  // is available; without the catalog we cannot build a full matrix, so we omit
  // the field (the backend then leaves existing permissions untouched).
  if (role >= ROLE.ADMIN && catalog) {
    payload.admin_permissions = normalizeAdminPermissions(
      data.admin_permissions as AdminPermissionMatrix | undefined,
      catalog
    )
  }

  // For create: only send required fields
  if (userId === undefined) {
    payload.role = role
  } else {
    // For update: quota is adjusted atomically via /api/user/manage, not sent here
    payload.group = data.group
    payload.remark = data.remark || undefined
    payload.id = userId
    // Always sent on update: the backend treats {} as "clear the override" and
    // a missing field as "leave untouched"; the form round-trips the current
    // override from GetUser, so sending it back is lossless.
    payload.rate_limit = buildRateLimitOverride(data)
  }

  return payload
}

/**
 * Transform user data to form defaults. The admin permission matrix is passed
 * through as-is (the backend already returns a full matrix); it is filled against
 * the catalog at render time in UsersMutateDrawer.
 */
export function transformUserToFormDefaults(user: User): UserFormValues {
  const rateLimit = user.rate_limit
  return {
    username: user.username,
    display_name: user.display_name,
    password: '',
    role: user.role,
    quota_dollars: quotaUnitsToDollars(user.quota),
    group: user.group || DEFAULT_GROUP,
    remark: user.remark || '',
    admin_permissions: user.admin_permissions ?? {},
    rate_limit_default_rpm:
      rateLimit?.default?.rpm !== undefined
        ? String(rateLimit.default.rpm)
        : '',
    rate_limit_default_tpm:
      rateLimit?.default?.tpm !== undefined
        ? String(rateLimit.default.tpm)
        : '',
    rate_limit_models:
      rateLimit?.models && Object.keys(rateLimit.models).length > 0
        ? JSON.stringify(rateLimit.models, null, 2)
        : '',
  }
}
