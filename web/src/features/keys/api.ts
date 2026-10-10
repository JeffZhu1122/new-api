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

import type {
  ApiKey,
  ApiResponse,
  GetApiKeysParams,
  GetApiKeysResponse,
  SearchApiKeysParams,
  ApiKeyFormData,
  TokenAutoGroupsConfig,
  UserGroupModels,
} from './types'

// ============================================================================
// API Key Management
// ============================================================================

// Every call takes an optional ownerId: the root user manages another user's
// keys under /api/user/:id/tokens; without it the signed-in user's own
// /api/token routes are used.
function ownerTokensPath(ownerId: number) {
  return `/api/user/${ownerId}/tokens`
}

// Get paginated API keys list
export async function getApiKeys(
  params: GetApiKeysParams = {},
  ownerId?: number
): Promise<GetApiKeysResponse> {
  const { p = 1, size = 10 } = params
  const base = ownerId ? ownerTokensPath(ownerId) : '/api/token/'
  const res = await api.get(`${base}?p=${p}&size=${size}`)
  return res.data
}

// Search API keys by keyword or token (with pagination)
export async function searchApiKeys(
  params: SearchApiKeysParams,
  ownerId?: number
): Promise<GetApiKeysResponse> {
  const { keyword = '', token = '', p, size } = params
  const queryParams = new URLSearchParams()
  if (keyword) queryParams.set('keyword', keyword)
  if (token) queryParams.set('token', token)
  if (p != null) queryParams.set('p', String(p))
  if (size != null) queryParams.set('size', String(size))
  const path = ownerId ? ownerTokensPath(ownerId) : '/api/token/search'
  const res = await api.get(`${path}?${queryParams.toString()}`)
  return res.data
}

// Groups the user may select and the models each one can call
export async function getUserGroupModels(): Promise<
  ApiResponse<UserGroupModels[]>
> {
  const res = await api.get('/api/user/self/group-models')
  return res.data
}

// Get single API key by ID
export async function getApiKey(
  id: number,
  ownerId?: number
): Promise<ApiResponse<ApiKey>> {
  const path = ownerId
    ? `${ownerTokensPath(ownerId)}/${id}`
    : `/api/token/${id}`
  const res = await api.get(path)
  return res.data
}

// Get the key owner's global Auto order and the per-token selection limit.
export async function getTokenAutoGroups(
  ownerId?: number
): Promise<ApiResponse<TokenAutoGroupsConfig>> {
  const path = ownerId
    ? `${ownerTokensPath(ownerId)}/auto-groups`
    : '/api/token/auto-groups'
  const res = await api.get(path)
  return res.data
}

// Groups another user may bind a key to, shaped like /api/user/self/groups
export async function getApiKeyOwnerGroups(
  ownerId: number
): Promise<
  ApiResponse<Record<string, { desc: string; ratio: number | string }>>
> {
  const res = await api.get(`${ownerTokensPath(ownerId)}/groups`)
  return res.data
}

// Models another user can call, shaped like /api/user/models
export async function getApiKeyOwnerModels(
  ownerId: number
): Promise<ApiResponse<string[]>> {
  const res = await api.get(`${ownerTokensPath(ownerId)}/models`)
  return res.data
}

// Create a new API key
export async function createApiKey(
  data: ApiKeyFormData,
  ownerId?: number
): Promise<ApiResponse<ApiKey>> {
  const res = await api.post(
    ownerId ? ownerTokensPath(ownerId) : '/api/token/',
    data
  )
  return res.data
}

// Update an existing API key
export async function updateApiKey(
  data: ApiKeyFormData & { id: number },
  ownerId?: number
): Promise<ApiResponse<ApiKey>> {
  const res = await api.put(
    ownerId ? ownerTokensPath(ownerId) : '/api/token/',
    data
  )
  return res.data
}

// Delete a single API key
export async function deleteApiKey(
  id: number,
  ownerId?: number
): Promise<ApiResponse> {
  const path = ownerId
    ? `${ownerTokensPath(ownerId)}/${id}`
    : `/api/token/${id}/`
  const res = await api.delete(path)
  return res.data
}

// Batch delete multiple API keys
export async function batchDeleteApiKeys(
  ids: number[]
): Promise<ApiResponse<number>> {
  const res = await api.post('/api/token/batch', { ids })
  return res.data
}

// Update API key status (enable/disable)
export async function updateApiKeyStatus(
  id: number,
  status: number,
  ownerId?: number
): Promise<ApiResponse<ApiKey>> {
  const base = ownerId ? ownerTokensPath(ownerId) : '/api/token/'
  const res = await api.put(`${base}?status_only=true`, { id, status })
  return res.data
}

// Fetch the real (unmasked) key for a token by ID. Another user's key needs an
// admin.user.token.read proof bound to that user and key.
export async function fetchTokenKey(
  id: number,
  owner?: { ownerId: number; proofToken: string }
): Promise<{ success: boolean; message?: string; data?: { key: string } }> {
  if (owner) {
    const res = await api.post(
      `${ownerTokensPath(owner.ownerId)}/${id}/key`,
      undefined,
      {
        headers: { 'X-Security-Proof': owner.proofToken },
        // A proof is single-use: never let the auth refresh replay it.
        singleUseAuthorization: true,
      }
    )
    return res.data
  }
  const res = await api.post(`/api/token/${id}/key`)
  return res.data
}

// Batch fetch real (unmasked) keys for multiple tokens
export async function fetchTokenKeysBatch(ids: number[]): Promise<{
  success: boolean
  message?: string
  data?: { keys: Record<number, string> }
}> {
  const res = await api.post('/api/token/batch/keys', { ids })
  return res.data
}
