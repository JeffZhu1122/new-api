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
import { useFormContext } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'

import type { ChannelFormValues } from '../lib/channel-form'

export function ChannelTestRequestSettings(props: { disabled?: boolean }) {
  const { t } = useTranslation()
  const form = useFormContext<ChannelFormValues>()

  return (
    <>
      <FormField
        control={form.control}
        name='test_message'
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('Test Message')}</FormLabel>
            <FormControl>
              <Textarea
                rows={2}
                placeholder='hi'
                {...field}
                value={field.value ?? ''}
                disabled={props.disabled}
              />
            </FormControl>
            <FormDescription>
              {t(
                'The prompt the channel test sends to chat models. Leave it empty to send the built-in "hi".'
              )}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name='test_max_tokens'
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('Test Max Tokens')}</FormLabel>
            <FormControl>
              <Input
                type='number'
                min={0}
                placeholder='0'
                {...field}
                value={field.value ?? 0}
                onChange={(e) => field.onChange(Number(e.target.value))}
                disabled={props.disabled}
              />
            </FormControl>
            <FormDescription>
              {t(
                'The output token limit of the channel test request. 0 keeps the built-in limit.'
              )}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  )
}
