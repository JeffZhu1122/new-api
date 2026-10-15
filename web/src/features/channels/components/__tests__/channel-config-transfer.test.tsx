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
import type { Row } from '@tanstack/react-table'
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import type { ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { copyToClipboard } from '@/lib/copy-to-clipboard'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { CHANNEL_STATUS, CHANNEL_TYPE_CODEX } from '../../constants'
import { encodeChannelConfig } from '../../lib/channel-config-transfer'
import type { ChannelFormValues } from '../../lib/channel-form'
import type { Channel } from '../../types'
import { ChannelsProvider, useChannels } from '../channels-provider'
import { DataTableRowActions } from '../data-table-row-actions'
import { CopyChannelConfigDialog } from '../dialogs/copy-channel-config-dialog'
import { PasteChannelConfigDialog } from '../dialogs/paste-channel-config-dialog'

vi.mock('@/lib/copy-to-clipboard', () => ({
  copyToClipboard: vi.fn(async () => true),
}))

const i18n = createInstance()
await i18n.init({
  lng: 'en',
  resources: { en: { translation: {} } },
  initAsync: false,
})

function sourceChannel(overrides: Partial<Channel> = {}): Channel {
  return {
    id: 7,
    type: 1,
    key: 'sk-copied-secret',
    status: CHANNEL_STATUS.ENABLED,
    name: 'Upstream A',
    created_time: 0,
    test_time: 0,
    response_time: 0,
    base_url: 'https://upstream.example.com',
    other: '',
    balance: 0,
    balance_updated_time: 0,
    models: 'gpt-4o',
    group: 'default',
    used_quota: 0,
    other_info: '',
    remark: '',
    max_input_tokens: 0,
    channel_info: {
      is_multi_key: false,
      multi_key_size: 0,
      multi_key_polling_index: 0,
      multi_key_mode: 'random',
    },
    settings: '{}',
    extend_config: { force_retry: true },
    ...overrides,
  }
}

function renderWithProviders(children: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    </I18nextProvider>
  )
}

// The channel list hands the dialog a channel without its key; the key only
// arrives from the 2FA-protected config endpoint.
function mockConfigCopyApi(config: Channel) {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        scope: 'channel.config.copy',
        methods: [{ method: '2fa', available: true }],
        oauth_providers: [],
        password_encryption_enabled: false,
      },
    },
  })
  return vi.spyOn(api, 'post').mockImplementation(async (url) => {
    if (url === '/api/verify') {
      return {
        data: {
          success: true,
          data: {
            proof_token: 'config-proof',
            method: '2fa',
            scope: 'channel.config.copy',
            expires_at: Math.floor(Date.now() / 1000) + 60,
          },
        },
      }
    }
    if (url === `/api/channel/${config.id}/config`) {
      return { data: { success: true, data: config } }
    }
    throw new Error(`Unexpected POST ${url}`)
  })
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.mocked(copyToClipboard).mockClear()
  useAuthStore.getState().auth.setUser(null)
})

describe('Copy Full Configuration', () => {
  it('copies the configuration with its keys only after 2FA', async () => {
    const config = sourceChannel()
    const post = mockConfigCopyApi(config)
    const user = userEvent.setup()
    renderWithProviders(
      <CopyChannelConfigDialog
        channel={sourceChannel({ key: '' })}
        onClose={vi.fn()}
      />
    )
    expect(
      screen.queryByRole('button', { name: 'Copy configuration' })
    ).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Verify with 2FA' }))
    await user.type(
      await screen.findByLabelText('Authenticator code or backup code'),
      '123456'
    )
    await user.click(screen.getByRole('button', { name: 'Verify' }))
    await user.click(
      await screen.findByRole('button', { name: 'Copy configuration' })
    )

    expect(post).toHaveBeenCalledWith(
      '/api/verify',
      expect.objectContaining({
        method: '2fa',
        scope: 'channel.config.copy',
        context: { channel_id: 7 },
      }),
      expect.anything()
    )
    expect(post).toHaveBeenCalledWith(
      '/api/channel/7/config',
      undefined,
      expect.objectContaining({
        headers: expect.objectContaining({
          'X-Security-Proof': 'config-proof',
        }),
      })
    )
    expect(copyToClipboard).toHaveBeenCalledWith(encodeChannelConfig(config))
  })

  it('fetches and copies nothing when 2FA is cancelled', async () => {
    const post = mockConfigCopyApi(sourceChannel())
    const user = userEvent.setup()
    renderWithProviders(
      <CopyChannelConfigDialog channel={sourceChannel()} onClose={vi.fn()} />
    )

    await user.click(screen.getByRole('button', { name: 'Verify with 2FA' }))
    const verification = await screen.findByRole('dialog', {
      name: 'Verify to copy channel configuration',
    })
    await user.click(
      within(verification).getByRole('button', { name: 'Cancel' })
    )

    await waitFor(() =>
      expect(
        screen.queryByRole('dialog', {
          name: 'Verify to copy channel configuration',
        })
      ).not.toBeInTheDocument()
    )
    expect(post).not.toHaveBeenCalled()
    expect(
      screen.queryByRole('button', { name: 'Copy configuration' })
    ).not.toBeInTheDocument()
    expect(copyToClipboard).not.toHaveBeenCalled()
  })

  it('warns that a Codex credential used on two sites signs one of them out', () => {
    renderWithProviders(
      <CopyChannelConfigDialog
        channel={sourceChannel({ type: CHANNEL_TYPE_CODEX })}
        onClose={vi.fn()}
      />
    )
    expect(
      screen.getByText(/If two sites use the same credential/)
    ).toBeInTheDocument()
  })

  it.each([
    ['the root user', ROLE.SUPER_ADMIN, true],
    ['an admin', ROLE.ADMIN, false],
  ])(
    'offers it in the row menu only to the root user (%s)',
    async (_, role, offered) => {
      useAuthStore
        .getState()
        .auth.setUser({ id: 1, username: 'operator', role })
      const user = userEvent.setup()
      const row = { original: sourceChannel() } as Row<Channel>
      renderWithProviders(
        <ChannelsProvider>
          <DataTableRowActions row={row} />
        </ChannelsProvider>
      )

      await user.click(screen.getByRole('button', { name: 'Open menu' }))
      await screen.findByRole('menuitem', { name: /Test Connection/ })
      expect(
        screen.queryByRole('menuitem', { name: /Copy Full Configuration/ })
      ).toEqual(offered ? expect.anything() : null)
    }
  )
})

describe('Paste Channel', () => {
  it('flags text that is not a copied configuration and keeps the dialog open', async () => {
    const onPaste = vi.fn()
    const user = userEvent.setup()
    renderWithProviders(
      <PasteChannelConfigDialog onPaste={onPaste} onClose={vi.fn()} />
    )

    const input = screen.getByLabelText('Channel configuration')
    await user.click(input)
    await user.paste('sk-just-a-key')
    await user.click(screen.getByRole('button', { name: 'Continue' }))

    expect(screen.getByRole('alert')).toHaveTextContent(
      'This is not a copied channel configuration.'
    )
    expect(input).toHaveAttribute('aria-invalid', 'true')
    expect(onPaste).not.toHaveBeenCalled()
  })

  it('hands a copied configuration to the create form, disabled until tested', async () => {
    const onPaste = vi.fn()
    const user = userEvent.setup()
    renderWithProviders(
      <PasteChannelConfigDialog onPaste={onPaste} onClose={vi.fn()} />
    )

    await user.click(screen.getByLabelText('Channel configuration'))
    await user.paste(encodeChannelConfig(sourceChannel()))
    await user.click(screen.getByRole('button', { name: 'Continue' }))

    expect(onPaste).toHaveBeenCalledWith(
      expect.objectContaining({
        name: 'Upstream A',
        key: 'sk-copied-secret',
        base_url: 'https://upstream.example.com',
        force_retry: true,
        status: CHANNEL_STATUS.MANUAL_DISABLED,
      })
    )
  })

  it('drops a pasted draft once any other dialog opens, so Create Channel starts empty', async () => {
    const draft = { name: 'Pasted' } as ChannelFormValues
    function DraftProbe() {
      const channels = useChannels()
      return (
        <>
          <button
            type='button'
            onClick={() => channels.openChannelDraft(draft)}
          >
            Paste
          </button>
          <button
            type='button'
            onClick={() => channels.setOpen('create-channel')}
          >
            Create
          </button>
          <output aria-label='Draft'>
            {`${channels.open}:${channels.channelDraft?.name ?? 'none'}`}
          </output>
        </>
      )
    }
    const user = userEvent.setup()
    renderWithProviders(
      <ChannelsProvider>
        <DraftProbe />
      </ChannelsProvider>
    )

    await user.click(screen.getByRole('button', { name: 'Paste' }))
    expect(screen.getByLabelText('Draft')).toHaveTextContent(
      'create-channel:Pasted'
    )
    await user.click(screen.getByRole('button', { name: 'Create' }))
    expect(screen.getByLabelText('Draft')).toHaveTextContent(
      'create-channel:none'
    )
  })
})
