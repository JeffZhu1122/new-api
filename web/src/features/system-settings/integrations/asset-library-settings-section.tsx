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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { StaticDataTable } from '@/components/data-table/static/static-data-table'
import { StaticRowActions } from '@/components/data-table/static/static-row-actions'
import { MultiSelect } from '@/components/multi-select'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { handleServerError } from '@/lib/handle-server-error'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'
import { safeNumberFieldProps } from '../utils/numeric-field'
import { AssetLibraryAccountDialog } from './asset-library-account-dialog'
import {
  type AssetLibraryAccount,
  assetLibraryAccountsQueryKey,
  deleteAssetLibraryAccount,
  listAssetLibraryAccounts,
  listUserGroups,
} from './asset-library-api'

const assetLibrarySchema = z.object({
  enabled: z.boolean(),
  block_asset_refs_when_disabled: z.boolean(),
  max_groups_per_user: z.number().int().min(0),
  max_assets_per_user: z.number().int().min(0),
  allowed_user_groups: z.array(z.string()),
  compliance_notice: z.string().max(5000),
})

export type AssetLibrarySettingsValues = z.infer<typeof assetLibrarySchema>

type AssetLibrarySettingsSectionProps = {
  defaultValues: AssetLibrarySettingsValues
}

export function AssetLibrarySettingsSection(
  props: AssetLibrarySettingsSectionProps
) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const form = useForm<AssetLibrarySettingsValues>({
    resolver: zodResolver(assetLibrarySchema),
    defaultValues: props.defaultValues,
  })
  useResetForm(form, props.defaultValues)

  const onSubmit = async (values: AssetLibrarySettingsValues) => {
    for (const key of Object.keys(values) as Array<
      keyof AssetLibrarySettingsValues
    >) {
      const value = values[key]
      const initial = props.defaultValues[key]
      const changed = Array.isArray(value)
        ? JSON.stringify(value) !== JSON.stringify(initial)
        : value !== initial
      if (!changed) continue
      await updateOption.mutateAsync({
        key: `asset_library_setting.${key}`,
        value: Array.isArray(value) ? JSON.stringify(value) : value,
      })
    }
  }

  const userGroupsQuery = useQuery({
    queryKey: ['asset-library', 'user-groups'],
    queryFn: listUserGroups,
  })
  const userGroups = userGroupsQuery.data ?? []

  return (
    <SettingsSection title={t('Asset Library')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)} autoComplete='off'>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            saveLabel='Save asset library settings'
          />
          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable the asset library')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Users manage their own Seedance private assets on the Asset Library page. While off, the page and its APIs do not exist.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />
          <FormField
            control={form.control}
            name='block_asset_refs_when_disabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>
                    {t('Block all asset:// references while off')}
                  </FormLabel>
                  <FormDescription>
                    {t(
                      'Any API key of the Volcengine project can use any asset ID, so while the library is off, requests referencing asset:// are refused unless this is turned off.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />
          <FormField
            control={form.control}
            name='max_groups_per_user'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Asset groups per user')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={0}
                    step={1}
                    {...safeNumberFieldProps(field)}
                  />
                </FormControl>
                <FormDescription>{t('0 means unlimited.')}</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='max_assets_per_user'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Assets per user')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={0}
                    step={1}
                    {...safeNumberFieldProps(field)}
                  />
                </FormControl>
                <FormDescription>{t('0 means unlimited.')}</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='allowed_user_groups'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Allowed user groups')}</FormLabel>
                <FormControl>
                  <MultiSelect
                    options={userGroups.map((group) => ({
                      label: group,
                      value: group,
                    }))}
                    selected={field.value}
                    onChange={field.onChange}
                    placeholder={t('All user groups')}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Users of these groups see and use the asset library. Leave empty for every group.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='compliance_notice'
            render={({ field }) => (
              <FormItem data-settings-form-span='full'>
                <FormLabel>{t('Compliance notice')}</FormLabel>
                <FormControl>
                  <Textarea rows={4} {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    'Shown to users before they create an asset group; they must agree to continue.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </SettingsForm>
      </Form>
      <AssetLibraryAccounts userGroups={userGroups} />
    </SettingsSection>
  )
}

function AssetLibraryAccounts(props: { userGroups: string[] }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState<AssetLibraryAccount | null>(null)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [deleting, setDeleting] = useState<AssetLibraryAccount | null>(null)
  const accountsQuery = useQuery({
    queryKey: assetLibraryAccountsQueryKey,
    queryFn: listAssetLibraryAccounts,
  })
  const deleteMutation = useMutation({
    mutationFn: (id: number) => deleteAssetLibraryAccount(id),
    onSuccess: async () => {
      toast.success(t('Deleted successfully'))
      setDeleting(null)
      await queryClient.invalidateQueries({
        queryKey: assetLibraryAccountsQueryKey,
      })
    },
    onError: (error) => handleServerError(error),
  })

  const openDialog = (account: AssetLibraryAccount | null) => {
    setEditing(account)
    setDialogOpen(true)
  }

  return (
    <div className='flex flex-col gap-3'>
      <div className='flex items-center justify-between gap-3'>
        <div className='min-w-0'>
          <h4 className='text-sm font-medium'>{t('Volcengine accounts')}</h4>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Each asset group lives in one account; only channels bound to that account can use its assets.'
            )}
          </p>
        </div>
        <Button size='sm' onClick={() => openDialog(null)}>
          <Plus className='size-4' />
          {t('Add account')}
        </Button>
      </div>
      <StaticDataTable
        data={accountsQuery.data ?? []}
        getRowKey={(account) => account.id}
        emptyContent={
          accountsQuery.isLoading
            ? t('Loading...')
            : t('No Volcengine accounts yet')
        }
        columns={[
          {
            id: 'name',
            header: t('Name'),
            cell: (account) => (
              <div className='min-w-0'>
                <div className='font-medium'>{account.name}</div>
                <div className='text-muted-foreground font-mono text-xs'>
                  {account.access_key_id_masked}
                </div>
              </div>
            ),
          },
          {
            id: 'project',
            header: t('Region / project'),
            cell: (account) => `${account.region} / ${account.project_name}`,
          },
          {
            id: 'types',
            header: t('Group types'),
            cell: (account) =>
              [
                account.aigc_enabled ? t('Virtual avatars') : null,
                account.liveness_face_enabled ? t('Real people') : null,
              ]
                .filter(Boolean)
                .join(' / ') || '-',
          },
          {
            id: 'priority',
            header: t('Priority'),
            cell: (account) => account.priority,
          },
          {
            id: 'status',
            header: t('Status'),
            cell: (account) => (
              <StatusBadge
                label={account.enabled ? t('Enabled') : t('Disabled')}
                variant={account.enabled ? 'success' : 'neutral'}
                copyable={false}
              />
            ),
          },
          {
            id: 'actions',
            header: <span className='sr-only'>{t('Actions')}</span>,
            cell: (account) => (
              <StaticRowActions
                editLabel={t('Edit')}
                deleteLabel={t('Delete')}
                menuLabel={t('Open menu')}
                onEdit={() => openDialog(account)}
                onDelete={() => setDeleting(account)}
              />
            ),
          },
        ]}
      />
      <AssetLibraryAccountDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        account={editing}
        userGroups={props.userGroups}
      />
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={t('Delete Volcengine account')}
        desc={t(
          'The account is removed from the gateway only; its assets on Volcengine are kept.'
        )}
        destructive
        isLoading={deleteMutation.isPending}
        handleConfirm={() => deleting && deleteMutation.mutate(deleting.id)}
      />
    </div>
  )
}
