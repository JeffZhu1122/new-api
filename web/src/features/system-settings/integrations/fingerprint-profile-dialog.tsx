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
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
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
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'

import {
  type FingerprintProfile,
  type FingerprintProfileFormValues,
  TLS_SOURCE_CUSTOM,
  TLS_SOURCE_DEFAULT,
  createFingerprintProfileSchema,
  fingerprintProfileFormValues,
  fingerprintProfileFromForm,
  tlsPresetSource,
} from './fingerprint-profile-schema'

const FINGERPRINT_PROFILE_FORM_ID = 'fingerprint-profile-form'

type FingerprintProfileDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  // The profile being edited; null adds a new one.
  profile: FingerprintProfile | null
  // Names of the other stored profiles.
  takenNames: string[]
  tlsPresets: string[]
  isSaving: boolean
  onSave: (profile: FingerprintProfile) => void
}

export function FingerprintProfileDialog(props: FingerprintProfileDialogProps) {
  const { t } = useTranslation()
  const isEdit = props.profile !== null
  const form = useForm<FingerprintProfileFormValues>({
    resolver: zodResolver(createFingerprintProfileSchema(t, props.takenNames)),
    defaultValues: fingerprintProfileFormValues(props.profile),
  })
  const tlsSource = useWatch({ control: form.control, name: 'tls_source' })

  // A preset the profile already uses stays selectable even when this build
  // no longer lists it, so opening the dialog never silently changes it.
  const presets = [...props.tlsPresets]
  const storedPreset = props.profile?.tls_preset
  if (storedPreset && !presets.includes(storedPreset)) {
    presets.push(storedPreset)
  }
  const tlsSourceItems = [
    { value: TLS_SOURCE_DEFAULT, label: t('Go default') },
    ...presets.map((preset) => ({
      value: tlsPresetSource(preset),
      label: preset,
    })),
    { value: TLS_SOURCE_CUSTOM, label: t('Custom capture (hex)') },
  ]

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={
        isEdit ? t('Edit fingerprint profile') : t('Add fingerprint profile')
      }
      description={t(
        'Every part is optional. A part left empty keeps the Go default for that part.'
      )}
      contentClassName='sm:max-w-[640px]'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='submit'
            form={FINGERPRINT_PROFILE_FORM_ID}
            disabled={props.isSaving}
          >
            {isEdit ? t('Update') : t('Add')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <form
          id={FINGERPRINT_PROFILE_FORM_ID}
          onSubmit={form.handleSubmit((values) =>
            props.onSave(fingerprintProfileFromForm(values))
          )}
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
                  <Input
                    placeholder='chrome-133'
                    disabled={isEdit}
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Channels select a profile by its name, so the name cannot be changed later.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='tls_source'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('TLS ClientHello')}</FormLabel>
                <Select
                  items={tlsSourceItems}
                  value={field.value}
                  onValueChange={(value) =>
                    value !== null && field.onChange(value)
                  }
                >
                  <FormControl>
                    <SelectTrigger aria-label={t('TLS ClientHello')}>
                      <SelectValue />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      {tlsSourceItems.map((item) => (
                        <SelectItem key={item.value} value={item.value}>
                          {item.label}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FormDescription>
                  {t(
                    'A built-in preset imitates that client. A custom capture replays a ClientHello you recorded.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          {tlsSource === TLS_SOURCE_CUSTOM && (
            <FormField
              control={form.control}
              name='tls_client_hello'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('ClientHello capture')}</FormLabel>
                  <FormControl>
                    <Textarea
                      rows={5}
                      className='font-mono text-xs'
                      placeholder='16 03 01 02 00 01 00 01 fc 03 03 ...'
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Hex of the whole TLS record or of the handshake message. Spaces, line breaks and colons are ignored.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          )}
          <FormField
            control={form.control}
            name='http2_fingerprint'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('HTTP/2 fingerprint')}</FormLabel>
                <FormControl>
                  <Input
                    className='font-mono'
                    placeholder='1:65536;2:0;4:4194304;6:262144|15663105|0|m,a,s,p'
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'SETTINGS|WINDOW_UPDATE|PRIORITY|pseudo-header order, as HTTP/2 fingerprint reports show it.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='http2_header_priority'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('HTTP/2 header priority')}</FormLabel>
                <FormControl>
                  <Input
                    className='font-mono'
                    placeholder='1:0:256'
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'The exclusive:dependency:weight priority every HEADERS frame carries.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='header_order'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Header order')}</FormLabel>
                <FormControl>
                  <Textarea
                    rows={5}
                    className='font-mono text-xs'
                    placeholder={
                      'host\ncontent-type\nauthorization\nuser-agent'
                    }
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'One header name per line, in the order they are written. Headers not listed follow in the default order.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='user_agent'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Default User-Agent')}</FormLabel>
                <FormControl>
                  <Input {...field} />
                </FormControl>
                <FormDescription>
                  {t('Sent when a request carries no User-Agent of its own.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </form>
      </Form>
    </Dialog>
  )
}
