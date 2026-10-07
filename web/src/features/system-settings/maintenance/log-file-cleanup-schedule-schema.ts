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

export const LOG_FILE_CLEANUP_MAX_MONTH_DAY = 28

export const logFileCleanupScheduleSchema = z.object({
  enabled: z.boolean(),
  frequency: z.enum(['daily', 'weekly', 'monthly']),
  weekday: z.number().int().min(0).max(6),
  month_day: z.number().int().min(1).max(LOG_FILE_CLEANUP_MAX_MONTH_DAY),
  time: z
    .string()
    .regex(/^([01]\d|2[0-3]):[0-5]\d$/, 'Enter a time as HH:MM (24-hour)'),
  mode: z.enum(['by_count', 'by_days']),
  value: z
    .number({ message: 'Please enter a valid number' })
    .int('Please enter a valid number')
    .min(1, 'Please enter a valid number'),
})

export type LogFileCleanupScheduleValues = z.infer<
  typeof logFileCleanupScheduleSchema
>

/** Option keys are `log_file_cleanup_setting.<field>`. */
export const LOG_FILE_CLEANUP_SCHEDULE_FIELDS = [
  'enabled',
  'frequency',
  'weekday',
  'month_day',
  'time',
  'mode',
  'value',
] as const satisfies ReadonlyArray<keyof LogFileCleanupScheduleValues>

export const LOG_FILE_CLEANUP_SCHEDULE_QUERY_KEY = [
  'log-file-cleanup-schedule',
] as const
