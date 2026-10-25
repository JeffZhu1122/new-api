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
import { ImageIcon, Pencil, Trash2, Upload, X } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { CopyButton } from '@/components/copy-button'
import { EmptyState } from '@/components/empty-state'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { handleServerError } from '@/lib/handle-server-error'

import {
  type Asset,
  type AssetGroup,
  adminDeleteAsset,
  adminListGroupAssets,
  deleteAsset,
  listGroupAssets,
} from '../api'
import { RenameAssetDialog, UploadAssetDialog } from './asset-dialogs'

type GroupAssetsSheetProps = {
  group: AssetGroup | null
  // owner: the signed-in user's own group; admin: any user's group.
  mode: 'owner' | 'admin'
  canManage: boolean
  onOpenChange: (open: boolean) => void
}

export function GroupAssetsSheet(props: GroupAssetsSheetProps) {
  return (
    <Sheet open={props.group !== null} onOpenChange={props.onOpenChange}>
      {props.group && <GroupAssetsContent {...props} group={props.group} />}
    </Sheet>
  )
}

function GroupAssetsContent(
  props: GroupAssetsSheetProps & { group: AssetGroup }
) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [uploadOpen, setUploadOpen] = useState(false)
  const [renaming, setRenaming] = useState<Asset | null>(null)
  const [deleting, setDeleting] = useState<Asset | null>(null)
  const queryKey = ['asset-library', 'assets', props.mode, props.group.Id]
  const assetsQuery = useQuery({
    queryKey,
    queryFn: () =>
      props.mode === 'admin'
        ? adminListGroupAssets(props.group.Id)
        : listGroupAssets(props.group),
    // Ark processes uploads in the background; follow them until they settle.
    refetchInterval: (query) =>
      query.state.data?.some((asset) => asset.Status === 'Processing')
        ? 5000
        : false,
  })
  const deleteMutation = useMutation({
    mutationFn: (id: string) =>
      props.mode === 'admin' ? adminDeleteAsset(id) : deleteAsset(id),
    onSuccess: async () => {
      toast.success(t('Deleted successfully'))
      setDeleting(null)
      await queryClient.invalidateQueries({ queryKey: ['asset-library'] })
    },
    onError: (error) => handleServerError(error),
  })
  const assets = assetsQuery.data ?? []

  return (
    <SheetContent
      showCloseButton={false}
      className='w-full gap-0 overflow-hidden sm:max-w-3xl'
    >
      <SheetClose
        render={
          <Button
            variant='ghost'
            size='icon-sm'
            className='absolute top-3 right-3'
            aria-label={t('Close')}
          />
        }
      >
        <X aria-hidden='true' />
      </SheetClose>
      <SheetHeader className='shrink-0 gap-2 border-b p-4 pr-12 sm:p-6 sm:pr-14'>
        <SheetTitle className='min-w-0 text-lg font-semibold break-words'>
          {props.group.Name}
        </SheetTitle>
        <div className='flex min-w-0 items-center gap-1'>
          <span className='text-muted-foreground min-w-0 font-mono text-xs break-all'>
            {props.group.Id}
          </span>
          <CopyButton
            value={props.group.Id}
            className='size-7'
            iconClassName='size-3.5'
            aria-label={t('Copy group ID')}
          />
        </div>
        {props.mode === 'owner' && props.canManage && (
          <div>
            <Button size='sm' onClick={() => setUploadOpen(true)}>
              <Upload />
              {t('Upload asset')}
            </Button>
          </div>
        )}
      </SheetHeader>
      <div className='min-h-0 flex-1 space-y-3 overflow-y-auto p-4 sm:p-6'>
        {assetsQuery.isLoading && (
          <p className='text-muted-foreground text-sm'>{t('Loading...')}</p>
        )}
        {assetsQuery.isError && (
          <p className='text-destructive text-sm'>
            {assetsQuery.error instanceof Error
              ? assetsQuery.error.message
              : t('Failed to load')}
          </p>
        )}
        {assetsQuery.isSuccess && assets.length === 0 && (
          <EmptyState
            icon={ImageIcon}
            title={t('No assets yet')}
            description={t(
              'Upload images, videos or audio by URL. Each upload is checked by Volcengine before it can be used.'
            )}
          />
        )}
        {assets.map((asset) => (
          <AssetRow
            key={asset.Id}
            asset={asset}
            canRename={props.mode === 'owner' && props.canManage}
            canDelete={props.canManage}
            onRename={() => setRenaming(asset)}
            onDelete={() => setDeleting(asset)}
          />
        ))}
      </div>
      {props.mode === 'owner' && (
        <UploadAssetDialog
          open={uploadOpen}
          onOpenChange={setUploadOpen}
          groupId={props.group.Id}
        />
      )}
      <RenameAssetDialog
        asset={renaming}
        onOpenChange={(open) => !open && setRenaming(null)}
      />
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={t('Delete asset')}
        desc={t(
          'The asset is deleted on Volcengine and can no longer be used in video generation. This cannot be undone.'
        )}
        destructive
        isLoading={deleteMutation.isPending}
        handleConfirm={() => deleting && deleteMutation.mutate(deleting.Id)}
      />
    </SheetContent>
  )
}

const STATUS_VARIANTS = {
  Active: 'success',
  Processing: 'warning',
  Failed: 'danger',
} as const

function AssetRow(props: {
  asset: Asset
  canRename: boolean
  canDelete: boolean
  onRename: () => void
  onDelete: () => void
}) {
  const { t } = useTranslation()
  const asset = props.asset
  const statusLabels = {
    Active: t('Active'),
    Processing: t('Processing'),
    Failed: t('Failed'),
  }
  const typeLabels = {
    Image: t('Image'),
    Video: t('Video'),
    Audio: t('Audio'),
  }
  const reference = `asset://${asset.Id}`
  // Preview URLs are signed and expire; a preview that does not load falls
  // back to the placeholder until the URL is refreshed.
  const [failedUrl, setFailedUrl] = useState('')
  const preview = asset.URL === failedUrl ? '' : asset.URL
  const onPreviewError = () => setFailedUrl(asset.URL)

  return (
    <div
      className='flex min-w-0 flex-col gap-3 rounded-lg border p-3 sm:flex-row'
      data-testid='asset-row'
    >
      <div className='bg-muted flex h-28 w-full shrink-0 items-center justify-center overflow-hidden rounded-md sm:w-40'>
        {preview && asset.AssetType === 'Image' && (
          <img
            src={preview}
            alt={asset.Name || asset.Id}
            className='h-full w-full object-contain'
            loading='lazy'
            onError={onPreviewError}
          />
        )}
        {preview && asset.AssetType === 'Video' && (
          <video
            src={preview}
            className='h-full w-full object-contain'
            controls
            preload='metadata'
            onError={onPreviewError}
          />
        )}
        {preview && asset.AssetType === 'Audio' && (
          <audio
            src={preview}
            className='w-full'
            controls
            onError={onPreviewError}
          />
        )}
        {!preview && (
          <span className='text-muted-foreground text-xs'>
            {typeLabels[asset.AssetType]}
          </span>
        )}
      </div>
      <div className='min-w-0 flex-1 space-y-1.5'>
        <div className='flex flex-wrap items-center gap-2'>
          <span className='min-w-0 font-medium break-words'>
            {asset.Name || t('Unnamed asset')}
          </span>
          <StatusBadge
            label={statusLabels[asset.Status] ?? asset.Status}
            variant={STATUS_VARIANTS[asset.Status] ?? 'neutral'}
            copyable={false}
          />
          <span className='text-muted-foreground text-xs'>
            {typeLabels[asset.AssetType]}
          </span>
        </div>
        <div className='flex min-w-0 items-center gap-1'>
          <code className='bg-muted min-w-0 rounded px-1.5 py-0.5 font-mono text-xs break-all'>
            {reference}
          </code>
          <CopyButton
            value={reference}
            className='size-7'
            iconClassName='size-3.5'
            aria-label={t('Copy asset URI')}
          />
        </div>
        {asset.Error && (
          <p className='text-destructive text-xs break-words'>
            {asset.Error.Code}: {asset.Error.Message}
          </p>
        )}
        {asset.LastInferenceTime && (
          <p className='text-muted-foreground text-xs'>
            {t('Last used: {{time}}', { time: asset.LastInferenceTime })}
          </p>
        )}
      </div>
      <div className='flex shrink-0 items-start gap-1'>
        {props.canRename && (
          <Button
            variant='ghost'
            size='icon-sm'
            aria-label={t('Rename asset')}
            onClick={props.onRename}
          >
            <Pencil />
          </Button>
        )}
        {props.canDelete && (
          <Button
            variant='ghost'
            size='icon-sm'
            aria-label={t('Delete asset')}
            onClick={props.onDelete}
          >
            <Trash2 />
          </Button>
        )}
      </div>
    </div>
  )
}
