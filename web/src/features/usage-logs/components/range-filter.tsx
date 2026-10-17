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
import type { KeyboardEvent } from 'react'

import { LogsFilterField, LogsFilterInput } from './logs-filter-toolbar'

type RangeFilterProps = {
  label: string
  /** Input step; 1 (whole numbers) by default */
  step?: number
  minLabel: string
  maxLabel: string
  min?: string
  max?: string
  onMinChange: (value: string) => void
  onMaxChange: (value: string) => void
  onKeyDown: (event: KeyboardEvent) => void
}

/**
 * Bounds a numeric metric of the usage logs (tokens, seconds) with a minimum
 * and a maximum box; either may stay empty for an open bound.
 */
export function RangeFilter(props: RangeFilterProps) {
  return (
    <LogsFilterField wide>
      <div
        role='group'
        aria-label={props.label}
        className='flex min-w-0 items-center gap-1.5'
      >
        <LogsFilterInput
          type='number'
          inputMode='numeric'
          min={0}
          step={props.step ?? 1}
          placeholder={props.minLabel}
          aria-label={props.minLabel}
          value={props.min ?? ''}
          onChange={(event) => props.onMinChange(event.target.value)}
          onKeyDown={props.onKeyDown}
        />
        <span className='text-muted-foreground text-sm' aria-hidden='true'>
          –
        </span>
        <LogsFilterInput
          type='number'
          inputMode='numeric'
          min={0}
          step={props.step ?? 1}
          placeholder={props.maxLabel}
          aria-label={props.maxLabel}
          value={props.max ?? ''}
          onChange={(event) => props.onMaxChange(event.target.value)}
          onKeyDown={props.onKeyDown}
        />
      </div>
    </LogsFilterField>
  )
}
