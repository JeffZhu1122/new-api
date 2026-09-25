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
import { Reorder } from 'motion/react'
import { useMemo, type ComponentProps } from 'react'
import { useTranslation } from 'react-i18next'

import { AutoGroupOrderItem } from '@/components/auto-group-order-item'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from '@/components/ui/empty'
import { cn } from '@/lib/utils'

import {
  ApiKeyGroupCombobox,
  type ApiKeyGroupOption,
} from './api-key-group-combobox'

type FallbackGroupOrderEditorProps = Omit<ComponentProps<'div'>, 'onChange'> & {
  value: string[]
  primaryGroup: string
  options: ApiKeyGroupOption[]
  maxCount: number
  onChange: (groups: string[]) => void
  'data-slot'?: string
  'data-form-root'?: string
}

/**
 * Ordered fallback groups for an ordinary (non-Auto) API key. The primary group
 * is tried first, then each fallback in the configured order.
 */
export function FallbackGroupOrderEditor(props: FallbackGroupOrderEditorProps) {
  const { t } = useTranslation()
  const maxCount =
    Number.isInteger(props.maxCount) && props.maxCount > 0 ? props.maxCount : 0
  const atLimit = props.value.length >= maxCount
  const candidates = useMemo(
    () =>
      props.options.filter(
        (option) =>
          option.value !== 'auto' &&
          option.value !== props.primaryGroup &&
          !props.value.includes(option.value)
      ),
    [props.options, props.primaryGroup, props.value]
  )

  const handleAdd = (group: string) => {
    if (
      atLimit ||
      group === props.primaryGroup ||
      props.value.includes(group)
    ) {
      return
    }
    props.onChange([...props.value, group])
  }

  const handleRemove = (group: string) => {
    props.onChange(props.value.filter((item) => item !== group))
  }

  const handleMove = (index: number, direction: 'up' | 'down') => {
    const targetIndex = direction === 'up' ? index - 1 : index + 1
    if (targetIndex < 0 || targetIndex >= props.value.length) return
    const next = [...props.value]
    ;[next[index], next[targetIndex]] = [next[targetIndex], next[index]]
    props.onChange(next)
  }

  return (
    <div
      id={props.id}
      data-slot={props['data-slot']}
      data-form-root={props['data-form-root']}
      role='group'
      tabIndex={-1}
      aria-label={props['aria-label'] || t('Fallback groups')}
      aria-describedby={props['aria-describedby']}
      aria-invalid={props['aria-invalid']}
      className={cn('flex flex-col gap-3', props.className)}
    >
      <p className='text-muted-foreground text-xs' aria-live='polite'>
        {t('{{count}} / {{max}} fallback groups selected', {
          count: props.value.length,
          max: maxCount,
        })}
      </p>

      <ApiKeyGroupCombobox
        options={candidates}
        value={undefined}
        onValueChange={handleAdd}
        placeholder={
          atLimit
            ? t('Maximum {{max}} groups selected', { max: maxCount })
            : t('Add fallback group')
        }
        disabled={atLimit || candidates.length === 0}
      />

      {props.value.length === 0 && (
        <Empty className='min-h-20 border'>
          <EmptyHeader>
            <EmptyTitle>{t('No fallback groups')}</EmptyTitle>
            <EmptyDescription>
              {t(
                'This key only uses its primary group. Add fallback groups to try them in order.'
              )}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}

      {props.value.length > 0 && (
        <Reorder.Group
          axis='y'
          values={props.value}
          onReorder={props.onChange}
          className='flex flex-col gap-2'
        >
          {props.value.map((group, index) => (
            <AutoGroupOrderItem
              key={group}
              group={group}
              index={index}
              count={props.value.length}
              onMove={handleMove}
              onRemove={handleRemove}
            />
          ))}
        </Reorder.Group>
      )}
    </div>
  )
}
