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
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useEffect } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { channelSchema } from '../../types'
import { ChannelsProvider, useChannels } from '../channels-provider'
import { ChannelTestDialog } from '../dialogs/channel-test-dialog'

const channel = channelSchema.parse({
  id: 7,
  type: 1,
  key: '',
  name: 'OpenAI',
  models: 'gpt-x',
  status: 1,
  created_time: 1,
  test_time: 0,
  response_time: 0,
  balance_updated_time: 0,
})
let client: QueryClient

function OpenTestDialog() {
  const { currentRow, setCurrentRow } = useChannels()
  useEffect(() => setCurrentRow(channel), [setCurrentRow])
  return currentRow ? <ChannelTestDialog open onOpenChange={vi.fn()} /> : null
}

async function renderDialog() {
  render(
    <QueryClientProvider client={client}>
      <ChannelsProvider>
        <OpenTestDialog />
      </ChannelsProvider>
    </QueryClientProvider>
  )
  await screen.findByRole('combobox', { name: 'Endpoint Type' })
}

async function chooseEndpoint(name: RegExp) {
  await userEvent.click(screen.getByRole('combobox', { name: 'Endpoint Type' }))
  await userEvent.click(await screen.findByRole('option', { name }))
}

function testRequestParams() {
  const call = vi
    .mocked(api.get)
    .mock.calls.find(([url]) => url === '/api/channel/test/7')
  return (call?.[1] as { params?: Record<string, unknown> } | undefined)?.params
}

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, time: 0.2 },
  })
})

afterEach(() => {
  cleanup()
  client.clear()
  vi.restoreAllMocks()
})

it('shows the max tokens field only for the Chat Completions endpoint', async () => {
  await renderDialog()
  expect(
    screen.queryByRole('combobox', { name: 'Max tokens field' })
  ).not.toBeInTheDocument()

  await chooseEndpoint(/\/v1\/chat\/completions/)
  expect(
    screen.getByRole('combobox', { name: 'Max tokens field' })
  ).toHaveTextContent('Auto (by model name)')

  await chooseEndpoint(/^OpenAI Responses/)
  expect(
    screen.queryByRole('combobox', { name: 'Max tokens field' })
  ).not.toBeInTheDocument()
})

it('sends the chosen max tokens field with a Chat Completions test', async () => {
  await renderDialog()
  await chooseEndpoint(/\/v1\/chat\/completions/)
  await userEvent.click(
    screen.getByRole('combobox', { name: 'Max tokens field' })
  )
  await userEvent.click(
    await screen.findByRole('option', { name: 'max_completion_tokens' })
  )
  await userEvent.click(screen.getByRole('button', { name: 'Test Connection' }))

  await waitFor(() =>
    expect(testRequestParams()).toEqual({
      model: 'gpt-x',
      endpoint_type: 'openai',
      max_tokens_param: 'max_completion_tokens',
    })
  )
})

it('leaves the field choice to the backend when the field is auto', async () => {
  await renderDialog()
  await chooseEndpoint(/\/v1\/chat\/completions/)
  await userEvent.click(screen.getByRole('button', { name: 'Test Connection' }))

  await waitFor(() =>
    expect(testRequestParams()).toEqual({
      model: 'gpt-x',
      endpoint_type: 'openai',
    })
  )
})

it('drops a forced field after switching to another endpoint', async () => {
  await renderDialog()
  await chooseEndpoint(/\/v1\/chat\/completions/)
  await userEvent.click(
    screen.getByRole('combobox', { name: 'Max tokens field' })
  )
  await userEvent.click(
    await screen.findByRole('option', { name: 'max_tokens' })
  )
  await chooseEndpoint(/^OpenAI Responses/)
  await userEvent.click(screen.getByRole('button', { name: 'Test Connection' }))

  await waitFor(() =>
    expect(testRequestParams()).toEqual({
      model: 'gpt-x',
      endpoint_type: 'openai-response',
    })
  )
})
