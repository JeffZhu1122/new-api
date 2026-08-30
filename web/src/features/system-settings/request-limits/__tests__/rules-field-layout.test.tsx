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
import { render, screen } from '@testing-library/react'
import type { ReactElement } from 'react'
import { describe, expect, test } from 'vitest'

import { ModelRateLimitSection } from '../model-rate-limit-section'
import { RateLimitSection } from '../rate-limit-section'

// The settings form grid only stretches a form item across both columns when
// it carries data-settings-form-span='full' (or contains a textarea, which the
// visual rules editor does not). These tests protect the full-width contract
// for the rules editors in their default visual mode.

const renderSection = (ui: ReactElement) => {
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  })
  render(<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>)
}

describe('rules field layout', () => {
  test('model rate limit rules form item spans the full settings grid in visual mode', () => {
    renderSection(
      <ModelRateLimitSection
        defaultValues={{
          ModelRateLimitEnabled: false,
          ModelRateLimitRules: '',
        }}
      />
    )

    const formItem = screen
      .getByText('RPM/TPM rules')
      .closest('[data-slot=form-item]')

    expect(formItem).not.toBeNull()
    expect(formItem).toHaveAttribute('data-settings-form-span', 'full')
    expect(screen.getByRole('button', { name: /Add rule/ })).toBeInTheDocument()
  })

  test('group rate limit rules form item spans the full settings grid in visual mode', () => {
    renderSection(
      <RateLimitSection
        defaultValues={{
          ModelRequestRateLimitEnabled: false,
          ModelRequestRateLimitDurationMinutes: 1,
          ModelRequestRateLimitCount: 0,
          ModelRequestRateLimitSuccessCount: 1,
          ModelRequestRateLimitGroup: '',
        }}
      />
    )

    const formItem = screen
      .getByText('Group-based rate limits')
      .closest('[data-slot=form-item]')

    expect(formItem).not.toBeNull()
    expect(formItem).toHaveAttribute('data-settings-form-span', 'full')
    expect(
      screen.getByRole('button', { name: /Add group/ })
    ).toBeInTheDocument()
  })
})
