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

type BrandPulseProps = { tone?: 'brand' | 'success'; className?: string }

/** 6px live dot with a ping ring. Always paired with visible text. */
export function BrandPulse(props: BrandPulseProps) {
  return (
    <span
      aria-hidden='true'
      data-tone={props.tone ?? 'brand'}
      className={cn('brand-pulse', props.className)}
    />
  )
}
