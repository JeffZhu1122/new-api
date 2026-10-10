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
import { Boxes, Search, X } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import {
  sideDrawerContentClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { GroupBadge } from '@/components/group-badge'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from '@/components/ui/input-group'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { translateServerText } from '@/lib/server-error-message'

import { useUserGroupModels } from '../hooks/use-user-group-models'
import type { UserGroupModels } from '../types'

const ALL_GROUPS = '__all__'

type GroupModelsSheetProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  // Group selected when the sheet opens; every group when unset or unknown
  initialGroup?: string
}

/**
 * Lists every group the user may select and the models each one can call.
 */
export function GroupModelsSheet(props: GroupModelsSheetProps) {
  const { t } = useTranslation()
  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent
        className={sideDrawerContentClassName('max-w-none sm:!max-w-[640px]')}
      >
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle>{t('Groups & Models')}</SheetTitle>
          <SheetDescription>
            {t(
              'The models each group you can use is able to call. A listed model can still fail while its channels are outside their schedule, at their limits or temporarily unavailable.'
            )}
          </SheetDescription>
        </SheetHeader>
        <GroupModelsBody initialGroup={props.initialGroup} />
      </SheetContent>
    </Sheet>
  )
}

/**
 * The line under a group picker: how many models the group can call, opening
 * the sheet on that group.
 */
export function GroupModelsLink(props: { group?: string }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const query = useUserGroupModels()
  const entry = query.data?.find((item) => item.group === props.group)

  return (
    <>
      <Button
        type='button'
        variant='link'
        size='sm'
        className='h-auto justify-self-start p-0 text-xs'
        onClick={() => setOpen(true)}
      >
        {entry
          ? t('Models available in this group: {{count}}', {
              count: entry.models.length,
            })
          : t('View models by group')}
      </Button>
      <GroupModelsSheet
        open={open}
        onOpenChange={setOpen}
        initialGroup={props.group}
      />
    </>
  )
}

// Mounted only while the sheet is open, so search and selection start fresh.
function GroupModelsBody(props: { initialGroup?: string }) {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const [selected, setSelected] = useState(props.initialGroup || ALL_GROUPS)
  const query = useUserGroupModels()

  if (query.isLoading) {
    return <LoadingState message={t('Loading...')} />
  }
  if (query.isError) {
    return (
      <ErrorState
        description={t('Failed to load groups and models')}
        onRetry={() => void query.refetch()}
      />
    )
  }

  const groups = query.data ?? []
  if (groups.length === 0) {
    return (
      <EmptyState
        icon={Boxes}
        title={t('No groups available')}
        description={t('Your account cannot use any group yet.')}
      />
    )
  }

  const activeGroup = groups.some((group) => group.group === selected)
    ? selected
    : ALL_GROUPS
  const keyword = search.trim().toLowerCase()
  const visibleGroups = groups
    .filter(
      (group) => activeGroup === ALL_GROUPS || group.group === activeGroup
    )
    .map((group) => ({
      ...group,
      models: keyword
        ? group.models.filter((model) => model.toLowerCase().includes(keyword))
        : group.models,
    }))
    .filter((group) => !keyword || group.models.length > 0)

  return (
    <div className={sideDrawerFormClassName('gap-5')}>
      <div className='flex flex-col gap-3'>
        <InputGroup>
          <InputGroupAddon>
            <Search aria-hidden='true' />
          </InputGroupAddon>
          <InputGroupInput
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            aria-label={t('Search models')}
            placeholder={t('Search models')}
          />
          {search && (
            <InputGroupAddon align='inline-end'>
              <InputGroupButton
                size='icon-xs'
                aria-label={t('Clear search')}
                onClick={() => setSearch('')}
              >
                <X aria-hidden='true' />
              </InputGroupButton>
            </InputGroupAddon>
          )}
        </InputGroup>
        <ToggleGroup
          value={[activeGroup]}
          onValueChange={(values) => {
            if (values.length > 0) setSelected(values[0])
          }}
          variant='outline'
          size='sm'
          spacing={1}
          className='flex-wrap'
          aria-label={t('Filter by group')}
        >
          <ToggleGroupItem value={ALL_GROUPS} className='h-8'>
            {t('All groups')}
          </ToggleGroupItem>
          {groups.map((group) => (
            <ToggleGroupItem
              key={group.group}
              value={group.group}
              className='h-8'
            >
              {group.group === 'auto' ? t('Auto') : group.group}
              <span className='text-muted-foreground tabular-nums'>
                {group.models.length}
              </span>
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </div>

      {visibleGroups.length === 0 ? (
        <EmptyState
          icon={Search}
          title={t('No matching models')}
          description={t('Try a different search term.')}
        />
      ) : (
        visibleGroups.map((group) => (
          <GroupModelsSection key={group.group} group={group} />
        ))
      )}
    </div>
  )
}

function GroupModelsSection(props: { group: UserGroupModels }) {
  const { t } = useTranslation()
  const group = props.group
  const headingId = `group-models-${group.group}`
  const ratio = typeof group.ratio === 'number' ? group.ratio : undefined

  return (
    <section
      aria-labelledby={headingId}
      className='border-border/60 flex flex-col gap-3 border-b pb-5 last:border-b-0 last:pb-0'
    >
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div className='flex min-w-0 items-center gap-2'>
          <h3 id={headingId} className='min-w-0'>
            <GroupBadge group={group.group} ratio={ratio} copyable={false} />
          </h3>
          <span className='text-muted-foreground text-xs tabular-nums'>
            {t('Models: {{count}}', { count: group.models.length })}
          </span>
        </div>
        {group.models.length > 0 && (
          <CopyButton
            value={group.models.join(',')}
            variant='outline'
            size='sm'
            aria-label={t('Copy all model names in {{group}}', {
              group: group.group,
            })}
          >
            {t('Copy all')}
          </CopyButton>
        )}
      </div>
      {group.desc && (
        <p className='text-muted-foreground text-xs'>
          {translateServerText(t, group.desc)}
        </p>
      )}
      {group.group === 'auto' && (group.auto_groups?.length ?? 0) > 0 && (
        <p className='text-muted-foreground text-xs'>
          {t('Tries these groups in order: {{groups}}', {
            groups: group.auto_groups?.join(' → '),
          })}
        </p>
      )}
      {group.models.length === 0 ? (
        <p className='text-muted-foreground text-sm'>
          {t('No models available in this group')}
        </p>
      ) : (
        <ul className='flex flex-wrap gap-1.5'>
          {group.models.map((model) => (
            <li key={model} className='max-w-full min-w-0'>
              <CopyButton
                value={model}
                variant='outline'
                size='sm'
                iconClassName='size-3'
                className='h-7 max-w-full font-mono text-xs font-normal'
                aria-label={`${t('Copy model name')}: ${model}`}
              >
                <span className='min-w-0 truncate'>{model}</span>
              </CopyButton>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
