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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { toast } from 'sonner'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { ResetPasswordConfirm, type ResetPasswordSearchParams } from '..'

afterEach(() => vi.restoreAllMocks())

async function renderResetPage(link: ResetPasswordSearchParams) {
  const router = createRouter({
    routeTree: createRootRoute({
      component: () => <ResetPasswordConfirm {...link} />,
    }),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  await router.load()
  render(
    <QueryClientProvider client={new QueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  await screen.findByRole('heading', { level: 2 })
}

async function choosePassword(password: string, confirmation: string) {
  const user = userEvent.setup()
  await user.type(screen.getByLabelText('New Password'), password)
  await user.type(screen.getByLabelText('Confirm New Password'), confirmation)
  await user.click(
    screen.getByRole('button', { name: 'auth.resetPasswordConfirm.confirm' })
  )
}

const link = { email: 'owner@example.com', token: 'mailed-token' }

describe('password reset page', () => {
  test('sends the chosen password with the reset link and shows no password afterwards', async () => {
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValue({ data: { success: true, message: '' } })
    await renderResetPage(link)

    await choosePassword('chosen-password', 'chosen-password')

    expect(post).toHaveBeenCalledWith(
      '/api/user/reset',
      {
        email: 'owner@example.com',
        token: 'mailed-token',
        password: 'chosen-password',
      },
      expect.anything()
    )
    expect(
      await screen.findByRole('button', {
        name: 'auth.resetPasswordConfirm.backToLogin',
      })
    ).toBeInTheDocument()
    expect(screen.queryByLabelText('New Password')).not.toBeInTheDocument()
    expect(document.body).not.toHaveTextContent('chosen-password')
  })

  test('does not send passwords that differ', async () => {
    const post = vi.spyOn(api, 'post')
    await renderResetPage(link)

    await choosePassword('chosen-password', 'chosen-passw0rd')

    expect(
      await screen.findByText('Passwords do not match')
    ).toBeInTheDocument()
    expect(post).not.toHaveBeenCalled()
  })

  test('does not send a password shorter than the policy allows', async () => {
    const post = vi.spyOn(api, 'post')
    await renderResetPage(link)

    await choosePassword('short', 'short')

    expect(
      await screen.findByText(
        'Password must contain between 8 and 128 characters.'
      )
    ).toBeInTheDocument()
    expect(post).not.toHaveBeenCalled()
  })

  test('keeps the form open when the server refuses the reset', async () => {
    vi.spyOn(api, 'post').mockResolvedValue({
      data: {
        success: false,
        message: 'Password reset link is invalid or has expired',
      },
    })
    const error = vi.spyOn(toast, 'error')
    await renderResetPage(link)

    await choosePassword('chosen-password', 'chosen-password')

    await waitFor(() => expect(error).toHaveBeenCalled())
    expect(screen.getByLabelText('New Password')).toBeInTheDocument()
    expect(
      screen.queryByRole('button', {
        name: 'auth.resetPasswordConfirm.backToLogin',
      })
    ).not.toBeInTheDocument()
  })

  test('disables the form for a link without a token', async () => {
    await renderResetPage({ email: 'owner@example.com' })

    expect(
      screen.getByText(
        'Invalid reset link, please request a new password reset.'
      )
    ).toBeInTheDocument()
    expect(screen.getByLabelText('New Password')).toBeDisabled()
    expect(
      screen.getByRole('button', { name: 'auth.resetPasswordConfirm.confirm' })
    ).toBeDisabled()
  })
})
