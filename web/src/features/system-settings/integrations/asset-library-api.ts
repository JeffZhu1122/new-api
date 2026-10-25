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
import { api } from '@/lib/api'
import {
  getServerMessage,
  requireServerSuccess,
} from '@/lib/server-error-message'

// Volcengine accounts whose Ark private asset libraries are shared with
// users (controller/asset_library_account.go). The Secret Access Key is
// write-only: the list returns has_secret and a masked Access Key ID.

export type AssetLibraryAccount = {
  id: number
  name: string
  access_key_id_masked: string
  has_secret: boolean
  region: string
  project_name: string
  endpoint: string
  enabled: boolean
  aigc_enabled: boolean
  liveness_face_enabled: boolean
  allowed_user_groups: string[]
  priority: number
  max_groups: number
  max_assets: number
  create_asset_qpm: number
  created_time: number
  updated_time: number
}

// On update, an empty access_key_id or secret_access_key keeps the stored
// value.
export type AssetLibraryAccountPayload = {
  id?: number
  name: string
  access_key_id: string
  secret_access_key: string
  region: string
  project_name: string
  endpoint: string
  enabled: boolean
  aigc_enabled: boolean
  liveness_face_enabled: boolean
  allowed_user_groups: string[]
  priority: number
  max_groups: number
  max_assets: number
  create_asset_qpm: number
}

type ApiBody<T> = { success: boolean; message?: string; data?: T }

const ACCOUNTS_PATH = '/api/asset-library/accounts'

export const assetLibraryAccountsQueryKey = ['asset-library', 'accounts']

export async function listAssetLibraryAccounts(): Promise<
  AssetLibraryAccount[]
> {
  const res = await api.get<ApiBody<AssetLibraryAccount[]>>(ACCOUNTS_PATH)
  return requireServerSuccess(res.data).data ?? []
}

export async function saveAssetLibraryAccount(
  payload: AssetLibraryAccountPayload
): Promise<AssetLibraryAccount> {
  const res = payload.id
    ? await api.put<ApiBody<AssetLibraryAccount>>(ACCOUNTS_PATH, payload)
    : await api.post<ApiBody<AssetLibraryAccount>>(ACCOUNTS_PATH, payload)
  return requireServerSuccess(res.data).data as AssetLibraryAccount
}

export async function deleteAssetLibraryAccount(id: number): Promise<void> {
  const res = await api.delete<ApiBody<null>>(`${ACCOUNTS_PATH}/${id}`)
  requireServerSuccess(res.data)
}

export async function listUserGroups(): Promise<string[]> {
  const res = await api.get<ApiBody<string[]>>('/api/group/')
  return requireServerSuccess(res.data).data ?? []
}

// Tests the account in the dialog, saved or not; returns the translated
// server message.
export async function testAssetLibraryAccount(
  payload: AssetLibraryAccountPayload
): Promise<string> {
  const res = await api.post<ApiBody<null>>(`${ACCOUNTS_PATH}/test`, payload)
  return getServerMessage(requireServerSuccess(res.data)) ?? ''
}
