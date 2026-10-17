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
/**
 * Utility functions for usage logs filters
 */
import { LOG_CATEGORY_LABELS } from '../constants'
import type {
  LogCategory,
  LogFilters,
  CommonLogFilters,
  DrawingLogFilters,
  TaskLogFilters,
} from '../types'

// ============================================================================
// Filter Building Functions
// ============================================================================

/**
 * Check the typed metric filters before searching, with the server's rules
 * and messages: token counts and use time are whole numbers of 0 or more,
 * TTFT (seconds) is 0 or more, a minimum is not above its maximum, and status
 * codes are 100-599. Returns the message key of the first problem, or null.
 */
export function getMetricFilterError(filters: CommonLogFilters): string | null {
  const tokenRule = {
    wholeNumbers: true,
    invalid: 'Token counts must be whole numbers of 0 or more',
    reversed: 'The minimum token count cannot be greater than the maximum',
  }
  const ranges = [
    { ...tokenRule, bounds: [filters.inputTokensMin, filters.inputTokensMax] },
    {
      ...tokenRule,
      bounds: [filters.outputTokensMin, filters.outputTokensMax],
    },
    {
      bounds: [filters.useTimeMin, filters.useTimeMax],
      wholeNumbers: true,
      invalid: 'Use time must be whole seconds of 0 or more',
      reversed: 'The minimum cannot be greater than the maximum',
    },
    {
      bounds: [filters.ttftMin, filters.ttftMax],
      wholeNumbers: false,
      invalid: 'TTFT must be 0 seconds or more',
      reversed: 'The minimum cannot be greater than the maximum',
    },
  ]
  for (const range of ranges) {
    const [min, max] = range.bounds.map((value) =>
      value?.trim() ? Number(value) : undefined
    )
    for (const bound of [min, max]) {
      if (bound === undefined) continue
      const valid =
        Number.isFinite(bound) &&
        bound >= 0 &&
        (!range.wholeNumbers || Number.isInteger(bound))
      if (!valid) return range.invalid
    }
    if (min !== undefined && max !== undefined && min > max) {
      return range.reversed
    }
  }
  for (const code of filters.statusCodes ?? []) {
    const value = Number(code)
    if (!Number.isInteger(value) || value < 100 || value > 599) {
      return 'Status codes must be numbers from 100 to 599'
    }
  }
  return null
}

/**
 * Build search params from filters based on log category
 */
export function buildSearchParams(
  filters: LogFilters,
  logCategory: LogCategory
): Record<string, unknown> {
  const baseParams: Record<string, unknown> = {
    ...(filters.startTime && { startTime: filters.startTime.getTime() }),
    ...(filters.endTime && { endTime: filters.endTime.getTime() }),
    ...(filters.channel && { channel: filters.channel }),
  }

  switch (logCategory) {
    case 'common': {
      const commonFilters = filters as CommonLogFilters
      return {
        ...baseParams,
        ...(commonFilters.model && { model: commonFilters.model }),
        ...(commonFilters.token && { token: commonFilters.token }),
        ...(commonFilters.group && { group: commonFilters.group }),
        ...(commonFilters.username && { username: commonFilters.username }),
        ...(commonFilters.requestId && { requestId: commonFilters.requestId }),
        ...(commonFilters.upstreamRequestId && {
          upstreamRequestId: commonFilters.upstreamRequestId,
        }),
        ...(commonFilters.inputTokensMin && {
          inputTokensMin: Number(commonFilters.inputTokensMin),
        }),
        ...(commonFilters.inputTokensMax && {
          inputTokensMax: Number(commonFilters.inputTokensMax),
        }),
        ...(commonFilters.outputTokensMin && {
          outputTokensMin: Number(commonFilters.outputTokensMin),
        }),
        ...(commonFilters.outputTokensMax && {
          outputTokensMax: Number(commonFilters.outputTokensMax),
        }),
        ...(commonFilters.useTimeMin && {
          useTimeMin: Number(commonFilters.useTimeMin),
        }),
        ...(commonFilters.useTimeMax && {
          useTimeMax: Number(commonFilters.useTimeMax),
        }),
        ...(commonFilters.ttftMin && {
          ttftMin: Number(commonFilters.ttftMin),
        }),
        ...(commonFilters.ttftMax && {
          ttftMax: Number(commonFilters.ttftMax),
        }),
        ...(commonFilters.statusCodes?.length && {
          statusCodes: commonFilters.statusCodes.map(Number),
        }),
      }
    }
    case 'drawing': {
      const drawingFilters = filters as DrawingLogFilters
      return {
        ...baseParams,
        ...(drawingFilters.mjId && { filter: drawingFilters.mjId }),
      }
    }
    case 'task': {
      const taskFilters = filters as TaskLogFilters
      return {
        ...baseParams,
        ...(taskFilters.taskId && { filter: taskFilters.taskId }),
      }
    }
    default:
      return baseParams
  }
}

/**
 * Get log category display name
 */
export function getLogCategoryLabel(category: LogCategory): string {
  return LOG_CATEGORY_LABELS[category]
}
