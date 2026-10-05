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
import { formatCompactNumber, formatNumber } from '@/lib/format'
import { requireServerSuccess } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'

/**
 * Where the server counted: shared Redis (all instances), this instance only,
 * or statistics switched off with RPM_STATS_ENABLED=false.
 */
export type LiveRpmSource = 'redis' | 'memory' | 'disabled'

/**
 * Settled usage within the window. `input` never contains cached tokens; TPM
 * is the sum of the four token categories.
 */
export type LiveTokenStats = {
  requests: number
  input: number
  cache_read: number
  cache_write: number
  output: number
}

export function liveTokenTotal(stats: LiveTokenStats): number {
  return stats.input + stats.cache_read + stats.cache_write + stats.output
}

/**
 * RPM (dispatch attempts) and settled token usage per id over the last
 * settled minute (`GET /api/{channel,user}/rpm`).
 */
export type LiveRpmTotals = {
  source: LiveRpmSource
  window_start: number
  window_end: number
  items: Record<string, number>
  tokens?: Record<string, LiveTokenStats>
}

export type LiveRpmBreakdownItem = {
  id: number
  name: string
  rpm: number
  tokens?: LiveTokenStats
}

/** One row split by user or by channel, busiest first. */
export type LiveRpmBreakdown = {
  source: LiveRpmSource
  window_start: number
  window_end: number
  total: number
  total_tokens?: LiveTokenStats
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
  // A malformed payload must blank the cell, never break the whole table.
  if (totals?.items && totals.source !== 'disabled') {
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
    return <LiveStatBadge value={value} className='-ml-1.5' />
  }
  return (
    <LiveRpmBreakdownPopover
      value={value}
      source={totals?.source}
      breakdown={props.breakdown}
    />
  )
}

function LiveStatBadge(props: {
  value: number | undefined
  /** Compact notation for token counts (12.3K). */
  compact?: boolean
  className?: string
}) {
  const { i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  if (props.value === undefined) {
    return <span className='text-muted-foreground text-xs'>-</span>
  }
  return (
    <StatusBadge
      label={
        props.compact
          ? formatCompactNumber(props.value, locale)
          : formatNumber(props.value, locale)
      }
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
    const rpm = `${t('RPM')} ${formatNumber(item.rpm, locale)}`
    const tpm = item.tokens
      ? ` · ${t('TPM')} ${formatCompactNumber(liveTokenTotal(item.tokens), locale)}`
      : ''
    return { label: `${name} #${item.id}`, value: rpm + tpm }
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
      <LiveStatBadge value={props.value} />
    </QuotaDetailsPopover>
  )
}

/**
 * Live TPM of a row (or the channels of a tag row) from the shared totals;
 * the value opens its split into input, cache read, cache write and output.
 */
export function LiveTpmCell(props: { ids: number[] }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const totals = useContext(LiveRpmContext)
  let stats: LiveTokenStats | undefined
  if (totals?.tokens && totals.source !== 'disabled') {
    stats = { requests: 0, input: 0, cache_read: 0, cache_write: 0, output: 0 }
    for (const id of props.ids) {
      const row = totals.tokens[String(id)]
      if (!row) {
        stats = undefined
        break
      }
      stats.requests += row.requests
      stats.input += row.input
      stats.cache_read += row.cache_read
      stats.cache_write += row.cache_write
      stats.output += row.output
    }
  }
  if (!stats) {
    return <LiveStatBadge value={undefined} />
  }
  const total = liveTokenTotal(stats)
  const notes = [
    t(
      'Tokens settled in the last full minute. Input excludes cached tokens. Refreshes every 10 seconds.'
    ),
  ]
  if (totals?.source === 'memory') {
    notes.push(t('Counted on this server instance only.'))
  }
  return (
    <QuotaDetailsPopover
      title={t('TPM breakdown')}
      triggerLabel={`${t('TPM')} ${formatNumber(total, locale)}`}
      details={[
        {
          label: t('Input (excluding cache)'),
          value: formatNumber(stats.input, locale),
        },
        {
          label: t('Cache Read'),
          value: formatNumber(stats.cache_read, locale),
        },
        {
          label: t('Cache Write'),
          value: formatNumber(stats.cache_write, locale),
        },
        { label: t('Output'), value: formatNumber(stats.output, locale) },
      ]}
      description={notes.join(' ')}
      className='-ml-1.5 w-auto'
      triggerClassName='w-auto'
    >
      <LiveStatBadge value={total} compact />
    </QuotaDetailsPopover>
  )
}
