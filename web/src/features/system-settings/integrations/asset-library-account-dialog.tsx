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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { Dialog } from '@/components/dialog'
import { MultiSelect } from '@/components/multi-select'
import { PasswordInput } from '@/components/password-input'
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
import { handleServerError } from '@/lib/handle-server-error'

import {
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { safeNumberFieldProps } from '../utils/numeric-field'
import {
  type AssetLibraryAccount,
  type AssetLibraryAccountPayload,
  assetLibraryAccountsQueryKey,
  saveAssetLibraryAccount,
  testAssetLibraryAccount,
} from './asset-library-api'

const ASSET_LIBRARY_ACCOUNT_FORM_ID = 'asset-library-account-form'

const createAccountSchema = (
  t: (key: string) => string,
  requireKeys: boolean
) =>
  z.object({
    name: z
      .string()
      .trim()
      .min(1, t('Account name must be 1 to 64 characters'))
      .max(64, t('Account name must be 1 to 64 characters')),
    access_key_id: requireKeys
      ? z.string().trim().min(1, t('Access Key ID is required'))
      : z.string(),
    secret_access_key: requireKeys
      ? z.string().trim().min(1, t('Secret Access Key is required'))
      : z.string(),
    region: z.string().trim(),
    project_name: z.string().trim().max(64),
    endpoint: z
      .string()
      .trim()
      .refine(
        (value) => value === '' || /^https?:\/\/[^?#]+$/.test(value),
        t('Invalid endpoint URL')
      ),
    enabled: z.boolean(),
    aigc_enabled: z.boolean(),
    liveness_face_enabled: z.boolean(),
    allowed_user_groups: z.array(z.string()),
    priority: z.number().int(),
    max_groups: z.number().int().min(0),
    max_assets: z.number().int().min(0),
    create_asset_qpm: z.number().int().min(0),
  })

type AccountFormValues = z.infer<ReturnType<typeof createAccountSchema>>

type AssetLibraryAccountDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  // The account being edited; null adds a new one.
  account: AssetLibraryAccount | null
  userGroups: string[]
}

function accountFormValues(
  account: AssetLibraryAccount | null
): AccountFormValues {
  return {
    name: account?.name ?? '',
    access_key_id: '',
    secret_access_key: '',
    region: account?.region ?? 'cn-beijing',
    project_name: account?.project_name ?? 'default',
    endpoint: account?.endpoint ?? '',
    enabled: account?.enabled ?? true,
    aigc_enabled: account?.aigc_enabled ?? true,
    liveness_face_enabled: account?.liveness_face_enabled ?? true,
    allowed_user_groups: account?.allowed_user_groups ?? [],
    priority: account?.priority ?? 0,
    max_groups: account?.max_groups ?? 1000000,
    max_assets: account?.max_assets ?? 1000000,
    create_asset_qpm: account?.create_asset_qpm ?? 120,
  }
}

export function AssetLibraryAccountDialog(
  props: AssetLibraryAccountDialogProps
) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const isEdit = props.account !== null
  const values = useMemo(
    () => accountFormValues(props.account),
    [props.account]
  )
  const form = useForm<AccountFormValues>({
    resolver: zodResolver(createAccountSchema(t, !isEdit)),
    values,
  })

  const toPayload = (
    values: AccountFormValues
  ): AssetLibraryAccountPayload => ({
    ...values,
    id: props.account?.id,
  })

  const saveMutation = useMutation({
    mutationFn: (values: AccountFormValues) =>
      saveAssetLibraryAccount(toPayload(values)),
    onSuccess: async () => {
      toast.success(t('Saved successfully'))
      await queryClient.invalidateQueries({
        queryKey: assetLibraryAccountsQueryKey,
      })
      props.onOpenChange(false)
    },
    onError: (error) => handleServerError(error),
  })

  const testMutation = useMutation({
    mutationFn: (values: AccountFormValues) =>
      testAssetLibraryAccount(toPayload(values)),
    onSuccess: (message) =>
      toast.success(message || t('Connection test succeeded')),
    onError: (error) => handleServerError(error),
  })

  const groupOptions = props.userGroups.map((group) => ({
    label: group,
    value: group,
  }))
  const keyPlaceholder = isEdit ? t('Leave blank to keep the current key') : ''

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={
        isEdit ? t('Edit Volcengine account') : t('Add Volcengine account')
      }
      description={t(
        'The Access Key needs the ark:*Asset* permission in the project the Seedance channels use.'
      )}
      contentClassName='sm:max-w-[640px]'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            disabled={testMutation.isPending}
            onClick={form.handleSubmit((values) => testMutation.mutate(values))}
          >
            {testMutation.isPending ? t('Testing...') : t('Test connection')}
          </Button>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='submit'
            form={ASSET_LIBRARY_ACCOUNT_FORM_ID}
            disabled={saveMutation.isPending}
          >
            {isEdit ? t('Update') : t('Add')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <form
          id={ASSET_LIBRARY_ACCOUNT_FORM_ID}
          onSubmit={form.handleSubmit((values) => saveMutation.mutate(values))}
          className='space-y-4'
          autoComplete='off'
        >
          <FormField
            control={form.control}
            name='name'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Name')}</FormLabel>
                <FormControl>
                  <Input {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <div className='grid gap-4 sm:grid-cols-2'>
            <FormField
              control={form.control}
              name='access_key_id'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Access Key ID')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder={
                        props.account?.access_key_id_masked || keyPlaceholder
                      }
                      autoComplete='off'
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='secret_access_key'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Secret Access Key')}</FormLabel>
                  <FormControl>
                    <PasswordInput
                      placeholder={keyPlaceholder}
                      autoComplete='new-password'
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='region'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Region')}</FormLabel>
                  <FormControl>
                    <Input placeholder='cn-beijing' {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='project_name'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Project name')}</FormLabel>
                  <FormControl>
                    <Input placeholder='default' {...field} />
                  </FormControl>
                  <FormDescription>
                    {t('Assets only work with API keys of the same project.')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
          <FormField
            control={form.control}
            name='endpoint'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Endpoint (optional)')}</FormLabel>
                <FormControl>
                  <Input
                    placeholder='https://ark.cn-beijing.volcengineapi.com'
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t('Leave blank to use the public endpoint of the region.')}
                </FormDescription>
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
                    options={groupOptions}
                    selected={field.value}
                    onChange={field.onChange}
                    placeholder={t('All user groups')}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Users of these groups may create asset groups in this account. Leave empty for every group.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <div className='grid gap-4 sm:grid-cols-2'>
            <FormField
              control={form.control}
              name='priority'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Priority')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      step={1}
                      {...safeNumberFieldProps(field)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'New asset groups go to the highest-priority account with room.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='create_asset_qpm'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Uploads per minute')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      step={1}
                      {...safeNumberFieldProps(field)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'The CreateAsset limit of the subscribed tier. 0 means unlimited.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='max_groups'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Asset group quota')}</FormLabel>
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
              name='max_assets'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Asset quota')}</FormLabel>
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
          </div>
          {(
            [
              [
                'enabled',
                t('Enabled'),
                t('Disabled accounts take no new groups.'),
              ],
              [
                'aigc_enabled',
                t('Virtual avatars'),
                t('Users may create virtual avatar (AIGC) groups here.'),
              ],
              [
                'liveness_face_enabled',
                t('Real people'),
                t('Users may create verified real-person groups here.'),
              ],
            ] as const
          ).map(([name, label, description]) => (
            <FormField
              key={name}
              control={form.control}
              name={name}
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{label}</FormLabel>
                    <FormDescription>{description}</FormDescription>
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
          ))}
        </form>
      </Form>
    </Dialog>
  )
}
