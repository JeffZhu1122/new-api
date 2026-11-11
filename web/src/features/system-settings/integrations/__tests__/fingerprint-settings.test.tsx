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
import type { FingerprintProfile } from '../fingerprint-profile-schema'
import { FingerprintSettingsSection } from '../fingerprint-settings-section'

const HTTP2_FINGERPRINT = '1:65536;2:0;4:4194304;6:262144|15663105|0|m,a,s,p'

const storedProfile: FingerprintProfile = {
  name: 'chrome',
  tls_preset: 'chrome_131',
  http2_fingerprint: HTTP2_FINGERPRINT,
  user_agent: 'Mozilla/5.0 stored',
}

function Fixture(props: { profiles: FingerprintProfile[] }) {
  const [container, setContainer] = useState<HTMLDivElement | null>(null)
  return (
    <>
      <div ref={setContainer} />
      <SettingsPageProvider actionsContainer={container}>
        <FingerprintSettingsSection enabled profiles={props.profiles} />
      </SettingsPageProvider>
    </>
  )
}

function renderSettings(profiles: FingerprintProfile[] = [storedProfile]) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <Fixture profiles={profiles} />
    </QueryClientProvider>
  )
}

async function expectStoredProfiles(profiles: FingerprintProfile[]) {
  await waitFor(() => expect(api.put).toHaveBeenCalled())
  const [url, body] = vi.mocked(api.put).mock.calls[0]
  const request = body as { key: string; value: string }
  expect(url).toBe('/api/option/')
  expect(request.key).toBe('fingerprint_setting.profiles')
  expect(JSON.parse(request.value)).toEqual(profiles)
}

async function openDelete(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByRole('button', { name: 'Open menu' }))
  await user.click(await screen.findByRole('menuitem', { name: 'Delete' }))
  return screen.findByRole('alertdialog')
}

beforeEach(() => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        enabled: true,
        profiles: ['chrome'],
        tls_presets: ['chrome_131', 'chrome_133', 'firefox_120'],
      },
    },
  })
  vi.spyOn(api, 'put').mockResolvedValue({ data: { success: true } })
})

afterEach(() => vi.restoreAllMocks())

test('a stored profile is listed with its TLS preset and HTTP/2 fingerprint', async () => {
  renderSettings()

  const row = (await screen.findByText('chrome')).closest('tr')
  expect(row).not.toBeNull()
  expect(within(row as HTMLElement).getByText('chrome_131')).toBeVisible()
  expect(within(row as HTMLElement).getByText(HTTP2_FINGERPRINT)).toBeVisible()
  expect(
    within(row as HTMLElement).getByText('Mozilla/5.0 stored')
  ).toBeVisible()
})

test('adding a profile stores the whole list and omits the parts left empty', async () => {
  const user = userEvent.setup()
  renderSettings()
  await user.click(await screen.findByRole('button', { name: 'Add profile' }))
  const dialog = await screen.findByRole('dialog')

  await user.type(within(dialog).getByLabelText('Name'), 'firefox')
  await user.click(
    within(dialog).getByRole('combobox', { name: 'TLS ClientHello' })
  )
  await user.click(await screen.findByRole('option', { name: 'firefox_120' }))
  await user.type(
    within(dialog).getByLabelText('Header order'),
    'Host\n\n content-type \nUser-Agent'
  )
  await user.click(within(dialog).getByRole('button', { name: 'Add' }))

  await expectStoredProfiles([
    storedProfile,
    {
      name: 'firefox',
      tls_preset: 'firefox_120',
      header_order: ['Host', 'content-type', 'User-Agent'],
    },
  ])
})

test('a name another profile already uses is refused before anything is stored', async () => {
  const user = userEvent.setup()
  renderSettings()
  await user.click(await screen.findByRole('button', { name: 'Add profile' }))
  const dialog = await screen.findByRole('dialog')

  await user.type(within(dialog).getByLabelText('Name'), 'chrome')
  await user.click(within(dialog).getByRole('button', { name: 'Add' }))

  expect(
    await within(dialog).findByText('A profile with this name already exists')
  ).toBeVisible()
  expect(api.put).not.toHaveBeenCalled()
})

test('a custom capture that is not hex is refused before anything is stored', async () => {
  const user = userEvent.setup()
  renderSettings()
  await user.click(await screen.findByRole('button', { name: 'Add profile' }))
  const dialog = await screen.findByRole('dialog')
  await user.type(within(dialog).getByLabelText('Name'), 'captured')
  await user.click(
    within(dialog).getByRole('combobox', { name: 'TLS ClientHello' })
  )
  await user.click(
    await screen.findByRole('option', { name: 'Custom capture (hex)' })
  )

  await user.type(
    within(dialog).getByLabelText('ClientHello capture'),
    '16 03 01 zz'
  )
  await user.click(within(dialog).getByRole('button', { name: 'Add' }))

  expect(
    await within(dialog).findByText(
      'Paste the hex dump of a captured ClientHello'
    )
  ).toBeVisible()
  expect(api.put).not.toHaveBeenCalled()
})

test('editing a profile keeps its name fixed and replaces it in place', async () => {
  const user = userEvent.setup()
  const other: FingerprintProfile = { name: 'other', user_agent: 'curl/8.0' }
  renderSettings([storedProfile, other])
  const row = (await screen.findByText('chrome')).closest('tr')
  await user.click(
    within(row as HTMLElement).getByRole('button', { name: 'Edit' })
  )
  const dialog = await screen.findByRole('dialog')
  expect(within(dialog).getByLabelText('Name')).toBeDisabled()

  const userAgent = within(dialog).getByLabelText('Default User-Agent')
  await user.clear(userAgent)
  await user.type(userAgent, 'Mozilla/5.0 edited')
  await user.click(within(dialog).getByRole('button', { name: 'Update' }))

  await expectStoredProfiles([
    { ...storedProfile, user_agent: 'Mozilla/5.0 edited' },
    other,
  ])
})

test('deleting a profile stores the list without it', async () => {
  const user = userEvent.setup()
  renderSettings()
  const confirm = await openDelete(user)
  await user.click(within(confirm).getByRole('button', { name: 'Continue' }))

  await expectStoredProfiles([])
  await waitFor(() =>
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  )
})

test('a deletion the gateway refuses leaves the confirmation open', async () => {
  vi.mocked(api.put).mockResolvedValue({
    data: {
      success: false,
      message:
        'fingerprint profile "chrome" is still selected by channel #3 (main); clear it on the channel first',
    },
  })
  const user = userEvent.setup()
  renderSettings()
  const confirm = await openDelete(user)
  const confirmButton = within(confirm).getByRole('button', {
    name: 'Continue',
  })

  await user.click(confirmButton)

  await waitFor(() => expect(api.put).toHaveBeenCalledTimes(1))
  await waitFor(() => expect(confirmButton).toBeEnabled())
  expect(screen.getByRole('alertdialog')).toBeVisible()
})

test('saving the switch stores the changed kill switch', async () => {
  const user = userEvent.setup()
  renderSettings()

  await user.click(
    await screen.findByRole('switch', { name: 'Allow fingerprint profiles' })
  )
  await user.click(
    screen.getByRole('button', { name: 'Save outbound fingerprint settings' })
  )

  await waitFor(() =>
    expect(api.put).toHaveBeenCalledWith('/api/option/', {
      key: 'fingerprint_setting.enabled',
      value: false,
    })
  )
})
