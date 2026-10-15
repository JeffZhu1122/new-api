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
import { Loader2, ShieldCheck, TriangleAlert } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Dialog } from '@/components/dialog'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  SecureVerificationDialog,
  useSecureVerification,
} from '@/features/auth/secure-verification'
import { handleServerError } from '@/lib/handle-server-error'
import { AuthOperationError } from '@/lib/secure-verification'
import { createServerError } from '@/lib/server-error-message'

import { getChannelConfig } from '../../api'
import { CHANNEL_TYPE_CODEX } from '../../constants'
import { encodeChannelConfig } from '../../lib/channel-config-transfer'
import type { Channel } from '../../types'

type CopyChannelConfigDialogProps = {
  channel: Channel
  onClose: () => void
}

/**
 * Copies one channel's whole configuration, keys included, after 2FA. The
 * copy itself is a separate click so the clipboard write runs in a user
 * gesture once the configuration has loaded.
 */
export function CopyChannelConfigDialog(props: CopyChannelConfigDialogProps) {
  const { t } = useTranslation()
  const verification = useSecureVerification()
  const [configText, setConfigText] = useState<string | null>(null)
  const [isLoading, setIsLoading] = useState(false)
  const operation = useRef<AbortController | null>(null)

  useEffect(
    () => () => {
      operation.current?.abort()
      operation.current = null
    },
    []
  )

  const handleVerify = async () => {
    if (operation.current) return
    const current = new AbortController()
    operation.current = current
    try {
      const proof = await verification.requestVerification({
        scope: 'channel.config.copy',
        context: { channel_id: props.channel.id },
        title: t('Verify to copy channel configuration'),
        description: t(
          'Enter your 2FA code to copy the full configuration of this channel, keys included.'
        ),
      })
      if (!proof || operation.current !== current) return
      setIsLoading(true)
      const res = await getChannelConfig(
        props.channel.id,
        proof.proof_token,
        current.signal
      )
      if (operation.current !== current) return
      if (!res.success || !res.data) {
        throw createServerError(
          res,
          t('Failed to fetch the channel configuration')
        )
      }
      setConfigText(encodeChannelConfig(res.data))
    } catch (error) {
      if (operation.current === current && !current.signal.aborted) {
        handleServerError(AuthOperationError.from(error))
      }
    } finally {
      if (operation.current === current) {
        operation.current = null
        setIsLoading(false)
      }
    }
  }

  return (
    <>
      <Dialog
        open
        onOpenChange={(open) => !open && props.onClose()}
        title={t('Copy Full Configuration')}
        description={t(
          'Copy every setting of {{name}}, keys included, then paste it with Paste Channel on another site.',
          { name: props.channel.name }
        )}
        contentHeight='auto'
        bodyClassName='space-y-3 py-2'
        footer={
          <>
            <Button variant='outline' onClick={props.onClose}>
              {configText ? t('Close') : t('Cancel')}
            </Button>
            {configText ? (
              <CopyButton
                value={configText}
                variant='default'
                size='default'
                aria-label={t('Copy configuration')}
              >
                {t('Copy configuration')}
              </CopyButton>
            ) : (
              <Button onClick={handleVerify} disabled={isLoading}>
                {isLoading ? (
                  <Loader2 className='size-4 animate-spin' aria-hidden='true' />
                ) : (
                  <ShieldCheck className='size-4' aria-hidden='true' />
                )}
                {t('Verify with 2FA')}
              </Button>
            )}
          </>
        }
      >
        <Alert>
          <TriangleAlert aria-hidden='true' />
          <AlertDescription>
            {configText
              ? t(
                  'The configuration is ready. It contains the channel keys, so paste it only into a site you manage.'
                )
              : t(
                  'The copied configuration contains the channel keys. Two-factor authentication is required.'
                )}
          </AlertDescription>
        </Alert>
        {props.channel.type === CHANNEL_TYPE_CODEX && (
          <Alert variant='destructive'>
            <TriangleAlert aria-hidden='true' />
            <AlertDescription>
              {t(
                'Codex credentials change each time they are refreshed. If two sites use the same credential, one of them will be signed out.'
              )}
            </AlertDescription>
          </Alert>
        )}
      </Dialog>
      <SecureVerificationDialog {...verification.dialogProps} />
    </>
  )
}
