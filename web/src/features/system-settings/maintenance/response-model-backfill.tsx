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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { Progress } from '@/components/ui/progress'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber, formatTimestampToDate } from '@/lib/format'
import { requireServerSuccess } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'

import {
  getLogResponseModelBackfill,
  startLogResponseModelBackfill,
} from '../api'
import type {
  LogResponseModelBackfillStatus,
  LogResponseModelBackfillTask,
} from '../types'

const BACKFILL_QUERY_KEY = [
  'system-settings',
  'log-response-model-backfill',
] as const

/** While a run is queued or running, its progress is polled this often. */
const ACTIVE_POLL_INTERVAL_MS = 1000

function isActiveBackfill(task: LogResponseModelBackfillTask | null) {
  return task?.status === 'pending' || task?.status === 'running'
}

/** One line on the backfill when no run is in progress. */
function backfillSummary(
  status: LogResponseModelBackfillStatus,
  t: TFunction,
  locale: string | undefined
) {
  if (!status.supported) {
    return t('ClickHouse log databases are not backfilled')
  }
  const task = status.task
  if (task?.status === 'succeeded') {
    return t('The last run on {{date}} filled {{filled}} logs.', {
      date: formatTimestampToDate(task.updated_at),
      filled: formatNumber(task.result?.filled ?? 0, locale),
    })
  }
  if (task?.status === 'failed') {
    return t('The last run on {{date}} failed: {{error}}', {
      date: formatTimestampToDate(task.updated_at),
      error: task.error ?? '',
    })
  }
  if (status.done) {
    return t('The backfill has finished.')
  }
  return t('Not run yet. The master node starts it automatically.')
}

/**
 * The response model backfill module of the log maintenance page. A system
 * task fills the response model of the logs written before the response model
 * filters existed; this shows its latest run and runs it again.
 */
export function ResponseModelBackfill() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const titleId = useId()
  const queryClient = useQueryClient()
  const [confirmOpen, setConfirmOpen] = useState(false)

  const statusQuery = useQuery({
    queryKey: BACKFILL_QUERY_KEY,
    queryFn: async () =>
      requireServerSuccess(await getLogResponseModelBackfill()).data ?? null,
    refetchInterval: (query) =>
      isActiveBackfill(query.state.data?.task ?? null)
        ? ACTIVE_POLL_INTERVAL_MS
        : false,
  })
  const startMutation = useMutation({
    mutationFn: async () =>
      requireServerSuccess(await startLogResponseModelBackfill()),
    onSuccess: async () => {
      setConfirmOpen(false)
      toast.success(t('Response model backfill started.'))
      await queryClient.invalidateQueries({ queryKey: BACKFILL_QUERY_KEY })
    },
  })

  const status = statusQuery.data ?? null
  const task = status?.task ?? null
  const active = isActiveBackfill(task)
  const progress = Math.min(100, Math.max(0, task?.state?.progress ?? 0))
  const busy = active || startMutation.isPending

  return (
    <div
      role='group'
      aria-labelledby={titleId}
      className='flex min-w-0 flex-col gap-4'
    >
      <div>
        <h4 id={titleId} className='font-medium'>
          {t('Response model backfill')}
        </h4>
        <p className='text-muted-foreground mt-1 text-xs'>
          {t(
            'Fills the response model of logs written before the response model filters existed, so the usage logs can find them. It runs once after the upgrade. Run it again if some nodes were upgraded later and wrote logs in the meantime.'
          )}
        </p>
      </div>
      {active && (
        <div className='rounded-md border p-3'>
          <div className='mb-2 flex items-center justify-between gap-3 text-sm'>
            <span className='font-medium'>{t('Backfill progress')}</span>
            <span className='text-muted-foreground tabular-nums'>
              {progress}%
            </span>
          </div>
          <Progress value={progress} aria-label={t('Backfill progress')} />
          <div className='text-muted-foreground mt-2 text-xs'>
            {t('Filled {{filled}} logs so far.', {
              filled: formatNumber(task?.state?.filled ?? 0, locale),
            })}
          </div>
        </div>
      )}
      {status && !active && (
        <p
          className={cn(
            'text-sm',
            task?.status === 'failed'
              ? 'text-destructive'
              : 'text-muted-foreground'
          )}
        >
          {backfillSummary(status, t, locale)}
        </p>
      )}
      {status?.supported !== false && (
        <div>
          <Button
            type='button'
            variant='outline'
            onClick={() => setConfirmOpen(true)}
            disabled={busy}
          >
            {active ? t('Backfilling...') : t('Run backfill')}
          </Button>
        </div>
      )}
      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t('Run the response model backfill?')}
        desc={t(
          'It reads every log in the background and fills only the logs still missing a response model. On a large log table it takes a while and adds load to the database.'
        )}
        confirmText={t('Run backfill')}
        isLoading={startMutation.isPending}
        handleConfirm={() => startMutation.mutate()}
      />
    </div>
  )
}
