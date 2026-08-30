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
import { describe, expect, test } from 'vitest'

import {
  USER_FORM_DEFAULT_VALUES,
  transformFormDataToPayload,
  userFormSchema,
} from '../user-form'

const validateModelDiscount = (value: string) =>
  userFormSchema.safeParse({
    ...USER_FORM_DEFAULT_VALUES,
    username: 'alice',
    model_discount: value,
  })

describe('user model discount validation', () => {
  test('accepts empty input as "no discount"', () => {
    expect(validateModelDiscount('').success).toBe(true)
    expect(validateModelDiscount('  ').success).toBe(true)
  })

  test('accepts a JSON map with values in (0, 10] including the "*" fallback', () => {
    expect(
      validateModelDiscount('{"gpt-4o": 0.8, "*": 0.9, "expensive": 10}')
        .success
    ).toBe(true)
  })

  test('rejects zero, negative, and above-10 discount values', () => {
    for (const value of [
      '{"gpt-4o": 0}',
      '{"gpt-4o": -1}',
      '{"gpt-4o": 80}',
    ]) {
      expect(validateModelDiscount(value).success).toBe(false)
    }
  })

  test('rejects non-object JSON and non-numeric discount values', () => {
    for (const value of ['[0.5]', '"0.5"', '{"gpt-4o": "cheap"}', '{bad']) {
      expect(validateModelDiscount(value).success).toBe(false)
    }
  })
})

describe('user model discount payload transformation', () => {
  test('update payload carries the parsed discount map', () => {
    const payload = transformFormDataToPayload(
      {
        ...USER_FORM_DEFAULT_VALUES,
        username: 'alice',
        model_discount: '{"gpt-4o": 0.8, "*": 0.9}',
      },
      42
    )
    expect(payload.model_discount).toEqual({ 'gpt-4o': 0.8, '*': 0.9 })
  })

  test('update payload sends {} for empty input so the backend clears the override', () => {
    const payload = transformFormDataToPayload(
      { ...USER_FORM_DEFAULT_VALUES, username: 'alice', model_discount: '' },
      42
    )
    expect(payload.model_discount).toEqual({})
  })

  test('create payload omits the discount field entirely', () => {
    const payload = transformFormDataToPayload({
      ...USER_FORM_DEFAULT_VALUES,
      username: 'alice',
      model_discount: '{"gpt-4o": 0.8}',
    })
    expect(payload.model_discount).toBeUndefined()
  })
})
