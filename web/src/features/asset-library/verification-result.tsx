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
import { CircleCheck, CircleX } from 'lucide-react'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'

import { reportVerificationResult } from './api'

// resultCode of a person who passed the liveness check.
const PASSED = '10000'

// The page the H5 verification page returns to, on the phone of the person
// being verified. It passes the result on to the gateway, which then records
// the group without waiting for its next check, and says what to do next.
export function VerificationResult(props: {
  bytedToken: string
  resultCode: string
}) {
  const { t } = useTranslation()
  const passed = props.resultCode === PASSED

  useEffect(() => {
    if (!props.bytedToken) return
    // The page shows the result either way; the gateway's own checks
    // record the group even if this report never arrives.
    reportVerificationResult({
      byted_token: props.bytedToken,
      result_code: props.resultCode,
    }).catch(() => undefined)
  }, [props.bytedToken, props.resultCode])

  let detail: string | undefined
  if (!passed && props.resultCode) {
    detail = t('Result code: {{code}}', { code: props.resultCode })
  }

  return (
    <main className='flex min-h-svh items-center justify-center p-4'>
      <EmptyState
        icon={passed ? CircleCheck : CircleX}
        title={
          passed
            ? t('Face verification passed')
            : t('Face verification did not pass')
        }
        description={
          passed
            ? t(
                'Go back to the Asset Library page on your computer; the real-person group appears there shortly.'
              )
            : t(
                'Start a new verification on the Asset Library page on your computer.'
              )
        }
        action={
          detail && <p className='text-muted-foreground text-xs'>{detail}</p>
        }
        className='max-w-md'
      />
    </main>
  )
}
