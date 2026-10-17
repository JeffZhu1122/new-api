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
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { ModelListPopover } from '../model-list-popover'

afterEach(() => {
  vi.restoreAllMocks()
})

function renderPopover(props: {
  models?: string[]
  loading?: boolean
  error?: string
}) {
  render(
    <ModelListPopover title='Models: 3' label='Models: 3 models' {...props}>
      3 models
    </ModelListPopover>
  )
  return screen.getByRole('button', { name: 'Models: 3 models' })
}

it('lists every model when the count is hovered', async () => {
  const user = userEvent.setup()
  const trigger = renderPopover({ models: ['gpt-4o', 'gpt-4o-mini', 'o3'] })

  await user.hover(trigger)

  const list = await screen.findByRole('list', { name: 'Models: 3' })
  expect(
    within(list)
      .getAllByRole('listitem')
      .map((item) => item.textContent)
  ).toEqual(['gpt-4o', 'gpt-4o-mini', 'o3'])
  expect(trigger).toHaveAttribute('aria-expanded', 'true')
})

it('copies all models as one comma-separated line', async () => {
  const user = userEvent.setup()
  const copy = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()
  const trigger = renderPopover({ models: ['gpt-4o', 'gpt-4o-mini', 'o3'] })

  await user.click(trigger)
  await user.click(
    await screen.findByRole('button', { name: 'Copy all models' })
  )

  expect(copy).toHaveBeenCalledWith('gpt-4o,gpt-4o-mini,o3')
  expect(
    await screen.findByRole('button', { name: 'Copied' })
  ).toBeInTheDocument()
})

it('opens from the keyboard', async () => {
  const user = userEvent.setup()
  const trigger = renderPopover({ models: ['gpt-4o'] })

  await user.tab()
  expect(trigger).toHaveFocus()
  await user.keyboard('{Enter}')

  expect(await screen.findByRole('list', { name: 'Models: 3' })).toBeVisible()
})

it.each([
  { state: 'loading', props: { loading: true }, text: 'Loading...' },
  {
    state: 'failed',
    props: { models: ['gpt-4o'], error: 'Token not found' },
    text: 'Token not found',
  },
  { state: 'empty', props: { models: [] }, text: 'No models' },
])('shows the $state state without a copy button', async ({ props, text }) => {
  const user = userEvent.setup()
  const trigger = renderPopover(props)

  await user.click(trigger)

  expect(await screen.findByText(text)).toBeVisible()
  expect(screen.queryByRole('list')).not.toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: 'Copy all models' })
  ).not.toBeInTheDocument()
})
