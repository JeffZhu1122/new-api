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
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { OAuthProviders } from '../components/oauth-providers'

describe('OAuthProviders', () => {
  it('renders nothing when no OAuth provider is enabled', () => {
    const { container } = render(
      <OAuthProviders
        status={{
          github_oauth: false,
          discord_oauth: false,
          oidc_enabled: false,
          linuxdo_oauth: false,
          telegram_oauth: false,
          custom_oauth_providers: [],
        }}
      />
    )

    expect(container).toBeEmptyDOMElement()
  })

  it('shows the divider label and gates GitHub on the required consent', () => {
    const status = { github_oauth: true, github_client_id: 'github-client' }
    const { rerender } = render(<OAuthProviders status={status} disabled />)

    expect(screen.getByText('Or continue with')).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: /Continue with GitHub/ })
    ).toBeDisabled()

    rerender(<OAuthProviders status={status} disabled={false} />)

    expect(
      screen.getByRole('button', { name: /Continue with GitHub/ })
    ).toBeEnabled()
  })

  it('leaves provider buttons at the default outline size so the auth card sizes them like its other controls', () => {
    render(
      <OAuthProviders
        status={{ github_oauth: true, github_client_id: 'github-client' }}
      />
    )

    // The auth card's 44px control rule matches outline, non-icon buttons; a
    // fixed h-11 would scale with --spacing presets and outgrow the inputs.
    const github = screen.getByRole('button', { name: /Continue with GitHub/ })
    expect(github).toHaveAttribute('data-variant', 'outline')
    expect(github).toHaveAttribute('data-size', 'default')
    expect(github).not.toHaveClass('h-11')
  })
})
