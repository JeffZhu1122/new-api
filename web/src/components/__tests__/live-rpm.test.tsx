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
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import type { ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { afterEach, expect, it, vi } from 'vitest'

import {
  LiveRpmCell,
  LiveRpmContext,
  useLiveRpmTotals,
  type LiveRpmBreakdown,
  type LiveRpmBreakdownSource,
  type LiveRpmTotals,
} from '../live-rpm'

const i18n = createInstance()
await i18n.init({
  lng: 'en',
  resources: { en: { translation: {} } },
  initAsync: false,
})

const clients: QueryClient[] = []

afterEach(() => {
  cleanup()
  for (const client of clients.splice(0)) client.clear()
})

function totals(
  items: Record<string, number>,
  source: LiveRpmTotals['source'] = 'redis'
): LiveRpmTotals {
  return { source, window_start: 0, window_end: 60, items }
}

function renderWithProviders(ui: ReactNode, value?: LiveRpmTotals) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  const result = render(
    <QueryClientProvider client={client}>
      <I18nextProvider i18n={i18n}>
        <LiveRpmContext.Provider value={value}>{ui}</LiveRpmContext.Provider>
      </I18nextProvider>
    </QueryClientProvider>
  )
  return { ...result, client }
}

function breakdownSource(
  data: Partial<LiveRpmBreakdown>,
  extra: Partial<LiveRpmBreakdownSource> = {}
): LiveRpmBreakdownSource {
  return {
    title: 'RPM by user',
    description: 'window note',
    queryKey: ['live-rpm', 'test', Math.random()],
    fetch: vi.fn().mockResolvedValue({
      success: true,
      data: {
        source: 'redis',
        window_start: 0,
        window_end: 60,
        total: 0,
        items: [],
        ...data,
      },
    }),
    ...extra,
  }
}

it('sums the RPM of every channel in a tag row', () => {
  renderWithProviders(<LiveRpmCell ids={[1, 2]} />, totals({ 1: 3, 2: 4 }))

  expect(screen.getByText('7')).toBeInTheDocument()
})

it('shows a dash when statistics are disabled or the row has no reading yet', () => {
  renderWithProviders(<LiveRpmCell ids={[1]} />, totals({ 1: 5 }, 'disabled'))
  expect(screen.getByText('-')).toBeInTheDocument()
  cleanup()

  renderWithProviders(<LiveRpmCell ids={[1, 9]} />, totals({ 1: 5 }))
  expect(screen.getByText('-')).toBeInTheDocument()
})

it('opening the value loads the breakdown with name, id and RPM', async () => {
  const breakdown = breakdownSource({
    total: 5,
    items: [
      { id: 7, name: 'alice', rpm: 3 },
      { id: 8, name: '', rpm: 2 },
    ],
  })
  renderWithProviders(
    <LiveRpmCell ids={[1]} breakdown={breakdown} />,
    totals({ 1: 5 })
  )

  await userEvent.click(screen.getByRole('button', { name: 'RPM 5' }))

  expect(await screen.findByText('alice #7')).toBeInTheDocument()
  expect(screen.getByText('Unknown #8')).toBeInTheDocument()
  expect(screen.getByText('3')).toBeInTheDocument()
  expect(breakdown.fetch).toHaveBeenCalledTimes(1)
})

it('keeps the table offset off the clickable badge so the trigger cannot clip digits', () => {
  renderWithProviders(
    <LiveRpmCell ids={[1]} breakdown={breakdownSource({})} />,
    totals({ 1: 90 })
  )

  const trigger = screen.getByRole('button', { name: 'RPM 90' })
  const badge = trigger.querySelector('[data-slot=status-badge]')
  expect(badge).toHaveTextContent('90')
  expect(badge).not.toHaveClass('-ml-1.5')
  expect(trigger.parentElement).toHaveClass('-ml-1.5')
})

it('masks breakdown names but keeps ids when names are hidden', async () => {
  const breakdown = breakdownSource(
    { total: 3, items: [{ id: 7, name: 'alice', rpm: 3 }] },
    { maskNames: true }
  )
  renderWithProviders(
    <LiveRpmCell ids={[1]} breakdown={breakdown} />,
    totals({ 1: 3 })
  )

  await userEvent.click(screen.getByRole('button', { name: 'RPM 3' }))

  expect(await screen.findByText('•••• #7')).toBeInTheDocument()
  expect(screen.queryByText(/alice/)).not.toBeInTheDocument()
})

it('explains an empty breakdown and a single-instance count', async () => {
  const breakdown = breakdownSource({ source: 'memory' })
  renderWithProviders(
    <LiveRpmCell ids={[1]} breakdown={breakdown} />,
    totals({ 1: 0 }, 'memory')
  )

  await userEvent.click(screen.getByRole('button', { name: 'RPM 0' }))

  expect(
    await screen.findByText(
      'No requests in the last minute window note Counted on this server instance only.'
    )
  ).toBeInTheDocument()
})

function PolledCell(props: { fetchTotals: () => Promise<unknown> }) {
  const value = useLiveRpmTotals('test', [1], props.fetchTotals as never)
  return (
    <LiveRpmContext.Provider value={value}>
      <LiveRpmCell ids={[1]} />
    </LiveRpmContext.Provider>
  )
}

it('replaces polled totals with a dash once a refresh fails', async () => {
  const fetchTotals = vi
    .fn()
    .mockResolvedValueOnce({ success: true, data: totals({ 1: 12 }) })
    .mockResolvedValue({ success: false, message: 'redis down' })
  const { client } = renderWithProviders(
    <PolledCell fetchTotals={fetchTotals} />
  )
  expect(await screen.findByText('12')).toBeInTheDocument()
  expect(fetchTotals).toHaveBeenCalledWith([1])

  await client.refetchQueries({ queryKey: ['live-rpm', 'test'] })

  expect(await screen.findByText('-')).toBeInTheDocument()
  expect(screen.queryByText('12')).not.toBeInTheDocument()
})
