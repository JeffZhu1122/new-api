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
import { useQuery } from '@tanstack/react-query'
import { useFormContext, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import { getFingerprintProfileOptions } from '../api'
import { channelsQueryKeys } from '../lib/channel-actions'
import {
  HTTP_PROTOCOL_HTTP3,
  type ChannelFormValues,
} from '../lib/channel-form'

// Not a legal profile name, so it can never collide with a stored one.
const NO_PROFILE = '<off>'

export function ChannelFingerprintProfileSetting(props: {
  disabled?: boolean
}) {
  const { t } = useTranslation()
  const form = useFormContext<ChannelFormValues>()
  const protocol = useWatch({ control: form.control, name: 'http_protocol' })
  const selected = useWatch({
    control: form.control,
    name: 'fingerprint_profile',
  })
  const optionsQuery = useQuery({
    queryKey: channelsQueryKeys.fingerprintProfiles(),
    queryFn: getFingerprintProfileOptions,
    meta: { errorToast: false },
  })

  const http3Selected = protocol === HTTP_PROTOCOL_HTTP3
  const disabled = props.disabled || http3Selected
  // A profile the channel already selects stays listed while the options load
  // or after the profile was removed, so opening the channel never drops it.
  const names = optionsQuery.data?.profiles ?? []
  const listed =
    selected && !names.includes(selected) ? [selected, ...names] : names

  let description = t(
    'Send the requests of this channel with the TLS, HTTP/2 and header fingerprint of a profile from the system settings. Off keeps the default Go transport.'
  )
  if (http3Selected) {
    description = t(
      'Fingerprint profiles are unavailable when HTTP/3 is selected.'
    )
  } else if (selected && optionsQuery.data?.enabled === false) {
    description = t(
      'Fingerprint profiles are turned off in the system settings, so this channel uses the default Go transport for now.'
    )
  }

  return (
    <FormField
      control={form.control}
      name='fingerprint_profile'
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t('Fingerprint Profile')}</FormLabel>
          <Select
            items={[
              { value: NO_PROFILE, label: t('Off') },
              ...listed.map((name) => ({ value: name, label: name })),
            ]}
            value={field.value || NO_PROFILE}
            onValueChange={(value) =>
              value !== null &&
              field.onChange(value === NO_PROFILE ? '' : value)
            }
            disabled={disabled}
          >
            <FormControl>
              <SelectTrigger
                aria-label={t('Fingerprint Profile')}
                disabled={disabled}
              >
                <SelectValue />
              </SelectTrigger>
            </FormControl>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                <SelectItem value={NO_PROFILE}>{t('Off')}</SelectItem>
                {listed.map((name) => (
                  <SelectItem key={name} value={name}>
                    {name}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
          <FormDescription>{description}</FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}
