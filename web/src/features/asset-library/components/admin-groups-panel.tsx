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
import { FolderOpen, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { StaticDataTable } from '@/components/data-table/static/static-data-table'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { handleServerError } from '@/lib/handle-server-error'

import {
  type AdminAssetGroup,
  type AssetGroupType,
  adminDeleteAssetGroup,
  adminListAssetGroups,
} from '../api'
import { GroupAssetsSheet } from './group-assets-sheet'

const PAGE_SIZE = 20

// Every user's groups, for administrators with the asset library read
// permission; deleting needs the write permission.
export function AdminGroupsPanel(props: { canManage: boolean }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [userId, setUserId] = useState('')
  const [keyword, setKeyword] = useState('')
  const [groupType, setGroupType] = useState<'all' | AssetGroupType>('all')
  const [page, setPage] = useState(1)
  const [viewing, setViewing] = useState<AdminAssetGroup | null>(null)
  const [deleting, setDeleting] = useState<AdminAssetGroup | null>(null)
  const filters = {
    p: page,
    page_size: PAGE_SIZE,
    user_id: Number(userId) > 0 ? Number(userId) : undefined,
    group_type: groupType === 'all' ? undefined : groupType,
    keyword: keyword.trim() || undefined,
  }
  const groupsQuery = useQuery({
    queryKey: ['asset-library', 'admin-groups', filters],
    queryFn: () => adminListAssetGroups(filters),
  })
  const deleteMutation = useMutation({
    mutationFn: (id: string) => adminDeleteAssetGroup(id),
    onSuccess: async () => {
      toast.success(t('Deleted successfully'))
      setDeleting(null)
      await queryClient.invalidateQueries({ queryKey: ['asset-library'] })
    },
    onError: (error) => handleServerError(error),
  })
  const total = groupsQuery.data?.total ?? 0
  const pageCount = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const typeItems = [
    { value: 'all', label: t('All types') },
    { value: 'AIGC', label: t('Virtual avatars') },
    { value: 'LivenessFace', label: t('Real people') },
  ]
  const typeLabels: Record<AssetGroupType, string> = {
    AIGC: t('Virtual avatars'),
    LivenessFace: t('Real people'),
  }

  return (
    <div className='flex flex-col gap-3'>
      <div className='flex flex-wrap items-center gap-2'>
        <Input
          className='w-32'
          inputMode='numeric'
          placeholder={t('User ID')}
          aria-label={t('User ID')}
          value={userId}
          onChange={(event) => {
            setUserId(event.target.value.replaceAll(/\D/g, ''))
            setPage(1)
          }}
        />
        <Input
          className='w-56'
          placeholder={t('Group name or ID')}
          aria-label={t('Group name or ID')}
          value={keyword}
          onChange={(event) => {
            setKeyword(event.target.value)
            setPage(1)
          }}
        />
        <Select
          items={typeItems}
          value={groupType}
          onValueChange={(value) => {
            if (!value) return
            setGroupType(value as 'all' | AssetGroupType)
            setPage(1)
          }}
        >
          <SelectTrigger className='w-40' aria-label={t('Group type')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent alignItemWithTrigger={false}>
            <SelectGroup>
              {typeItems.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
      </div>
      <StaticDataTable
        data={groupsQuery.data?.items ?? []}
        getRowKey={(group) => group.Id}
        emptyContent={
          groupsQuery.isLoading ? t('Loading...') : t('No asset groups yet')
        }
        columns={[
          {
            id: 'name',
            header: t('Name'),
            cell: (group) => (
              <div className='min-w-0'>
                <div className='font-medium'>{group.Name}</div>
                <div className='text-muted-foreground font-mono text-xs'>
                  {group.Id}
                </div>
              </div>
            ),
          },
          {
            id: 'type',
            header: t('Group type'),
            cell: (group) => typeLabels[group.GroupType] ?? group.GroupType,
          },
          {
            id: 'user',
            header: t('User'),
            cell: (group) => `${group.Username || '-'} (#${group.UserId})`,
          },
          {
            id: 'account',
            header: t('Volcengine account'),
            cell: (group) => group.AccountName || '-',
          },
          {
            id: 'count',
            header: t('Assets'),
            cell: (group) => group.AssetCount,
          },
          {
            id: 'actions',
            header: <span className='sr-only'>{t('Actions')}</span>,
            cell: (group) => (
              <div className='flex justify-end gap-1'>
                <Button
                  variant='ghost'
                  size='icon-sm'
                  aria-label={t('View assets')}
                  onClick={() => setViewing(group)}
                >
                  <FolderOpen />
                </Button>
                {props.canManage && (
                  <Button
                    variant='ghost'
                    size='icon-sm'
                    aria-label={t('Delete asset group')}
                    onClick={() => setDeleting(group)}
                  >
                    <Trash2 />
                  </Button>
                )}
              </div>
            ),
          },
        ]}
      />
      <div className='flex items-center justify-end gap-2 text-sm'>
        <span className='text-muted-foreground'>
          {t('Page {{page}} of {{count}}, {{total}} groups', {
            page,
            count: pageCount,
            total,
          })}
        </span>
        <Button
          variant='outline'
          size='sm'
          disabled={page <= 1}
          onClick={() => setPage(page - 1)}
        >
          {t('Previous')}
        </Button>
        <Button
          variant='outline'
          size='sm'
          disabled={page >= pageCount}
          onClick={() => setPage(page + 1)}
        >
          {t('Next')}
        </Button>
      </div>
      <GroupAssetsSheet
        group={viewing}
        mode='admin'
        canManage={props.canManage}
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
