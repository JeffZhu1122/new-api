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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { handleServerError } from '@/lib/handle-server-error'

import {
  type Asset,
  type AssetGroup,
  type AssetGroupType,
  type AssetType,
  createAsset,
  createAssetGroup,
  updateAsset,
  updateAssetGroup,
} from '../api'

const NAME_MAX = 64
const DESCRIPTION_MAX = 300

function useInvalidateLibrary() {
  const queryClient = useQueryClient()
  return () => queryClient.invalidateQueries({ queryKey: ['asset-library'] })
}

// Creates or edits an asset group. A new group needs the compliance notice
// accepted when the administrator set one.
export function AssetGroupDialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  groupType: AssetGroupType
  group: AssetGroup | null
  complianceNotice: string
}) {
  const { t } = useTranslation()
  const invalidate = useInvalidateLibrary()
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [agreed, setAgreed] = useState(false)
  const isEdit = props.group !== null
  const needsAgreement = !isEdit && props.complianceNotice.trim() !== ''

  useEffect(() => {
    if (!props.open) return
    setName(props.group?.Name ?? '')
    setDescription(props.group?.Description ?? '')
    setAgreed(false)
  }, [props.open, props.group])

  const mutation = useMutation({
    mutationFn: () =>
      props.group
        ? updateAssetGroup({
            Id: props.group.Id,
            Name: name.trim(),
            Description: description,
          })
        : createAssetGroup({
            Name: name.trim(),
            Description: description,
            GroupType: props.groupType,
          }),
    onSuccess: async () => {
      toast.success(t('Saved successfully'))
      await invalidate()
      props.onOpenChange(false)
    },
    onError: (error) => handleServerError(error),
  })
  const nameValid = name.trim() !== '' && name.length <= NAME_MAX
  const canSubmit =
    nameValid &&
    description.length <= DESCRIPTION_MAX &&
    (!needsAgreement || agreed)

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={isEdit ? t('Edit asset group') : t('New asset group')}
      description={
        props.groupType === 'LivenessFace'
          ? t('A real-person group holds the assets of one real person.')
          : t('A virtual avatar group holds the assets of virtual characters.')
      }
      contentClassName='sm:max-w-[520px]'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button
            disabled={!canSubmit || mutation.isPending}
            onClick={() => mutation.mutate()}
          >
            {isEdit ? t('Save') : t('Create')}
          </Button>
        </>
      }
    >
      <div className='space-y-2'>
        <Label htmlFor='asset-group-name'>{t('Name')}</Label>
        <Input
          id='asset-group-name'
          value={name}
          maxLength={NAME_MAX}
          onChange={(event) => setName(event.target.value)}
        />
      </div>
      <div className='space-y-2'>
        <Label htmlFor='asset-group-description'>{t('Description')}</Label>
        <Textarea
          id='asset-group-description'
          rows={3}
          value={description}
          maxLength={DESCRIPTION_MAX}
          onChange={(event) => setDescription(event.target.value)}
        />
      </div>
      {needsAgreement && (
        <div className='bg-muted/50 space-y-3 rounded-md border p-3'>
          <p className='text-sm whitespace-pre-wrap'>
            {props.complianceNotice}
          </p>
          <div className='flex items-center gap-2'>
            <Checkbox
              id='asset-group-agree'
              checked={agreed}
              onCheckedChange={(checked) => setAgreed(checked === true)}
            />
            <Label htmlFor='asset-group-agree' className='font-normal'>
              {t('I have read and agree to the notice above')}
            </Label>
          </div>
        </div>
      )}
    </Dialog>
  )
}

export function UploadAssetDialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  groupId: string
}) {
  const { t } = useTranslation()
  const invalidate = useInvalidateLibrary()
  const [url, setUrl] = useState('')
  const [assetType, setAssetType] = useState<AssetType>('Image')
  const [name, setName] = useState('')

  useEffect(() => {
    if (!props.open) return
    setUrl('')
    setAssetType('Image')
    setName('')
  }, [props.open])

  const mutation = useMutation({
    mutationFn: () =>
      createAsset({
        GroupId: props.groupId,
        URL: url.trim(),
        AssetType: assetType,
        Name: name.trim(),
      }),
    onSuccess: async () => {
      toast.success(
        t(
          'Uploaded. Volcengine is processing the asset; it can be used once Active.'
        )
      )
      await invalidate()
      props.onOpenChange(false)
    },
    onError: (error) => handleServerError(error),
  })
  const urlValid = /^https?:\/\/\S+$/i.test(url.trim())
  const typeItems = [
    { value: 'Image', label: t('Image') },
    { value: 'Video', label: t('Video') },
    { value: 'Audio', label: t('Audio') },
  ]
  const limits: Record<AssetType, string> = {
    Image: t(
      'jpeg, png, webp, tiff, gif or heic; width and height 300-6000 px; aspect ratio 0.4-2.5; under 30 MB.'
    ),
    Video: t('mp4 or mov; 2-30 seconds; 480p to 4K; 24-60 fps; up to 200 MB.'),
    Audio: t('wav or mp3; 2-30 seconds; up to 15 MB.'),
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Upload asset')}
      description={t(
        'Volcengine downloads the file from a public URL; base64 is not supported.'
      )}
      contentClassName='sm:max-w-[520px]'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button
            disabled={!urlValid || name.length > NAME_MAX || mutation.isPending}
            onClick={() => mutation.mutate()}
          >
            {t('Upload')}
          </Button>
        </>
      }
    >
      <div className='space-y-2'>
        <Label htmlFor='asset-type'>{t('Asset type')}</Label>
        <Select
          items={typeItems}
          value={assetType}
          onValueChange={(value) => value && setAssetType(value as AssetType)}
        >
          <SelectTrigger id='asset-type' className='w-full'>
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
        <p className='text-muted-foreground text-xs'>{limits[assetType]}</p>
      </div>
      <div className='space-y-2'>
        <Label htmlFor='asset-url'>{t('File URL')}</Label>
        <Input
          id='asset-url'
          type='url'
          placeholder='https://'
          value={url}
          onChange={(event) => setUrl(event.target.value)}
        />
      </div>
      <div className='space-y-2'>
        <Label htmlFor='asset-name'>{t('Name (optional)')}</Label>
        <Input
          id='asset-name'
          value={name}
          maxLength={NAME_MAX}
          onChange={(event) => setName(event.target.value)}
        />
        <p className='text-muted-foreground text-xs'>
          {t(
            'Only for finding the asset later; prompts refer to assets as image 1, video 1 and so on.'
          )}
        </p>
      </div>
    </Dialog>
  )
}

export function RenameAssetDialog(props: {
  asset: Asset | null
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const invalidate = useInvalidateLibrary()
  const [name, setName] = useState('')

  useEffect(() => {
    if (props.asset) setName(props.asset.Name)
  }, [props.asset])

  const mutation = useMutation({
    mutationFn: () =>
      updateAsset({ Id: props.asset?.Id ?? '', Name: name.trim() }),
    onSuccess: async () => {
      toast.success(t('Saved successfully'))
      await invalidate()
      props.onOpenChange(false)
    },
    onError: (error) => handleServerError(error),
  })

  return (
    <Dialog
      open={props.asset !== null}
      onOpenChange={props.onOpenChange}
      title={t('Rename asset')}
      contentClassName='sm:max-w-[420px]'
      bodyClassName='space-y-2'
      footer={
        <>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button
            disabled={name.length > NAME_MAX || mutation.isPending}
            onClick={() => mutation.mutate()}
          >
            {t('Save')}
          </Button>
        </>
      }
    >
      <Label htmlFor='asset-rename'>{t('Name')}</Label>
      <Input
        id='asset-rename'
        value={name}
        maxLength={NAME_MAX}
        onChange={(event) => setName(event.target.value)}
      />
    </Dialog>
  )
}
