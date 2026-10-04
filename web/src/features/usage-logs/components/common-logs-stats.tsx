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
import { getRouteApi } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { Skeleton } from '@/components/ui/skeleton'
import { toIntlLocale } from '@/i18n/languages'
import { formatLogQuota, formatNumber } from '@/lib/format'
import { requireServerSuccess } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'

import { getLogStats, getUserLogStats } from '../api'
import { DEFAULT_LOG_STATS } from '../constants'
import { buildApiParams } from '../lib/utils'
import { useLogsViewScope, useUsageLogsContext } from './usage-logs-provider'

const route = getRouteApi('/_authenticated/usage-logs/$section')

function StatBadge(props: {
  label: string
  value: string | number
  accent: string
}) {
  return (
    <span className='border-border/60 bg-muted/25 inline-flex h-7 items-center gap-2 rounded-md border px-2.5 text-xs shadow-xs'>
      <span className={cn('h-3.5 w-0.5 rounded-full', props.accent)} />
      <span className='text-muted-foreground'>{props.label}</span>
      <span className='text-foreground/85 font-mono font-semibold tabular-nums'>
        {props.value}
      </span>
    </span>
  )
}

/** The live RPM / TPM are cheap Redis reads, so they poll on their own. */
const LIVE_RATE_REFRESH_INTERVAL_MS = 10_000

export function CommonLogsStats() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const { isAdminView: isAdmin } = useLogsViewScope()
  const searchParams = route.useSearch()
  const { sensitiveVisible } = useUsageLogsContext()

  const fetchStats = async (rateOnly: boolean) => {
    const params = {
      ...buildApiParams({
        page: 1,
        pageSize: 1,
        searchParams,
        columnFilters: [],
        isAdmin,
      }),
      ...(rateOnly ? { rate_only: true } : {}),
    }
    const result = isAdmin
      ? requireServerSuccess(await getLogStats(params))
      : requireServerSuccess(await getUserLogStats(params))
    return result.success ? result.data || DEFAULT_LOG_STATS : DEFAULT_LOG_STATS
  }

  // Quota of the selected time range: one logs-table query per filter change.
  const { data: stats, isLoading } = useQuery({
    queryKey: ['usage-logs-stats', isAdmin, searchParams],
    queryFn: () => fetchStats(false),
    placeholderData: (previousData) => previousData,
  })

  // RPM / TPM of the last settled minute, refreshed without touching the logs table.
  const rateQuery = useQuery({
    queryKey: ['usage-logs-rate', isAdmin, searchParams],
    queryFn: () => fetchStats(true),
    placeholderData: (previousData) => previousData,
    refetchInterval: LIVE_RATE_REFRESH_INTERVAL_MS,
    refetchIntervalInBackground: false,
    meta: { errorToast: false },
  })
  const rate = rateQuery.isError ? undefined : rateQuery.data
  const rateAvailable =
    rate?.rate_source === 'redis' || rate?.rate_source === 'memory'
  const rateValue = (value: number | undefined) =>
    rateAvailable ? formatNumber(value ?? 0, locale) : '-'

  if (isLoading) {
    return (
      <div className='flex items-center gap-2'>
        <Skeleton className='h-7 w-[150px] rounded-md' />
        <Skeleton className='h-7 w-[100px] rounded-md' />
        <Skeleton className='h-7 w-[120px] rounded-md' />
      </div>
    )
  }

  return (
    <div className='flex flex-wrap items-center gap-2'>
      <StatBadge
        label={t('Usage')}
        value={sensitiveVisible ? formatLogQuota(stats?.quota || 0) : '••••'}
        accent='bg-sky-500/70'
      />
      <StatBadge
        label={t('RPM')}
        value={rateValue(rate?.rpm)}
        accent='bg-rose-500/65'
      />
      <StatBadge
        label={t('TPM')}
        value={rateValue(rate?.tpm)}
        accent='bg-slate-400/70'
      />
      <StatBadge
        label={t('Input TPM')}
        value={rateValue(rate?.tpm_input)}
        accent='bg-emerald-500/60'
      />
      <StatBadge
        label={t('Cache read TPM')}
        value={rateValue(rate?.tpm_cache_read)}
        accent='bg-amber-500/60'
      />
      <StatBadge
        label={t('Cache write TPM')}
        value={rateValue(rate?.tpm_cache_write)}
        accent='bg-orange-500/60'
      />
      <StatBadge
        label={t('Output TPM')}
        value={rateValue(rate?.tpm_output)}
        accent='bg-violet-500/60'
      />
      {rate?.rate_source === 'memory' && (
        <span className='text-muted-foreground text-xs'>
          {t('Counted on this server instance only.')}
        </span>
      )}
    </div>
  )
}
