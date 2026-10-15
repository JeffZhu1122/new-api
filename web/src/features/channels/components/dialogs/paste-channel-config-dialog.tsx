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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'

import {
  channelConfigToFormValues,
  parseChannelConfig,
} from '../../lib/channel-config-transfer'
import type { ChannelFormValues } from '../../lib/channel-form'

type PasteChannelConfigDialogProps = {
  onPaste: (values: ChannelFormValues) => void
  onClose: () => void
}

/**
 * Takes a configuration copied with Copy Full Configuration and hands it to
 * the create form, where every field can be reviewed before saving. A text
 * box works on every site, while reading the clipboard needs HTTPS.
 */
export function PasteChannelConfigDialog(props: PasteChannelConfigDialogProps) {
  const { t } = useTranslation()
  const [text, setText] = useState('')
  const [invalid, setInvalid] = useState(false)

  const handleContinue = () => {
    const channel = parseChannelConfig(text)
    if (!channel) {
      setInvalid(true)
      return
    }
    props.onPaste(channelConfigToFormValues(channel))
  }

  return (
    <Dialog
      open
      onOpenChange={(open) => !open && props.onClose()}
      title={t('Paste Channel')}
      description={t(
        'Paste a configuration copied with Copy Full Configuration. You can review every field before the channel is created.'
      )}
      contentHeight='auto'
      bodyClassName='space-y-2 py-2'
      footer={
        <>
          <Button variant='outline' onClick={props.onClose}>
            {t('Cancel')}
          </Button>
          <Button onClick={handleContinue} disabled={!text.trim()}>
            {t('Continue')}
          </Button>
        </>
      }
    >
      <Label htmlFor='channel-config-text'>{t('Channel configuration')}</Label>
      <Textarea
        id='channel-config-text'
        value={text}
        onChange={(event) => {
          setText(event.target.value)
          setInvalid(false)
        }}
        rows={10}
        spellCheck={false}
        className='max-h-80 font-mono text-xs'
        aria-invalid={invalid}
        aria-describedby={invalid ? 'channel-config-text-error' : undefined}
      />
      {invalid && (
        <p
          id='channel-config-text-error'
          role='alert'
          className='text-destructive text-sm'
        >
          {t('This is not a copied channel configuration.')}
        </p>
      )}
    </Dialog>
  )
}
