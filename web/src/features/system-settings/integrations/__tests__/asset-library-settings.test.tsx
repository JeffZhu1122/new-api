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
import { useState } from 'react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import {
  AssetLibrarySettingsSection,
  type AssetLibrarySettingsValues,
} from '../asset-library-settings-section'

const storedAccount = {
  id: 3,
  name: 'Main account',
  access_key_id_masked: 'AKLT****3456',
  has_secret: true,
  region: 'cn-beijing',
  project_name: 'video',
  endpoint: '',
  enabled: true,
  aigc_enabled: true,
  liveness_face_enabled: false,
  liveness_face_verify: false,
  allowed_user_groups: [],
  priority: 0,
  max_groups: 1000000,
  max_assets: 1000000,
  create_asset_qpm: 120,
  created_time: 1,
  updated_time: 1,
}

const defaultValues: AssetLibrarySettingsValues = {
  enabled: false,
  block_asset_refs_when_disabled: false,
  auto_upload_references: false,
  max_groups_per_user: 20,
  max_assets_per_user: 200,
  verify_sessions_per_hour: 5,
  allowed_user_groups: [],
  compliance_notice: '',
}

function Fixture() {
  const [container, setContainer] = useState<HTMLDivElement | null>(null)
  return (
    <>
      <div ref={setContainer} />
      <SettingsPageProvider actionsContainer={container}>
        <AssetLibrarySettingsSection defaultValues={defaultValues} />
      </SettingsPageProvider>
    </>
  )
}

function renderSettings() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <Fixture />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  vi.spyOn(api, 'get').mockImplementation(async (url: string) => {
    if (url === '/api/group/') {
      return { data: { success: true, data: ['default', 'vip'] } }
    }
    return { data: { success: true, data: [storedAccount] } }
  })
  vi.spyOn(api, 'put').mockResolvedValue({ data: { success: true } })
  vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, data: storedAccount },
  })
})

afterEach(() => vi.restoreAllMocks())

test('the account list shows the masked Access Key ID and never asks for the stored secret', async () => {
  renderSettings()

  const row = (await screen.findByText('Main account')).closest('tr')
  expect(row).not.toBeNull()
  expect(within(row as HTMLElement).getByText('AKLT****3456')).toBeVisible()
  expect(
    within(row as HTMLElement).getByText('cn-beijing / video')
  ).toBeVisible()
  expect(within(row as HTMLElement).getByText('Virtual avatars')).toBeVisible()
})

test('a new account needs both keys before it can be added', async () => {
  const user = userEvent.setup()
  renderSettings()
  await user.click(await screen.findByRole('button', { name: 'Add account' }))
  const dialog = await screen.findByRole('dialog')

  await user.type(within(dialog).getByLabelText('Name'), 'Backup')
  await user.click(within(dialog).getByRole('button', { name: 'Add' }))

  expect(
    await within(dialog).findByText('Access Key ID is required')
  ).toBeVisible()
  expect(
    within(dialog).getByText('Secret Access Key is required')
  ).toBeVisible()
  expect(api.post).not.toHaveBeenCalled()
})

test.each([
  'https://ark.cn-beijing.volces.com/api/v3',
  'https://ark.ap-southeast.bytepluses.com/api/v3',
])(
  'the model inference address %s is refused as the endpoint',
  async (endpoint) => {
    const user = userEvent.setup()
    renderSettings()
    await user.click(await screen.findByRole('button', { name: 'Add account' }))
    const dialog = await screen.findByRole('dialog')

    await user.type(within(dialog).getByLabelText('Name'), 'Backup')
    await user.type(within(dialog).getByLabelText('Access Key ID'), 'AKLTnew')
    await user.type(within(dialog).getByLabelText('Secret Access Key'), 'sk')
    await user.type(
      within(dialog).getByLabelText('Endpoint (optional)'),
      endpoint
    )
    await user.click(within(dialog).getByRole('button', { name: 'Add' }))

    expect(
      await within(dialog).findByText(
        'This is a model inference address, which does not accept Access Keys. Leave the endpoint blank for Volcengine, or enter https://ark.ap-southeast-1.byteplusapi.com for BytePlus.'
      )
    ).toBeVisible()
    expect(api.post).not.toHaveBeenCalled()
  }
)

test('testing a new account sends the typed keys without saving it', async () => {
  const user = userEvent.setup()
  renderSettings()
  await user.click(await screen.findByRole('button', { name: 'Add account' }))
  const dialog = await screen.findByRole('dialog')
  await user.type(within(dialog).getByLabelText('Name'), 'Backup')
  await user.type(within(dialog).getByLabelText('Access Key ID'), 'AKLTnew')
  await user.type(within(dialog).getByLabelText('Secret Access Key'), 'sk-new')

  await user.click(
    within(dialog).getByRole('button', { name: 'Test connection' })
  )

  await waitFor(() =>
    expect(api.post).toHaveBeenCalledWith(
      '/api/asset-library/accounts/test',
      expect.objectContaining({
        name: 'Backup',
        access_key_id: 'AKLTnew',
        secret_access_key: 'sk-new',
        region: 'cn-beijing',
        project_name: 'default',
      })
    )
  )
  expect(api.post).toHaveBeenCalledTimes(1)
})

test('editing an account keeps the stored keys when the key fields stay empty', async () => {
  const user = userEvent.setup()
  renderSettings()
  const row = (await screen.findByText('Main account')).closest('tr')
  await user.click(
    within(row as HTMLElement).getByRole('button', { name: 'Edit' })
  )
  const dialog = await screen.findByRole('dialog')
  expect(within(dialog).getByLabelText('Secret Access Key')).toHaveValue('')

  await user.click(within(dialog).getByRole('button', { name: 'Update' }))

  await waitFor(() =>
    expect(api.put).toHaveBeenCalledWith(
      '/api/asset-library/accounts',
      expect.objectContaining({
        id: 3,
        name: 'Main account',
        access_key_id: '',
        secret_access_key: '',
        project_name: 'video',
      })
    )
  )
})

test('the face verification switch appears once real-person groups are on and is saved with the account', async () => {
  const user = userEvent.setup()
  renderSettings()
  const row = (await screen.findByText('Main account')).closest('tr')
  await user.click(
    within(row as HTMLElement).getByRole('button', { name: 'Edit' })
  )
  const dialog = await screen.findByRole('dialog')
  expect(
    within(dialog).queryByRole('switch', { name: 'Face verification required' })
  ).not.toBeInTheDocument()

  await user.click(within(dialog).getByRole('switch', { name: 'Real people' }))
  await user.click(
    within(dialog).getByRole('switch', { name: 'Face verification required' })
  )
  await user.click(within(dialog).getByRole('button', { name: 'Update' }))

  await waitFor(() =>
    expect(api.put).toHaveBeenCalledWith(
      '/api/asset-library/accounts',
      expect.objectContaining({
        id: 3,
        liveness_face_enabled: true,
        liveness_face_verify: true,
      })
    )
  )
})

test('saving the switches stores only the changed options', async () => {
  const user = userEvent.setup()
  renderSettings()
  await user.click(
    await screen.findByRole('switch', { name: 'Enable the asset library' })
  )

  await user.click(
    screen.getByRole('button', { name: 'Save asset library settings' })
  )

  await waitFor(() =>
    expect(api.put).toHaveBeenCalledWith('/api/option/', {
      key: 'asset_library_setting.enabled',
      value: true,
    })
  )
  expect(api.put).toHaveBeenCalledTimes(1)
})

test('turning on automatic reference uploads saves that option', async () => {
  const user = userEvent.setup()
  renderSettings()
  await user.click(
    await screen.findByRole('switch', {
      name: 'Upload URL references automatically',
    })
  )

  await user.click(
    screen.getByRole('button', { name: 'Save asset library settings' })
  )

  await waitFor(() =>
    expect(api.put).toHaveBeenCalledWith('/api/option/', {
      key: 'asset_library_setting.auto_upload_references',
      value: true,
    })
  )
  expect(api.put).toHaveBeenCalledTimes(1)
})
