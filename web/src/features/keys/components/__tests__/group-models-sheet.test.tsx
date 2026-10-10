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
import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { USER_GROUP_MODELS_QUERY_KEY } from '../../hooks/use-user-group-models'
import type { UserGroupModels } from '../../types'
import { GroupModelsLink, GroupModelsSheet } from '../group-models-sheet'

const GROUPS: UserGroupModels[] = [
  {
    group: 'default',
    desc: 'Default group',
    ratio: 1,
    models: ['claude-sonnet', 'gpt-4o'],
  },
  { group: 'vip', desc: 'VIP group', ratio: 1.5, models: ['gpt-4o', 'o3'] },
  { group: 'empty', desc: '', ratio: 1, models: [] },
  {
    group: 'auto',
    desc: 'Auto group',
    ratio: 'auto',
    auto_groups: ['vip', 'default'],
    models: ['claude-sonnet', 'gpt-4o', 'o3'],
  },
]

let client: QueryClient

beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { enabled: false, retry: false } },
  })
  client.setQueryData(USER_GROUP_MODELS_QUERY_KEY, GROUPS)
})

afterEach(() => {
  cleanup()
  client.clear()
  vi.restoreAllMocks()
})

function renderSheet(initialGroup?: string) {
  return render(
    <QueryClientProvider client={client}>
      <GroupModelsSheet
        open
        onOpenChange={() => {}}
        initialGroup={initialGroup}
      />
    </QueryClientProvider>
  )
}

function sections(): HTMLElement[] {
  return within(screen.getByRole('dialog')).queryAllByRole('region')
}

function modelsIn(section: HTMLElement): string[] {
  return within(section)
    .queryAllByRole('listitem')
    .map((item) => item.textContent ?? '')
}

it('lists every group with its models, count and auto order', () => {
  renderSheet()

  const [defaultGroup, vip, empty, auto] = sections()
  expect(modelsIn(defaultGroup)).toEqual(['claude-sonnet', 'gpt-4o'])
  expect(defaultGroup).toHaveTextContent('Models: 2')
  expect(modelsIn(vip)).toEqual(['gpt-4o', 'o3'])
  expect(empty).toHaveTextContent('No models available in this group')
  expect(modelsIn(auto)).toEqual(['claude-sonnet', 'gpt-4o', 'o3'])
  expect(auto).toHaveTextContent('vip → default')
})

it('shows only the chosen group after picking it in the filter', async () => {
  const user = userEvent.setup()
  renderSheet()

  await user.click(screen.getByRole('button', { name: /^vip/ }))

  const visible = sections()
  expect(visible).toHaveLength(1)
  expect(modelsIn(visible[0])).toEqual(['gpt-4o', 'o3'])
  expect(screen.getByRole('button', { name: /^vip/ })).toHaveAttribute(
    'aria-pressed',
    'true'
  )
})

it('opens on the initial group and falls back to every group when it is unknown', () => {
  const { unmount } = renderSheet('vip')
  expect(sections()).toHaveLength(1)
  unmount()

  renderSheet('missing')
  expect(sections()).toHaveLength(4)
})

it('filters models across groups by the search term', async () => {
  const user = userEvent.setup()
  renderSheet()

  await user.type(screen.getByRole('textbox', { name: 'Search models' }), 'O3')

  const visible = sections()
  expect(visible.map((section) => modelsIn(section))).toEqual([['o3'], ['o3']])
})

it('shows an empty state when no model matches the search term', async () => {
  const user = userEvent.setup()
  renderSheet()

  await user.type(
    screen.getByRole('textbox', { name: 'Search models' }),
    'no-such-model'
  )

  expect(sections()).toHaveLength(0)
  expect(screen.getByText('No matching models')).toBeVisible()
})

it('copies every model name of a group as a comma separated list', async () => {
  const user = userEvent.setup()
  const writeText = vi.spyOn(navigator.clipboard, 'writeText')
  renderSheet('default')

  await user.click(
    screen.getByRole('button', { name: 'Copy all model names in default' })
  )

  expect(writeText).toHaveBeenCalledWith('claude-sonnet,gpt-4o')
})

it('shows the model count of the picked group and opens the sheet on it', async () => {
  const user = userEvent.setup()
  render(
    <QueryClientProvider client={client}>
      <GroupModelsLink group='vip' />
    </QueryClientProvider>
  )

  await user.click(
    screen.getByRole('button', {
      name: 'Models available in this group: 2',
    })
  )

  const visible = sections()
  expect(visible).toHaveLength(1)
  expect(modelsIn(visible[0])).toEqual(['gpt-4o', 'o3'])
})

it('offers the full list when the key has no group yet', () => {
  render(
    <QueryClientProvider client={client}>
      <GroupModelsLink group='' />
    </QueryClientProvider>
  )

  expect(
    screen.getByRole('button', { name: 'View models by group' })
  ).toBeVisible()
})
