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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { FolderOpen, Plus } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { CopyButton } from '@/components/copy-button'
import { StaticDataTable } from '@/components/data-table/static/static-data-table'
import { StaticRowActions } from '@/components/data-table/static/static-row-actions'
import { Button } from '@/components/ui/button'
import { handleServerError } from '@/lib/handle-server-error'

import {
  type AssetGroup,
  type AssetGroupType,
  type AssetLibraryConfig,
  deleteAssetGroup,
  listAssetGroups,
} from '../api'
import { AssetGroupDialog } from './asset-dialogs'
import { GroupAssetsSheet } from './group-assets-sheet'

// The signed-in user's own groups of one type.
export function GroupsPanel(props: {
  groupType: AssetGroupType
  config: AssetLibraryConfig
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editing, setEditing] = useState<AssetGroup | null>(null)
  const [deleting, setDeleting] = useState<AssetGroup | null>(null)
  const [viewing, setViewing] = useState<AssetGroup | null>(null)
  const groupsQuery = useQuery({
    queryKey: ['asset-library', 'groups', props.groupType],
    queryFn: () => listAssetGroups(props.groupType),
  })
  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteAssetGroup(id),
    onSuccess: async () => {
      toast.success(t('Deleted successfully'))
      setDeleting(null)
      await queryClient.invalidateQueries({ queryKey: ['asset-library'] })
    },
    onError: (error) => handleServerError(error),
  })
  const typeAvailable = props.config.group_types.includes(props.groupType)
  const groupLimitReached =
    props.config.max_groups_per_user > 0 &&
    props.config.group_count >= props.config.max_groups_per_user
  const openDialog = (group: AssetGroup | null) => {
    setEditing(group)
    setDialogOpen(true)
  }
  let hint = t(
    'Group virtual characters however you like, for example one group per character.'
  )
  if (props.groupType === 'LivenessFace') {
    hint = t(
      'One group per real person; add that person’s images, videos and audio to it.'
    )
  }
  if (groupLimitReached) {
    hint = t('You have reached your asset group limit.')
  }
  if (!typeAvailable) {
    hint = t('This type of asset group is not available to you.')
  }
  let emptyContent = t('No asset groups yet')
  if (groupsQuery.isLoading) {
    emptyContent = t('Loading...')
  }
  if (groupsQuery.isError) {
    emptyContent =
      groupsQuery.error instanceof Error
        ? groupsQuery.error.message
        : t('Failed to load')
  }

  return (
    <div className='flex flex-col gap-3'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <p className='text-muted-foreground text-sm'>{hint}</p>
        <Button
          size='sm'
          disabled={!typeAvailable || groupLimitReached}
          onClick={() => openDialog(null)}
        >
          <Plus />
          {t('New asset group')}
        </Button>
      </div>
      <StaticDataTable
        data={groupsQuery.data ?? []}
        getRowKey={(group) => group.Id}
        emptyContent={emptyContent}
        columns={[
          {
            id: 'name',
            header: t('Name'),
            cell: (group) => (
              <button
                type='button'
                className='min-w-0 text-left'
                onClick={() => setViewing(group)}
              >
                <div className='font-medium hover:underline'>{group.Name}</div>
                {group.Description && (
                  <div className='text-muted-foreground line-clamp-1 text-xs'>
                    {group.Description}
                  </div>
                )}
              </button>
            ),
          },
          {
            id: 'id',
            header: t('Group ID'),
            cell: (group) => (
              <div className='flex items-center gap-1'>
                <span className='font-mono text-xs'>{group.Id}</span>
                <CopyButton
                  value={group.Id}
                  className='size-7'
                  iconClassName='size-3.5'
                  aria-label={t('Copy group ID')}
                />
              </div>
            ),
          },
          {
            id: 'created',
            header: t('Created'),
            cell: (group) => (
              <span className='text-muted-foreground text-xs'>
                {group.CreateTime}
              </span>
            ),
          },
          {
            id: 'assets',
            header: <span className='sr-only'>{t('Assets')}</span>,
            cell: (group) => (
              <Button
                variant='outline'
                size='sm'
                onClick={() => setViewing(group)}
              >
                <FolderOpen />
                {t('Assets')}
              </Button>
            ),
          },
          {
            id: 'actions',
            header: <span className='sr-only'>{t('Actions')}</span>,
            cell: (group) => (
              <StaticRowActions
                editLabel={t('Edit')}
                deleteLabel={t('Delete')}
                menuLabel={t('Open menu')}
                onEdit={() => openDialog(group)}
                onDelete={() => setDeleting(group)}
              />
            ),
          },
        ]}
      />
      <AssetGroupDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        groupType={props.groupType}
        group={editing}
        complianceNotice={props.config.compliance_notice}
      />
      <GroupAssetsSheet
        group={viewing}
        mode='owner'
        canManage
        onOpenChange={(open) => !open && setViewing(null)}
      />
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={t('Delete asset group')}
        desc={t(
          'The group and every asset in it are deleted on Volcengine. This cannot be undone.'
        )}
        destructive
        isLoading={deleteMutation.isPending}
        handleConfirm={() => deleting && deleteMutation.mutate(deleting.Id)}
      />
    </div>
  )
}
