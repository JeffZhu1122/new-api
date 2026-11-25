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
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import type {
  LogResponseModelBackfillStatus,
  LogResponseModelBackfillTask,
} from '../../types'
import { ResponseModelBackfill } from '../response-model-backfill'

const BACKFILL_URL = '/api/system-task/log-response-model-backfill'

function backfillTask(
  overrides: Partial<LogResponseModelBackfillTask>
): LogResponseModelBackfillTask {
  return {
    id: 1,
    task_id: 'systask_backfill',
    type: 'log_response_model_backfill',
    status: 'succeeded',
    created_at: 1764000000,
    updated_at: 1764000100,
    ...overrides,
  }
}

function mockStatus(...statuses: LogResponseModelBackfillStatus[]) {
  const get = vi.spyOn(api, 'get')
  for (const status of statuses) {
    get.mockResolvedValueOnce({ data: { success: true, data: status } })
  }
  get.mockResolvedValue({
    data: { success: true, data: statuses.at(-1) },
  })
  return get
}

function renderBackfill() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <ResponseModelBackfill />
    </QueryClientProvider>
  )
}

afterEach(() => {
  vi.restoreAllMocks()
})

test('a running backfill shows its progress and cannot be started again', async () => {
  mockStatus({
    supported: true,
    done: false,
    task: backfillTask({
      status: 'running',
      state: {
        from_id: 1,
        to_id: 10000,
        cursor: 4000,
        progress: 40,
        filled: 1234,
      },
    }),
  })
  renderBackfill()

  const progress = await screen.findByRole('progressbar', {
    name: 'Backfill progress',
  })
  expect(progress).toHaveAttribute('aria-valuenow', '40')
  expect(screen.getByText('Filled 1,234 logs so far.')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Backfilling...' })).toBeDisabled()
})

test('a finished run reports how many logs it filled', async () => {
  mockStatus({
    supported: true,
    done: true,
    task: backfillTask({ result: { filled: 12 } }),
  })
  renderBackfill()

  expect(
    await screen.findByText(/^The last run on .+ filled 12 logs\.$/)
  ).toBeInTheDocument()
  expect(screen.queryByRole('progressbar')).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Run backfill' })).toBeEnabled()
})

test('a failed run shows its error', async () => {
  mockStatus({
    supported: true,
    done: false,
    task: backfillTask({ status: 'failed', error: 'database is locked' }),
  })
  renderBackfill()

  expect(
    await screen.findByText(/failed: database is locked$/)
  ).toBeInTheDocument()
})

test('a ClickHouse log database offers no backfill', async () => {
  mockStatus({ supported: false, done: false, task: null })
  renderBackfill()

  expect(
    await screen.findByText('ClickHouse log databases are not backfilled')
  ).toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: 'Run backfill' })
  ).not.toBeInTheDocument()
})

test('running the backfill asks first, then starts it and follows its progress', async () => {
  const user = userEvent.setup()
  mockStatus(
    {
      supported: true,
      done: true,
      task: backfillTask({ result: { filled: 3 } }),
    },
    {
      supported: true,
      done: true,
      task: backfillTask({
        status: 'pending',
        state: { from_id: 0, to_id: 0, cursor: 0, progress: 0, filled: 0 },
      }),
    }
  )
  const post = vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, data: backfillTask({ status: 'pending' }) },
  })
  renderBackfill()

  await user.click(await screen.findByRole('button', { name: 'Run backfill' }))
  expect(post).not.toHaveBeenCalled()
  const dialog = await screen.findByRole('alertdialog')
  await user.click(within(dialog).getByRole('button', { name: 'Run backfill' }))

  expect(post).toHaveBeenCalledWith(BACKFILL_URL)
  await waitFor(() =>
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  )
  expect(
    await screen.findByRole('progressbar', { name: 'Backfill progress' })
  ).toBeInTheDocument()
})
