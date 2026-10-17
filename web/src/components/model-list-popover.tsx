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
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { LoadingState } from '@/components/loading-state'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import {
  Popover,
  PopoverContent,
  PopoverTitle,
  PopoverTrigger,
} from '@/components/ui/popover'
import { cn } from '@/lib/utils'

type ModelListPopoverProps = {
  /** Trigger content, usually the model count. */
  children: ReactNode
  /** Accessible name of the trigger. */
  label: string
  title: string
  /** undefined while the list is not loaded yet. */
  models?: readonly string[]
  loading?: boolean
  /** Shown instead of the list when loading failed. */
  error?: string
  /** Lets callers load the list lazily when the popover opens. */
  onOpenChange?: (open: boolean) => void
  triggerClassName?: string
}

/**
 * A model count that lists every model on hover (or tap) with a button that
 * copies them all as one comma-separated line, the format model fields take.
 */
export function ModelListPopover(props: ModelListPopoverProps) {
  const { t } = useTranslation()
  const models = props.models ?? []

  let body: ReactNode
  if (props.loading) {
    body = <LoadingState inline size='sm' message={t('Loading...')} />
  } else if (props.error) {
    body = <p className='text-destructive text-xs break-words'>{props.error}</p>
  } else if (models.length === 0) {
    body = <p className='text-muted-foreground text-xs'>{t('No models')}</p>
  } else {
    body = (
      <ul
        aria-label={props.title}
        className='-mx-1 flex max-h-64 flex-wrap gap-1 overflow-y-auto px-1'
      >
        {models.map((model) => (
          <li key={model} className='max-w-full min-w-0'>
            <StatusBadge
              label={model}
              autoColor={model}
              size='sm'
              className='font-mono'
            />
          </li>
        ))}
      </ul>
    )
  }

  return (
    <Popover onOpenChange={(open) => props.onOpenChange?.(open)}>
      <PopoverTrigger
        openOnHover
        delay={150}
        closeDelay={150}
        render={
          <Button
            variant='ghost'
            size='sm'
            aria-label={props.label}
            className={cn(
              'h-auto max-w-full min-w-0 justify-start px-0 py-0.5 font-normal hover:bg-transparent aria-expanded:bg-transparent',
              props.triggerClassName
            )}
          />
        }
      >
        {props.children}
      </PopoverTrigger>
      <PopoverContent
        align='start'
        className='w-80 max-w-[calc(100vw-2rem)] gap-2 p-3'
      >
        <div className='flex min-w-0 items-center justify-between gap-2'>
          <PopoverTitle className='min-w-0 truncate'>
            {props.title}
          </PopoverTitle>
          {!props.loading && !props.error && models.length > 0 && (
            <CopyButton
              value={models.join(',')}
              variant='outline'
              size='sm'
              aria-label={t('Copy all models')}
              className='h-7 gap-1 px-2 text-xs'
              iconClassName='size-3.5'
            >
              {t('Copy all')}
            </CopyButton>
          )}
        </div>
        {body}
      </PopoverContent>
    </Popover>
  )
}
