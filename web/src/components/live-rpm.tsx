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
/* eslint-disable react-refresh/only-export-components */
import { useQuery } from '@tanstack/react-query'
import { createContext, useContext, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { QuotaDetailsPopover } from '@/components/quota-details-popover'
import { StatusBadge } from '@/components/status-badge'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'
import { requireServerSuccess } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'

/**
 * Where the server counted: shared Redis (all instances), this instance only,
 * or statistics switched off with RPM_STATS_ENABLED=false.
 */
export type LiveRpmSource = 'redis' | 'memory' | 'disabled'

/** RPM per id over the last settled minute (`GET /api/{channel,user}/rpm`). */
export type LiveRpmTotals = {
  source: LiveRpmSource
  window_start: number
  window_end: number
  items: Record<string, number>
}

export type LiveRpmBreakdownItem = { id: number; name: string; rpm: number }

/** One row split by user or by channel, busiest first. */
export type LiveRpmBreakdown = {
  source: LiveRpmSource
  window_start: number
  window_end: number
  total: number
  items: LiveRpmBreakdownItem[]
}

export type LiveRpmResponse<T> = {
  success: boolean
  message?: string
  data?: T
}

export const LIVE_RPM_REFRESH_INTERVAL_MS = 10_000
const SENSITIVE_MASK = '••••'

/** Totals for the rows on screen; RPM cells read them from here. */
export const LiveRpmContext = createContext<LiveRpmTotals | undefined>(
  undefined
)

/**
 * Polls the live RPM of the given ids while the page is visible. Failures stay
 * silent and yield `undefined`, so cells show "-" instead of stale numbers and
 * a polling error never floods toasts.
 */
export function useLiveRpmTotals(
  scope: string,
  ids: number[],
  fetchTotals: (ids: number[]) => Promise<LiveRpmResponse<LiveRpmTotals>>
): LiveRpmTotals | undefined {
  const query = useQuery({
    // fetchTotals is fixed per scope, so the scope stands in for it in the key
    // eslint-disable-next-line @tanstack/query/exhaustive-deps
    queryKey: ['live-rpm', scope, ids],
    queryFn: async () => requireServerSuccess(await fetchTotals(ids)).data,
    enabled: ids.length > 0,
    refetchInterval: LIVE_RPM_REFRESH_INTERVAL_MS,
    refetchIntervalInBackground: false,
    placeholderData: (previous) => previous,
    meta: { errorToast: false },
  })
  return query.isError ? undefined : query.data
}

/**
 * Lazily loaded split of one row's RPM, shown in a popover when the value is
 * clicked.
 */
export type LiveRpmBreakdownSource = {
  title: string
  description: string
  queryKey: readonly unknown[]
  fetch: () => Promise<LiveRpmResponse<LiveRpmBreakdown>>
  /** Hides names (privacy toggle); ids stay visible. */
  maskNames?: boolean
}

type LiveRpmCellProps = {
  /** Ids summed into the value: the row itself, or every channel of a tag row. */
  ids: number[]
  breakdown?: LiveRpmBreakdownSource
}

export function LiveRpmCell(props: LiveRpmCellProps) {
  const totals = useContext(LiveRpmContext)
  let value: number | undefined
  if (totals && totals.source !== 'disabled') {
    value = 0
    for (const id of props.ids) {
      const count = totals.items[String(id)]
      if (count === undefined) {
        value = undefined
        break
      }
      value += count
    }
  }

  if (!props.breakdown) {
    return <LiveRpmBadge value={value} className='-ml-1.5' />
  }
  return (
    <LiveRpmBreakdownPopover
      value={value}
      source={totals?.source}
      breakdown={props.breakdown}
    />
  )
}

function LiveRpmBadge(props: {
  value: number | undefined
  className?: string
}) {
  const { i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  if (props.value === undefined) {
    return <span className='text-muted-foreground text-xs'>-</span>
  }
  return (
    <StatusBadge
      label={formatNumber(props.value, locale)}
      variant={props.value > 0 ? 'info' : 'neutral'}
      size='sm'
      copyable={false}
      className={cn('tabular-nums', props.className)}
    />
  )
}

function LiveRpmBreakdownPopover(props: {
  value: number | undefined
  source: LiveRpmSource | undefined
  breakdown: LiveRpmBreakdownSource
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [open, setOpen] = useState(false)
  const query = useQuery({
    // fetch is bound to the row the query key already identifies
    // eslint-disable-next-line @tanstack/query/exhaustive-deps
    queryKey: props.breakdown.queryKey,
    queryFn: async () =>
      requireServerSuccess(await props.breakdown.fetch()).data,
    enabled: open,
    refetchInterval: open ? LIVE_RPM_REFRESH_INTERVAL_MS : false,
    refetchIntervalInBackground: false,
    meta: { errorToast: false },
  })

  const items = query.data?.items ?? []
  const details = items.map((item) => {
    const name = props.breakdown.maskNames
      ? SENSITIVE_MASK
      : item.name || t('Unknown')
    return {
      label: `${name} #${item.id}`,
      value: formatNumber(item.rpm, locale),
    }
  })
  const notes = [props.breakdown.description]
  if (query.isPending) {
    notes.unshift(t('Loading...'))
  } else if (query.isError) {
    notes.unshift(t('Failed to load RPM statistics'))
  } else if (details.length === 0) {
    notes.unshift(t('No requests in the last minute'))
  }
  if ((query.data?.source ?? props.source) === 'memory') {
    notes.push(t('Counted on this server instance only.'))
  }
  const valueText =
    props.value === undefined ? '-' : formatNumber(props.value, locale)

  // The table offset sits on the wrapper: on the badge it would overflow the
  // size-to-content trigger and clip the last digit.
  return (
    <QuotaDetailsPopover
      title={props.breakdown.title}
      triggerLabel={`${t('RPM')} ${valueText}`}
      details={details}
      description={notes.join(' ')}
      onOpenChange={setOpen}
      className='-ml-1.5 w-auto'
      triggerClassName='w-auto'
    >
      <LiveRpmBadge value={props.value} />
    </QuotaDetailsPopover>
  )
}
