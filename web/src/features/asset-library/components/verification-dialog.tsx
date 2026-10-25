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
import { Loader2 } from 'lucide-react'
import { QRCodeSVG } from 'qrcode.react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { CopyButton } from '@/components/copy-button'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { handleServerError } from '@/lib/handle-server-error'
import { translateServerText } from '@/lib/server-error-message'

import {
  type VerificationSession,
  checkVerification,
  createVerification,
} from '../api'
import {
  VERIFICATION_CALLBACK_PATH,
  localizedVerificationLink,
} from '../lib/verification-link'
import { ComplianceAgreement } from './asset-dialogs'

const NAME_MAX = 64
const DESCRIPTION_MAX = 300
const POLL_INTERVAL_MS = 3000
// Starts a face verification for a new real-person group and waits for the
// person to pass it on their phone.
export function VerificationDialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  complianceNotice: string
  sessionsPerHour: number
}) {
  const { t, i18n } = useTranslation()
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [agreed, setAgreed] = useState(false)
  const [session, setSession] = useState<VerificationSession | null>(null)
  const needsAgreement = props.complianceNotice.trim() !== ''

  useEffect(() => {
    if (!props.open) return
    setName('')
    setDescription('')
    setAgreed(false)
    setSession(null)
  }, [props.open])

  const startMutation = useMutation({
    mutationFn: () =>
      createVerification({
        CallbackURL: `${window.location.origin}${VERIFICATION_CALLBACK_PATH}`,
        Name: name.trim(),
        Description: description,
      }),
    onSuccess: (result) => setSession(result),
    onError: (error) => handleServerError(error),
  })
  const resultQuery = useQuery({
    queryKey: ['asset-library', 'verification', session?.BytedToken],
    queryFn: () => checkVerification(session?.BytedToken ?? ''),
    enabled: props.open && session !== null,
    refetchInterval: (query) =>
      query.state.data?.status === 'waiting' ? POLL_INTERVAL_MS : false,
    meta: { errorToast: false },
  })
  const state = resultQuery.data
  // The token whose success was announced, so it is announced once.
  const announced = useRef('')

  useEffect(() => {
    if (state?.status !== 'passed' || !session) return
    if (announced.current === session.BytedToken) return
    announced.current = session.BytedToken
    toast.success(
      t('Face verification passed. The real-person group is ready.')
    )
    void queryClient.invalidateQueries({ queryKey: ['asset-library'] })
    props.onOpenChange(false)
  }, [state, session, queryClient, t, props])

  const canStart =
    name.trim() !== '' &&
    name.length <= NAME_MAX &&
    description.length <= DESCRIPTION_MAX &&
    (!needsAgreement || agreed)
  const failed = state?.status === 'failed'
  const link = session
    ? localizedVerificationLink(
        session.H5Link,
        i18n.resolvedLanguage || i18n.language
      )
    : ''

  let footer = (
    <>
      <Button variant='outline' onClick={() => props.onOpenChange(false)}>
        {t('Cancel')}
      </Button>
      <Button
        disabled={!canStart || startMutation.isPending}
        onClick={() => startMutation.mutate()}
      >
        {t('Get verification link')}
      </Button>
    </>
  )
  if (session) {
    footer = (
      <>
        <Button variant='outline' onClick={() => props.onOpenChange(false)}>
          {t('Close')}
        </Button>
        {failed && (
          <Button onClick={() => setSession(null)}>
            {t('Start a new verification')}
          </Button>
        )}
      </>
    )
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Start face verification')}
      description={t(
        'The real-person group is created once the person passes a liveness check on their phone.'
      )}
      contentClassName='sm:max-w-[520px]'
      bodyClassName='space-y-4'
      footer={footer}
    >
      {!session && (
        <>
          <div className='space-y-2'>
            <Label htmlFor='verification-name'>{t('Asset group name')}</Label>
            <Input
              id='verification-name'
              value={name}
              maxLength={NAME_MAX}
              onChange={(event) => setName(event.target.value)}
            />
          </div>
          <div className='space-y-2'>
            <Label htmlFor='verification-description'>{t('Description')}</Label>
            <Textarea
              id='verification-description'
              rows={3}
              value={description}
              maxLength={DESCRIPTION_MAX}
              onChange={(event) => setDescription(event.target.value)}
            />
          </div>
          {props.sessionsPerHour > 0 && (
            <p className='text-muted-foreground text-xs'>
              {t('You can start {{count}} verifications per hour.', {
                count: props.sessionsPerHour,
              })}
            </p>
          )}
          {needsAgreement && (
            <ComplianceAgreement
              id='verification-agree'
              notice={props.complianceNotice}
              agreed={agreed}
              onAgreedChange={setAgreed}
            />
          )}
        </>
      )}
      {session && !failed && (
        <div className='flex flex-col items-center gap-3 text-center'>
          <div
            className='rounded-md bg-white p-3'
            data-testid='verification-qr'
          >
            <QRCodeSVG value={link} size={200} />
          </div>
          <p className='text-sm'>
            {t(
              'Scan the code with the phone of the person being verified, or send them the link. It works once and expires in 30 minutes.'
            )}
          </p>
          <CopyButton
            value={link}
            variant='outline'
            size='sm'
            aria-label={t('Copy verification link')}
          >
            {t('Copy verification link')}
          </CopyButton>
          <p className='text-muted-foreground flex items-center gap-2 text-sm'>
            <Loader2 className='size-4 animate-spin' aria-hidden='true' />
            {t('Waiting for the verification…')}
          </p>
          <p className='text-muted-foreground text-xs'>
            {t(
              'You can close this window; the group appears in the list once the person passes.'
            )}
          </p>
        </div>
      )}
      {state?.status === 'failed' && (
        <p className='text-destructive text-sm' role='alert'>
          {translateServerText(t, state.reason)}
        </p>
      )}
    </Dialog>
  )
}
