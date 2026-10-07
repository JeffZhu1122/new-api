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
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { toIntlLocale } from '@/i18n/languages'
import { formatTimestampToDate } from '@/lib/format'

import { getLogFileCleanupSchedule } from '../api'
import {
  SettingsControlChildren,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import {
  LOG_FILE_CLEANUP_MAX_MONTH_DAY,
  LOG_FILE_CLEANUP_SCHEDULE_QUERY_KEY,
  type LogFileCleanupScheduleValues,
} from './log-file-cleanup-schedule-schema'

type ScheduleFormValues = {
  log_file_cleanup_setting: LogFileCleanupScheduleValues
}

type LogFileCleanupScheduleFieldsProps = {
  formatSize: (bytes: number) => string
}

// 2023-01-01 is a Sunday, so day n of that week has getUTCDay() === n.
const WEEKDAY_REFERENCE = Date.UTC(2023, 0, 1)

/**
 * Scheduled cleanup of the server log files. Rendered inside the log
 * maintenance form; the page-level Save stores the fields as
 * `log_file_cleanup_setting.*` options. Each node applies the schedule to its
 * own log directory, in the server's time zone.
 */
export function LogFileCleanupScheduleFields(
  props: LogFileCleanupScheduleFieldsProps
) {
  const { t, i18n } = useTranslation()
  const { control } = useFormContext<ScheduleFormValues>()
  const enabled = useWatch({
    control,
    name: 'log_file_cleanup_setting.enabled',
  })
  const frequency = useWatch({
    control,
    name: 'log_file_cleanup_setting.frequency',
  })
  const mode = useWatch({ control, name: 'log_file_cleanup_setting.mode' })

  const statusQuery = useQuery({
    queryKey: LOG_FILE_CLEANUP_SCHEDULE_QUERY_KEY,
    queryFn: async () => {
      const res = await getLogFileCleanupSchedule()
      return res.success ? (res.data ?? null) : null
    },
    refetchInterval: 60_000,
    meta: { errorToast: false },
  })
  const status = statusQuery.data

  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const weekdayFormatter = new Intl.DateTimeFormat(locale, {
    weekday: 'long',
    timeZone: 'UTC',
  })
  const weekdayItems = Array.from({ length: 7 }, (_, day) => ({
    value: String(day),
    label: weekdayFormatter.format(
      new Date(WEEKDAY_REFERENCE + day * 24 * 60 * 60 * 1000)
    ),
  }))
  const monthDayItems = Array.from(
    { length: LOG_FILE_CLEANUP_MAX_MONTH_DAY },
    (_, index) => ({ value: String(index + 1), label: String(index + 1) })
  )
  const frequencyItems = [
    { value: 'daily', label: t('Daily') },
    { value: 'weekly', label: t('Weekly') },
    { value: 'monthly', label: t('Monthly') },
  ]
  const modeItems = [
    { value: 'by_count', label: t('Retain last N files') },
    { value: 'by_days', label: t('Retain last N days') },
  ]
  const lastRun = status?.last_run ?? null

  return (
    <div className='space-y-3'>
      <FormField
        control={control}
        name='log_file_cleanup_setting.enabled'
        render={({ field }) => (
          <SettingsSwitchItem>
            <SettingsSwitchContent>
              <FormLabel>{t('Scheduled log file cleanup')}</FormLabel>
              <FormDescription>
                {t(
                  'Clean up server log files automatically at the time you choose, with the same retention rules as manual cleanup.'
                )}
              </FormDescription>
            </SettingsSwitchContent>
            <FormControl>
              <Switch checked={field.value} onCheckedChange={field.onChange} />
            </FormControl>
          </SettingsSwitchItem>
        )}
      />

      {enabled && (
        <SettingsControlChildren className='space-y-4'>
          <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-4'>
            <FormField
              control={control}
              name='log_file_cleanup_setting.frequency'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Repeat')}</FormLabel>
                  <Select
                    items={frequencyItems}
                    value={field.value}
                    onValueChange={(value) => value && field.onChange(value)}
                  >
                    <FormControl>
                      <SelectTrigger className='w-full'>
                        <SelectValue />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent alignItemWithTrigger={false}>
                      <SelectGroup>
                        {frequencyItems.map((item) => (
                          <SelectItem key={item.value} value={item.value}>
                            {item.label}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </FormItem>
              )}
            />

            {frequency === 'weekly' && (
              <FormField
                control={control}
                name='log_file_cleanup_setting.weekday'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Weekday')}</FormLabel>
                    <Select
                      items={weekdayItems}
                      value={String(field.value)}
                      onValueChange={(value) =>
                        value !== null && field.onChange(Number(value))
                      }
                    >
                      <FormControl>
                        <SelectTrigger className='w-full'>
                          <SelectValue />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          {weekdayItems.map((item) => (
                            <SelectItem key={item.value} value={item.value}>
                              {item.label}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </FormItem>
                )}
              />
            )}

            {frequency === 'monthly' && (
              <FormField
                control={control}
                name='log_file_cleanup_setting.month_day'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Day of month')}</FormLabel>
                    <Select
                      items={monthDayItems}
                      value={String(field.value)}
                      onValueChange={(value) =>
                        value !== null && field.onChange(Number(value))
                      }
                    >
                      <FormControl>
                        <SelectTrigger className='w-full'>
                          <SelectValue />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          {monthDayItems.map((item) => (
                            <SelectItem key={item.value} value={item.value}>
                              {item.label}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </FormItem>
                )}
              />
            )}

            <FormField
              control={control}
              name='log_file_cleanup_setting.time'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Time')}</FormLabel>
                  <FormControl>
                    <Input
                      type='time'
                      step={60}
                      value={field.value}
                      onChange={(event) => field.onChange(event.target.value)}
                      onBlur={field.onBlur}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-4'>
            <FormField
              control={control}
              name='log_file_cleanup_setting.mode'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Cleanup Mode')}</FormLabel>
                  <Select
                    items={modeItems}
                    value={field.value}
                    onValueChange={(value) => value && field.onChange(value)}
                  >
                    <FormControl>
                      <SelectTrigger className='w-full'>
                        <SelectValue />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent alignItemWithTrigger={false}>
                      <SelectGroup>
                        {modeItems.map((item) => (
                          <SelectItem key={item.value} value={item.value}>
                            {item.label}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </FormItem>
              )}
            />

            <FormField
              control={control}
              name='log_file_cleanup_setting.value'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {mode === 'by_count'
                      ? t('Files to Retain')
                      : t('Days to Retain')}
                  </FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      max={mode === 'by_count' ? 1000 : 3650}
                      value={Number.isNaN(field.value) ? '' : field.value}
                      onChange={(event) =>
                        field.onChange(event.target.valueAsNumber)
                      }
                      onBlur={field.onBlur}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <div className='text-muted-foreground space-y-1 text-xs'>
            {status && (
              <p>
                {t(
                  'Runs on each server node in the server time zone ({{timezone}}, UTC{{offset}}).',
                  { timezone: status.timezone, offset: status.utc_offset }
                )}
              </p>
            )}
            {status && status.next_run_at > 0 && (
              <p>
                {t('Next run: {{time}}', {
                  time: formatTimestampToDate(status.next_run_at),
                })}
              </p>
            )}
            {lastRun && !lastRun.error && (
              <p>
                {t(
                  'Last run: {{time}}, removed {{count}} files and freed {{size}}.',
                  {
                    time: formatTimestampToDate(lastRun.started_at),
                    count: lastRun.deleted_count,
                    size: props.formatSize(lastRun.freed_bytes),
                  }
                )}
              </p>
            )}
            {lastRun?.error && (
              <p className='text-destructive'>
                {t('Last run: {{time}} failed: {{error}}', {
                  time: formatTimestampToDate(lastRun.started_at),
                  error: lastRun.error,
                })}
              </p>
            )}
          </div>
        </SettingsControlChildren>
      )}
    </div>
  )
}
