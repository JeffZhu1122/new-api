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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { StaticDataTable } from '@/components/data-table/static/static-data-table'
import { StaticRowActions } from '@/components/data-table/static/static-row-actions'
import { Button } from '@/components/ui/button'
import { getFingerprintProfileOptions } from '@/features/channels/api'
import { channelsQueryKeys } from '@/features/channels/lib/channel-actions'

import { useUpdateOption } from '../hooks/use-update-option'
import { FingerprintProfileDialog } from './fingerprint-profile-dialog'
import {
  type FingerprintProfile,
  MAX_FINGERPRINT_PROFILES,
} from './fingerprint-profile-schema'

type FingerprintProfileListProps = {
  profiles: FingerprintProfile[]
}

type ProfileDialogState = {
  // Counts openings, so each one starts from a fresh form.
  session: number
  open: boolean
  // The name of the profile being edited; null adds a new one.
  editingName: string | null
}

export function FingerprintProfileList(props: FingerprintProfileListProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const updateOption = useUpdateOption()
  const [dialog, setDialog] = useState<ProfileDialogState>({
    session: 0,
    open: false,
    editingName: null,
  })
  const [deletingName, setDeletingName] = useState<string | null>(null)
  const optionsQuery = useQuery({
    queryKey: channelsQueryKeys.fingerprintProfiles(),
    queryFn: getFingerprintProfileOptions,
  })

  const editing =
    props.profiles.find((profile) => profile.name === dialog.editingName) ??
    null

  // The option holds the whole list, so every change stores the list again.
  // The gateway refuses a list that drops a profile a channel still selects.
  const saveProfiles = (next: FingerprintProfile[], onSaved: () => void) => {
    updateOption.mutate(
      { key: 'fingerprint_setting.profiles', value: JSON.stringify(next) },
      {
        onSuccess: () => {
          onSaved()
          queryClient.invalidateQueries({
            queryKey: channelsQueryKeys.fingerprintProfiles(),
          })
        },
      }
    )
  }

  const openDialog = (editingName: string | null) => {
    setDialog((current) => ({
      session: current.session + 1,
      open: true,
      editingName,
    }))
  }
  const setDialogOpen = (open: boolean) => {
    setDialog((current) => ({ ...current, open }))
  }

  const saveProfile = (profile: FingerprintProfile) => {
    const next =
      editing === null
        ? [...props.profiles, profile]
        : props.profiles.map((stored) =>
            stored.name === editing.name ? profile : stored
          )
    saveProfiles(next, () => setDialogOpen(false))
  }

  const tlsLabel = (profile: FingerprintProfile): string => {
    if (profile.tls_preset) return profile.tls_preset
    if (profile.tls_client_hello) return t('Custom capture')
    return t('Go default')
  }

  return (
    <div className='flex flex-col gap-3'>
      <div className='flex items-center justify-between gap-3'>
        <div className='min-w-0'>
          <h4 className='text-sm font-medium'>{t('Fingerprint profiles')}</h4>
          <p className='text-muted-foreground text-xs'>
            {t(
              'A profile sets the TLS ClientHello, the HTTP/2 parameters, the header order and the default User-Agent of outbound requests. It only applies to channels that select it.'
            )}
          </p>
        </div>
        <Button
          size='sm'
          disabled={props.profiles.length >= MAX_FINGERPRINT_PROFILES}
          onClick={() => openDialog(null)}
        >
          <Plus className='size-4' />
          {t('Add profile')}
        </Button>
      </div>
      <StaticDataTable
        data={props.profiles}
        getRowKey={(profile) => profile.name}
        emptyContent={t('No fingerprint profiles yet')}
        columns={[
          {
            id: 'name',
            header: t('Name'),
            cell: (profile) => (
              <span className='font-medium'>{profile.name}</span>
            ),
          },
          {
            id: 'tls',
            header: t('TLS ClientHello'),
            cell: tlsLabel,
          },
          {
            id: 'http2',
            header: t('HTTP/2 fingerprint'),
            cellClassName: 'max-w-64',
            cell: (profile) =>
              profile.http2_fingerprint ? (
                <span
                  className='block truncate font-mono text-xs'
                  title={profile.http2_fingerprint}
                >
                  {profile.http2_fingerprint}
                </span>
              ) : (
                t('Go default')
              ),
          },
          {
            id: 'header_order',
            header: t('Ordered headers'),
            cell: (profile) => profile.header_order?.length ?? 0,
          },
          {
            id: 'user_agent',
            header: t('Default User-Agent'),
            cellClassName: 'max-w-64',
            cell: (profile) =>
              profile.user_agent ? (
                <span className='block truncate' title={profile.user_agent}>
                  {profile.user_agent}
                </span>
              ) : (
                '-'
              ),
          },
          {
            id: 'actions',
            header: <span className='sr-only'>{t('Actions')}</span>,
            cell: (profile) => (
              <StaticRowActions
                editLabel={t('Edit')}
                deleteLabel={t('Delete')}
                menuLabel={t('Open menu')}
                onEdit={() => openDialog(profile.name)}
                onDelete={() => setDeletingName(profile.name)}
              />
            ),
          },
        ]}
      />
      <FingerprintProfileDialog
        key={dialog.session}
        open={dialog.open}
        onOpenChange={setDialogOpen}
        profile={editing}
        takenNames={props.profiles
          .filter((profile) => profile.name !== editing?.name)
          .map((profile) => profile.name)}
        tlsPresets={optionsQuery.data?.tls_presets ?? []}
        isSaving={updateOption.isPending}
        onSave={saveProfile}
      />
      <ConfirmDialog
        open={deletingName !== null}
        onOpenChange={(open) => !open && setDeletingName(null)}
        title={t('Delete fingerprint profile')}
        desc={t(
          'A profile that a channel still selects cannot be deleted. Clear it on that channel first.'
        )}
        destructive
        isLoading={updateOption.isPending}
        handleConfirm={() =>
          saveProfiles(
            props.profiles.filter((profile) => profile.name !== deletingName),
            () => setDeletingName(null)
          )
        }
      />
    </div>
  )
}
