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
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import {
  ApiKeyOwnerContext,
  type ApiKeyOwner,
} from '../../hooks/use-api-key-owner'
import { apiKeySchema } from '../../types'
import { ModelLimitsCell } from '../api-keys-cells'

const clients: QueryClient[] = []

afterEach(() => {
  for (const client of clients.splice(0)) client.clear()
  vi.restoreAllMocks()
})

function apiKey(limits?: string) {
  return apiKeySchema.parse({
    id: 7,
    name: 'prod',
    key: 'demo********1234',
    status: 1,
    remain_quota: 0,
    used_quota: 0,
    unlimited_quota: true,
    expired_time: -1,
    created_time: 0,
    accessed_time: 0,
    group: 'default',
    model_limits_enabled: limits !== undefined,
    model_limits: limits ?? '',
  })
}

function renderCell(
  key: ReturnType<typeof apiKey>,
  options: { owner?: ApiKeyOwner; detailsTrigger?: 'hover' | 'click' } = {}
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <ApiKeyOwnerContext.Provider value={options.owner ?? null}>
        <ModelLimitsCell apiKey={key} detailsTrigger={options.detailsTrigger} />
      </ApiKeyOwnerContext.Provider>
    </QueryClientProvider>
  )
}

it('loads the models a key can call only when its model cell is hovered', async () => {
  const user = userEvent.setup()
  const get = vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: ['gpt-4o', 'gpt-4o-mini'] },
  })
  const copy = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()
  renderCell(apiKey('gpt-4o,gpt-4o-mini,retired-model'))

  const trigger = screen.getByRole('button', {
    name: 'Callable models: 3 models',
  })
  expect(get).not.toHaveBeenCalled()
  // The cell offset sits on the trigger so the limit label is not cut short.
  expect(trigger).toHaveClass('-ml-1.5')
  expect(within(trigger).getByText('3 models')).not.toHaveClass('-ml-1.5')

  await user.hover(trigger)

  const list = await screen.findByRole('list', { name: 'Callable models' })
  expect(get).toHaveBeenCalledWith('/api/token/7/models')
  expect(
    within(list)
      .getAllByRole('listitem')
      .map((item) => item.textContent)
  ).toEqual(['gpt-4o', 'gpt-4o-mini'])
  await user.click(screen.getByRole('button', { name: 'Copy all models' }))
  expect(copy).toHaveBeenCalledWith('gpt-4o,gpt-4o-mini')
})

it('asks the owner route when the root user manages another user key', async () => {
  const user = userEvent.setup()
  const get = vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: ['claude-sonnet'] },
  })
  renderCell(apiKey(), {
    owner: { id: 42, username: 'alice' },
    detailsTrigger: 'click',
  })

  await user.click(
    screen.getByRole('button', { name: 'Callable models: Unlimited' })
  )

  expect(
    await screen.findByRole('list', { name: 'Callable models' })
  ).toHaveTextContent('claude-sonnet')
  expect(get).toHaveBeenCalledWith('/api/user/42/tokens/7/models')
})

it('shows the server reason when the models cannot be loaded', async () => {
  const user = userEvent.setup()
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: false, message: 'record not found' },
  })
  renderCell(apiKey())

  await user.click(
    screen.getByRole('button', { name: 'Callable models: Unlimited' })
  )

  expect(await screen.findByText('record not found')).toBeVisible()
  expect(
    screen.queryByRole('button', { name: 'Copy all models' })
  ).not.toBeInTheDocument()
})
