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
import { channelSchema, type ChannelSchedule } from '../../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  createChannelScheduleWindow,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
} from '../channel-form'
import { isInsideChannelSchedule } from '../channel-utils'

type WindowDraft = ReturnType<typeof createChannelScheduleWindow>

function scheduleForm(
  enabled: boolean,
  windows: WindowDraft[],
  timezone = 'Asia/Shanghai'
) {
  return {
    ...CHANNEL_FORM_DEFAULT_VALUES,
    name: 'scheduled upstream',
    type: 1,
    key: 'test-key',
    models: 'gpt-test',
    schedule_enabled: enabled,
    schedule_timezone: timezone,
    schedule_windows: windows,
  }
}

function issuesOn(
  values: ReturnType<typeof scheduleForm>,
  field: 'schedule_timezone' | 'schedule_windows'
): string[] {
  const result = channelFormSchema.safeParse(values)
  if (result.success) return []
  return result.error.issues
    .filter((issue) => issue.path[0] === field)
    .map((issue) => issue.message)
}

function storedChannel(schedule: ChannelSchedule | undefined) {
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
    models: 'gpt-test',
    group: 'default',
    extend_config: schedule ? { schedule } : undefined,
  })
}

// 2026-09-30 is a Wednesday; Shanghai is UTC+8 without daylight saving.
function shanghai(day: number, hour: number, minute = 0): Date {
  return new Date(Date.UTC(2026, 8, day, hour - 8, minute))
}

const workdays: ChannelSchedule = {
  timezone: 'Asia/Shanghai',
  windows: [{ days: [1, 2, 3, 4, 5], start: '09:00', end: '18:00' }],
}
const fridayNight: ChannelSchedule = {
  timezone: 'Asia/Shanghai',
  windows: [{ days: [5], start: '22:00', end: '06:00' }],
}

describe('isInsideChannelSchedule', () => {
  test('a channel without a schedule is always inside', () => {
    expect(isInsideChannelSchedule(undefined, shanghai(30, 3))).toBe(true)
    expect(
      isInsideChannelSchedule(
        { timezone: 'Asia/Shanghai', windows: [] },
        shanghai(30, 3)
      )
    ).toBe(true)
  })

  test('a weekday window includes its start and excludes its end', () => {
    expect(isInsideChannelSchedule(workdays, shanghai(30, 9))).toBe(true)
    expect(isInsideChannelSchedule(workdays, shanghai(30, 8, 59))).toBe(false)
    expect(isInsideChannelSchedule(workdays, shanghai(30, 17, 59))).toBe(true)
    expect(isInsideChannelSchedule(workdays, shanghai(30, 18))).toBe(false)
  })

  test('a weekday window does not apply on the weekend', () => {
    expect(isInsideChannelSchedule(workdays, shanghai(26, 10))).toBe(false)
  })

  test('a window crossing midnight belongs to the day it starts on', () => {
    expect(isInsideChannelSchedule(fridayNight, shanghai(25, 23))).toBe(true)
    expect(isInsideChannelSchedule(fridayNight, shanghai(26, 5, 59))).toBe(true)
    expect(isInsideChannelSchedule(fridayNight, shanghai(26, 6))).toBe(false)
    expect(isInsideChannelSchedule(fridayNight, shanghai(26, 23))).toBe(false)
  })

  test('the instant is converted to the schedule timezone', () => {
    const tokyoMorning: ChannelSchedule = {
      timezone: 'Asia/Tokyo',
      windows: [{ days: [], start: '09:00', end: '10:00' }],
    }
    // 00:30 UTC is 09:30 in Tokyo but 08:30 in Shanghai
    const instant = new Date(Date.UTC(2026, 8, 30, 0, 30))
    expect(isInsideChannelSchedule(tokyoMorning, instant)).toBe(true)
    expect(
      isInsideChannelSchedule(
        { ...tokyoMorning, timezone: 'Asia/Shanghai' },
        instant
      )
    ).toBe(false)
  })

  test('an unknown timezone fails open', () => {
    expect(
      isInsideChannelSchedule(
        { ...workdays, timezone: 'Mars/Olympus' },
        shanghai(26, 3)
      )
    ).toBe(true)
  })
})

describe('channel schedule validation', () => {
  test('a disabled schedule ignores its windows', () => {
    expect(issuesOn(scheduleForm(false, []), 'schedule_windows')).toEqual([])
  })

  test('an enabled schedule without windows reports the empty error', () => {
    expect(issuesOn(scheduleForm(true, []), 'schedule_windows')).toEqual([
      ERROR_MESSAGES.INVALID_CHANNEL_SCHEDULE_EMPTY,
    ])
  })

  test('an enabled schedule without a timezone reports the timezone error', () => {
    expect(
      issuesOn(
        scheduleForm(true, [createChannelScheduleWindow()], ' '),
        'schedule_timezone'
      )
    ).toEqual([ERROR_MESSAGES.INVALID_CHANNEL_SCHEDULE_TIMEZONE])
  })

  test('a window with a malformed time reports the time error', () => {
    expect(
      issuesOn(
        scheduleForm(true, [{ days: [], start: '9:00', end: '18:00' }]),
        'schedule_windows'
      )
    ).toEqual([ERROR_MESSAGES.INVALID_CHANNEL_SCHEDULE_TIME])
  })

  test('a window that starts and ends at the same time is rejected', () => {
    expect(
      issuesOn(
        scheduleForm(true, [{ days: [1], start: '09:00', end: '09:00' }]),
        'schedule_windows'
      )
    ).toEqual([ERROR_MESSAGES.INVALID_CHANNEL_SCHEDULE_SAME_TIME])
  })

  test('more than 16 windows are rejected', () => {
    const windows = Array.from({ length: 17 }, () =>
      createChannelScheduleWindow()
    )
    expect(issuesOn(scheduleForm(true, windows), 'schedule_windows')).toEqual([
      ERROR_MESSAGES.INVALID_CHANNEL_SCHEDULE_TOO_MANY,
    ])
  })
})

describe('channel schedule payload', () => {
  test('a disabled schedule is omitted from extend_config', () => {
    const { channel } = transformFormDataToCreatePayload(
      scheduleForm(false, [createChannelScheduleWindow()])
    )
    expect(channel.extend_config?.schedule).toBeUndefined()
  })

  test('an enabled schedule sends the timezone and sorted weekdays', () => {
    const { channel } = transformFormDataToCreatePayload(
      scheduleForm(true, [
        { days: [5, 1], start: '22:00', end: '06:00' },
        { days: [], start: '09:00', end: '12:00' },
      ])
    )
    expect(channel.extend_config?.schedule).toEqual({
      timezone: 'Asia/Shanghai',
      windows: [
        { days: [1, 5], start: '22:00', end: '06:00' },
        { days: [], start: '09:00', end: '12:00' },
      ],
    })
  })
})

describe('channel schedule form defaults', () => {
  test('a stored schedule is restored with the toggle on', () => {
    const defaults = transformChannelToFormDefaults(storedChannel(fridayNight))
    expect(defaults.schedule_enabled).toBe(true)
    expect(defaults.schedule_timezone).toBe('Asia/Shanghai')
    expect(defaults.schedule_windows).toEqual([
      { days: [5], start: '22:00', end: '06:00' },
    ])
  })

  test('a channel without a schedule shows the toggle off with the default timezone', () => {
    const defaults = transformChannelToFormDefaults(storedChannel(undefined))
    expect(defaults.schedule_enabled).toBe(false)
    expect(defaults.schedule_timezone).toBe('Asia/Shanghai')
    expect(defaults.schedule_windows).toEqual([])
  })
})
