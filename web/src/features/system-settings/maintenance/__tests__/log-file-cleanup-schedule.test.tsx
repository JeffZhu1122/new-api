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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import type { LogFileCleanupScheduleValues } from '../log-file-cleanup-schedule-schema'
import { LogSettingsSection } from '../log-settings-section'

const DEFAULT_SCHEDULE: LogFileCleanupScheduleValues = {
  enabled: false,
  frequency: 'daily',
  weekday: 1,
  month_day: 1,
  time: '03:00',
  mode: 'by_days',
  value: 30,
}

const NEXT_RUN_AT = 1791399600
const LAST_RUN_AT = 1791313200

function Fixture(props: { schedule: LogFileCleanupScheduleValues }) {
  const [container, setContainer] = useState<HTMLDivElement | null>(null)
  return (
    <>
      <div ref={setContainer} />
      <SettingsPageProvider actionsContainer={container}>
        <LogSettingsSection
          defaultEnabled={false}
          scheduleDefaults={props.schedule}
        />
      </SettingsPageProvider>
    </>
  )
}

async function renderSection(schedule: LogFileCleanupScheduleValues) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const router = createRouter({
    routeTree: createRootRoute({
      component: () => <Fixture schedule={schedule} />,
    }),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  return screen.findByRole('switch', { name: 'Scheduled log file cleanup' })
}

beforeEach(() => {
  vi.spyOn(api, 'get').mockImplementation(async (url: string) => {
    if (url === '/api/performance/logs') {
      return {
        data: {
          success: true,
          data: {
            enabled: true,
            log_dir: '/var/log/new-api',
            file_count: 3,
            total_size: 3072,
          },
        },
      }
    }
    if (url === '/api/performance/logs/schedule') {
      return {
        data: {
          success: true,
          data: {
            log_dir_configured: true,
            timezone: 'Asia/Shanghai',
            utc_offset: '+08:00',
            next_run_at: NEXT_RUN_AT,
            last_run: {
              started_at: LAST_RUN_AT,
              mode: 'by_days',
              value: 30,
              deleted_count: 2,
              freed_bytes: 2048,
              failed_files: null,
            },
          },
        },
      }
    }
    if (url === '/api/system-task/current') {
      return { data: { success: true, data: null } }
    }
    throw new Error(`Unexpected request: ${url}`)
  })
  vi.spyOn(api, 'put').mockResolvedValue({ data: { success: true } })
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('scheduled log file cleanup', () => {
  test('with the schedule off only the switch shows; turning it on reveals the schedule and saves only changed keys', async () => {
    const user = userEvent.setup()
    const toggle = await renderSection(DEFAULT_SCHEDULE)
    expect(screen.queryByLabelText('Time')).not.toBeInTheDocument()

    await user.click(toggle)
    const time = await screen.findByLabelText('Time')
    expect(screen.getByRole('combobox', { name: 'Repeat' })).toBeInTheDocument()
    expect(screen.getByLabelText('Days to Retain')).toHaveValue(30)

    fireEvent.change(time, { target: { value: '04:30' } })
    await user.click(screen.getByRole('button', { name: 'Save log settings' }))

    await waitFor(() => expect(api.put).toHaveBeenCalledTimes(2))
    expect(api.put).toHaveBeenCalledWith('/api/option/', {
      key: 'log_file_cleanup_setting.enabled',
      value: true,
    })
    expect(api.put).toHaveBeenCalledWith('/api/option/', {
      key: 'log_file_cleanup_setting.time',
      value: '04:30',
    })
  })

  test('weekly schedules ask for the weekday, monthly schedules for the day of month', async () => {
    await renderSection({
      ...DEFAULT_SCHEDULE,
      enabled: true,
      frequency: 'weekly',
    })
    expect(
      await screen.findByRole('combobox', { name: 'Weekday' })
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('combobox', { name: 'Day of month' })
    ).not.toBeInTheDocument()
  })

  test('monthly schedules ask for the day of month only', async () => {
    await renderSection({
      ...DEFAULT_SCHEDULE,
      enabled: true,
      frequency: 'monthly',
      mode: 'by_count',
      value: 10,
    })
    expect(
      await screen.findByRole('combobox', { name: 'Day of month' })
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('combobox', { name: 'Weekday' })
    ).not.toBeInTheDocument()
    expect(screen.getByLabelText('Files to Retain')).toHaveValue(10)
  })

  test('an invalid run time shows a field error and nothing is saved', async () => {
    const user = userEvent.setup()
    await renderSection({ ...DEFAULT_SCHEDULE, enabled: true })
    const time = await screen.findByLabelText('Time')

    fireEvent.change(time, { target: { value: '' } })
    await user.click(screen.getByRole('button', { name: 'Save log settings' }))

    expect(
      await screen.findByText('Enter a time as HH:MM (24-hour)')
    ).toBeInTheDocument()
    expect(time).toHaveAttribute('aria-invalid', 'true')
    expect(api.put).not.toHaveBeenCalled()
  })

  test('shows the server time zone, the next run and the last run result', async () => {
    await renderSection({ ...DEFAULT_SCHEDULE, enabled: true })

    // The test i18n instance escapes interpolated values ("/" -> "&#x2F;");
    // the app config disables that, so match around the separator.
    expect(
      await screen.findByText(
        /^Runs on each server node in the server time zone \(Asia.+Shanghai, UTC\+08:00\)\.$/
      )
    ).toBeInTheDocument()
    expect(screen.getByText(/^Next run: /)).toBeInTheDocument()
    expect(
      screen.getByText(/^Last run: .*removed 2 files and freed 2 KB\.$/)
    ).toBeInTheDocument()
  })
})
