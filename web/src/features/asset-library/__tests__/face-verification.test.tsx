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
import { render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { localizedVerificationLink } from '../lib/verification-link'
import { VerificationResult } from '../verification-result'

beforeEach(() => {
  vi.spyOn(api, 'post').mockResolvedValue({ data: { success: true } })
})

afterEach(() => {
  vi.restoreAllMocks()
})

test('the verification link gets the interface language and keeps every other parameter byte for byte', () => {
  const link =
    'https://h5.example/?accessKeyId=AK&sessionToken=a%2Bb.c&lng=zh&bytedToken=t'

  expect(localizedVerificationLink(link, 'zhTW')).toBe(
    'https://h5.example/?accessKeyId=AK&sessionToken=a%2Bb.c&lng=zh-Hant&bytedToken=t'
  )
  expect(localizedVerificationLink(link, 'zhCN')).toBe(link)
  expect(localizedVerificationLink(link, 'fr')).toBe(
    'https://h5.example/?accessKeyId=AK&sessionToken=a%2Bb.c&lng=en&bytedToken=t'
  )
  expect(localizedVerificationLink('https://h5.example/?a=1', 'ja')).toBe(
    'https://h5.example/?a=1&lng=en'
  )
  expect(localizedVerificationLink('https://h5.example/', 'zh')).toBe(
    'https://h5.example/?lng=zh'
  )
})

test('a passed result tells the person to go back to the computer and reports the token', async () => {
  render(<VerificationResult bytedToken='tok-1' resultCode='10000' />)

  expect(screen.getByText('Face verification passed')).toBeVisible()
  await waitFor(() =>
    expect(api.post).toHaveBeenCalledWith(
      '/api/asset-library/verification/callback',
      { byted_token: 'tok-1', result_code: '10000' }
    )
  )
})

test('a failed result shows its code and is reported so the verification ends', async () => {
  render(<VerificationResult bytedToken='tok-1' resultCode='10001' />)

  expect(screen.getByText('Face verification did not pass')).toBeVisible()
  expect(screen.getByText('Result code: 10001')).toBeVisible()
  await waitFor(() =>
    expect(api.post).toHaveBeenCalledWith(
      '/api/asset-library/verification/callback',
      { byted_token: 'tok-1', result_code: '10001' }
    )
  )
})

test('without a token nothing is reported', () => {
  render(<VerificationResult bytedToken='' resultCode='' />)

  expect(screen.getByText('Face verification did not pass')).toBeVisible()
  expect(api.post).not.toHaveBeenCalled()
})
