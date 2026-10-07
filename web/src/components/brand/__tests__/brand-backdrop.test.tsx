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
import { render } from '@testing-library/react'
import { describe, expect, test } from 'vitest'

import { BrandBackdrop, type BrandBackdropVariant } from '../brand-backdrop'

// brand.css targets these class names and the data-brand-backdrop attribute;
// they are the styling contract between the component and the stylesheet.
const VARIANTS: BrandBackdropVariant[] = [
  'hero',
  'hero-center',
  'band',
  'pricing',
  'auth',
  'auth-panel',
  'panel',
  'dawn',
]

const FOCUSABLE_SELECTOR =
  'a[href], button, input, select, textarea, [tabindex], [contenteditable]'

describe('BrandBackdrop', () => {
  test.each(VARIANTS)(
    'variant %s renders a hidden decorative root with no focusable descendants',
    (variant) => {
      const { container } = render(
        <BrandBackdrop variant={variant} rings='sonar' flutes='refract' />
      )

      const root = container.querySelector('[data-brand-backdrop]')
      expect(root).toHaveAttribute('aria-hidden', 'true')
      expect(root).toHaveAttribute('data-brand-backdrop', variant)
      expect(root?.querySelectorAll(FOCUSABLE_SELECTOR)).toHaveLength(0)
    }
  )

  test('sonar rings render three expanding ring spans and no static rings', () => {
    const { container } = render(<BrandBackdrop variant='hero' rings='sonar' />)

    expect(container.querySelectorAll('.brand-sonar > span')).toHaveLength(3)
    expect(container.querySelector('.brand-rings')).not.toBeInTheDocument()
  })

  test('static rings render a single ring layer and no sonar', () => {
    const { container } = render(
      <BrandBackdrop variant='band' rings='static' />
    )

    expect(container.querySelectorAll('.brand-rings')).toHaveLength(1)
    expect(container.querySelector('.brand-sonar')).not.toBeInTheDocument()
  })

  test('omitting rings renders neither ring layer', () => {
    const { container } = render(<BrandBackdrop variant='pricing' />)

    expect(container.querySelector('.brand-rings')).not.toBeInTheDocument()
    expect(container.querySelector('.brand-sonar')).not.toBeInTheDocument()
  })

  test('flutes render a start and an end strip without refraction', () => {
    const { container } = render(<BrandBackdrop variant='band' flutes />)

    const strips = container.querySelectorAll('.brand-flutes')
    expect(strips).toHaveLength(2)
    expect(strips[0]).toHaveClass('brand-flutes--start')
    expect(strips[1]).toHaveClass('brand-flutes--end')
    expect(
      container.querySelector('.brand-flutes--refract')
    ).not.toBeInTheDocument()
  })

  test('refracting flutes mark both strips for real blur', () => {
    const { container } = render(
      <BrandBackdrop variant='band' flutes='refract' />
    )

    const strips = container.querySelectorAll('.brand-flutes')
    expect(strips).toHaveLength(2)
    for (const strip of strips) {
      expect(strip).toHaveClass('brand-flutes--refract')
    }
  })

  test('omitting flutes renders no edge strips', () => {
    const { container } = render(<BrandBackdrop variant='band' />)

    expect(container.querySelector('.brand-flutes')).not.toBeInTheDocument()
  })
})
