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
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'

import { SettingsCard } from '../components/settings-card'
import {
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { safeNumberFieldProps } from '../utils/numeric-field'
import type { RoutingPolicyFormValues } from './routing-form'

export function RetrySection() {
  const { t } = useTranslation()
  const form = useFormContext<RoutingPolicyFormValues>()
  return (
    <SettingsCard title={t('Retry budget')} className='shadow-none'>
      <div className='space-y-4'>
        <FormField
          control={form.control}
          name='RetryTimes'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Maximum retries')}</FormLabel>
              <FormControl>
                <Input
                  type='number'
                  min={0}
                  max={99}
                  step={1}
                  {...safeNumberFieldProps(field)}
                />
              </FormControl>
              <FormDescription>
                {t('Excludes the first attempt. Counted per group.')}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='AutomaticRetryStatusCodes'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Auto-retry status codes')}</FormLabel>
              <FormControl>
                <Input
                  {...field}
                  placeholder={t('e.g. 401, 403, 429, 500-599')}
                />
              </FormControl>
              <FormDescription>
                {t('2xx, 504 and 524 are always excluded.')}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='AutomaticRetryKeywordsEnabled'
          render={({ field }) => (
            <SettingsSwitchItem>
              <SettingsSwitchContent>
                <FormLabel>{t('Retry 400 errors by keywords')}</FormLabel>
                <FormDescription>
                  {t(
                    'Master switch. Keyword-based retry of 400 errors only takes effect when enabled.'
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
          name='AutomaticRetryKeywords'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Retry keywords for 400 errors')}</FormLabel>
              <FormControl>
                <Textarea
                  rows={6}
                  placeholder={t('one keyword per line')}
                  {...field}
                  onChange={(event) => field.onChange(event.target.value)}
                />
              </FormControl>
              <FormDescription>
                {t(
                  'When upstream returns status code 400 and the error message contains any of these keywords (case insensitive), the request will be retried on another channel.'
                )}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='RetryAvoidFailedChannelsEnabled'
          render={({ field }) => (
            <SettingsSwitchItem>
              <SettingsSwitchContent>
                <FormLabel>{t('Avoid failed channels on retry')}</FormLabel>
                <FormDescription>
                  {t(
                    'When enabled, retries skip channels that already failed in this request. Multi-key channels are not excluded so keys can rotate. When every channel has failed, the configured status code and error message are returned.'
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
          name='RetryAvoidFailedChannelsStatusCode'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Status code when all channels failed')}</FormLabel>
              <FormControl>
                <Input
                  type='number'
                  min={100}
                  max={599}
                  step={1}
                  {...safeNumberFieldProps(field)}
                />
              </FormControl>
              <FormDescription>
                {t(
                  'HTTP status code returned when every available channel has already failed in this request (100-599).'
                )}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='RetryAvoidFailedChannelsErrorMessage'
          render={({ field }) => (
            <FormItem>
              <FormLabel>
                {t('Error message when all channels failed')}
              </FormLabel>
              <FormControl>
                <Textarea
                  rows={2}
                  {...field}
                  onChange={(event) => field.onChange(event.target.value)}
                />
              </FormControl>
              <FormDescription>
                {t(
                  'Supports the {model} placeholder for the model name. Leave empty to use the default message.'
                )}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      </div>
    </SettingsCard>
  )
}
