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
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormLabel,
} from '@/components/ui/form'
import { Switch } from '@/components/ui/switch'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'
import { FingerprintProfileList } from './fingerprint-profile-list'
import type { FingerprintProfile } from './fingerprint-profile-schema'

const fingerprintSwitchSchema = z.object({ enabled: z.boolean() })

type FingerprintSwitchValues = z.infer<typeof fingerprintSwitchSchema>

type FingerprintSettingsSectionProps = {
  enabled: boolean
  profiles: FingerprintProfile[]
}

export function FingerprintSettingsSection(
  props: FingerprintSettingsSectionProps
) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const form = useForm<FingerprintSwitchValues>({
    resolver: zodResolver(fingerprintSwitchSchema),
    defaultValues: { enabled: props.enabled },
  })
  useResetForm(form, { enabled: props.enabled })

  const onSubmit = (values: FingerprintSwitchValues) => {
    if (values.enabled === props.enabled) return
    updateOption.mutate({
      key: 'fingerprint_setting.enabled',
      value: values.enabled,
    })
  }

  return (
    <SettingsSection title={t('Outbound Fingerprint')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)} autoComplete='off'>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            saveLabel='Save outbound fingerprint settings'
          />
          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Allow fingerprint profiles')}</FormLabel>
                  <FormDescription>
                    {t(
                      'A profile only applies to the channels that select it. While this is off, every channel sends requests with the default Go transport, whatever profile it selects.'
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
        </SettingsForm>
      </Form>
      <FingerprintProfileList profiles={props.profiles} />
    </SettingsSection>
  )
}
