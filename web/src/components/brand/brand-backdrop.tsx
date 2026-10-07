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
import { cn } from '@/lib/utils'

export type BrandBackdropVariant =
  | 'hero'
  | 'hero-center'
  | 'band'
  | 'pricing'
  | 'auth'
  | 'auth-panel'
  | 'panel'
  | 'dawn'

type BrandBackdropProps = {
  variant: BrandBackdropVariant
  /** 'static' = concentric hairline rings, 'sonar' = 3 expanding glass rims */
  rings?: 'static' | 'sonar'
  /** reeded glass strips at both inline edges (lg+); 'refract' adds real blur (static backdrops only) */
  flutes?: boolean | 'refract'
  className?: string
}

const DRIFTING_VARIANTS: ReadonlySet<BrandBackdropVariant> = new Set([
  'hero',
  'hero-center',
  'auth-panel',
])

const VEILED_VARIANTS: ReadonlySet<BrandBackdropVariant> = new Set([
  'hero',
  'hero-center',
])

/**
 * Decorative, non-interactive light layers. The parent must be a stacking
 * context (`relative isolate`, or positioned with a z-index).
 */
export function BrandBackdrop(props: BrandBackdropProps) {
  const variant = props.variant
  const fluteClassName =
    props.flutes === 'refract' ? 'brand-flutes--refract' : undefined

  return (
    <div
      aria-hidden='true'
      data-brand-backdrop={variant}
      className={cn('brand-backdrop', props.className)}
    >
      <div className='brand-mesh' />
      {DRIFTING_VARIANTS.has(variant) && <div className='brand-mesh-drift' />}
      {VEILED_VARIANTS.has(variant) && <div className='brand-veil' />}
      {variant !== 'dawn' && <div className='brand-grid' />}
      {props.rings === 'static' && <div className='brand-rings' />}
      {props.rings === 'sonar' && (
        <div className='brand-sonar'>
          <span />
          <span />
          <span />
        </div>
      )}
      {props.flutes && (
        <>
          <div
            className={cn('brand-flutes brand-flutes--start', fluteClassName)}
          />
          <div
            className={cn('brand-flutes brand-flutes--end', fluteClassName)}
          />
        </>
      )}
      {variant !== 'panel' && variant !== 'dawn' && (
        <div className='brand-noise' />
      )}
    </div>
  )
}
