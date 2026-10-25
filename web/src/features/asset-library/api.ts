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
import { requireServerSuccess } from '@/lib/server-error-message'

// The page talks to the same Ark-compatible actions API users call at /volc
// (service/asset_library_openapi.go), through the signed-in session.

export type AssetGroupType = 'AIGC' | 'LivenessFace'
export type AssetType = 'Image' | 'Video' | 'Audio'
export type AssetStatus = 'Processing' | 'Active' | 'Failed'

export type AssetGroup = {
  Id: string
  Name: string
  Description: string
  GroupType: AssetGroupType
  ProjectName?: string
  CreateTime: string
  UpdateTime?: string
}

export type Asset = {
  Id: string
  Name: string
  AssetType: AssetType
  GroupId: string
  Status: AssetStatus
  URL: string
  CreateTime: string
  UpdateTime?: string
  LastInferenceTime?: string
  Error?: { Code: string; Message: string }
}

export type AssetLibraryConfig = {
  allowed: boolean
  compliance_notice: string
  max_groups_per_user: number
  max_assets_per_user: number
  group_count: number
  asset_count: number
  group_types: AssetGroupType[]
  // Real-person groups are created through face verification only.
  liveness_face_verify: boolean
  verify_sessions_per_hour: number
}

export type AdminAssetGroup = AssetGroup & {
  UserId: number
  Username: string
  AccountName: string
  AssetCount: number
}

type ArkEnvelope<T> = {
  ResponseMetadata?: { Error?: { Code: string; Message: string } }
  Result?: T
}

// An error the asset library or Ark returned, with Ark's error code.
export class AssetLibraryError extends Error {
  code: string
  // The message without the code; the gateway's own are locale keys.
  reason: string

  constructor(code: string, message: string) {
    super(code ? `${code}: ${message}` : message)
    this.code = code
    this.reason = message
  }
}

async function callAction<T>(action: string, body: object): Promise<T> {
  const res = await api.post<ArkEnvelope<T>>(
    `/api/asset-library/openapi?Action=${action}&Version=2024-01-01`,
    body,
    { validateStatus: () => true }
  )
  const apiError = res.data?.ResponseMetadata?.Error
  if (apiError) throw new AssetLibraryError(apiError.Code, apiError.Message)
  if (res.status >= 400 || !res.data?.Result) {
    throw new AssetLibraryError('', `HTTP ${res.status}`)
  }
  return res.data.Result
}

export async function getAssetLibraryConfig(): Promise<AssetLibraryConfig> {
  const res = await api.get<{
    success: boolean
    data: AssetLibraryConfig
  }>('/api/asset-library/config')
  return requireServerSuccess(res.data).data
}

export async function listAssetGroups(
  groupType: AssetGroupType
): Promise<AssetGroup[]> {
  const groups: AssetGroup[] = []
  let nextToken: string | undefined
  // Users hold few groups (per-user limits), so the page reads them all.
  do {
    const result = await callAction<{
      Items: AssetGroup[]
      NextToken?: string
    }>('ListAssetGroups', {
      Filter: { GroupType: groupType },
      MaxResults: 100,
      ...(nextToken ? { NextToken: nextToken } : {}),
    })
    groups.push(...result.Items)
    nextToken = result.NextToken
  } while (nextToken)
  return groups
}

export async function listGroupAssets(group: AssetGroup): Promise<Asset[]> {
  const assets: Asset[] = []
  let nextToken: string | undefined
  do {
    const result = await callAction<{ Items: Asset[]; NextToken?: string }>(
      'ListAssets',
      {
        Filter: { GroupType: group.GroupType, GroupIds: [group.Id] },
        MaxResults: 100,
        ...(nextToken ? { NextToken: nextToken } : {}),
      }
    )
    assets.push(...result.Items)
    nextToken = result.NextToken
  } while (nextToken)
  return assets
}

export function createAssetGroup(body: {
  Name: string
  Description: string
  GroupType: AssetGroupType
}): Promise<{ Id: string }> {
  return callAction('CreateAssetGroup', body)
}

export function updateAssetGroup(body: {
  Id: string
  Name: string
  Description: string
}): Promise<unknown> {
  return callAction('UpdateAssetGroup', body)
}

export function deleteAssetGroup(id: string): Promise<unknown> {
  return callAction('DeleteAssetGroup', { Id: id })
}

export function createAsset(body: {
  GroupId: string
  URL: string
  AssetType: AssetType
  Name: string
}): Promise<{ Id: string }> {
  return callAction('CreateAsset', body)
}

export function updateAsset(body: { Id: string; Name: string }) {
  return callAction('UpdateAsset', body)
}

export function deleteAsset(id: string): Promise<unknown> {
  return callAction('DeleteAsset', { Id: id })
}

// Face verification: a real-person group is created once the person passes
// the check on the H5 page Ark links to.

export type VerificationSession = { BytedToken: string; H5Link: string }

export function createVerification(body: {
  CallbackURL: string
  Name: string
  Description: string
}): Promise<VerificationSession> {
  return callAction('CreateVisualValidateSession', body)
}

export type VerificationState =
  | { status: 'passed'; groupId: string }
  | { status: 'failed'; reason: string }
  | { status: 'waiting' }

// checkVerification asks for the group; Ark answers with an error until the
// person has passed, so only the gateway's VerificationFailed ends the wait.
export async function checkVerification(
  bytedToken: string
): Promise<VerificationState> {
  try {
    const result = await callAction<{ GroupId: string }>(
      'GetVisualValidateResult',
      { BytedToken: bytedToken }
    )
    return { status: 'passed', groupId: result.GroupId }
  } catch (error) {
    if (
      error instanceof AssetLibraryError &&
      error.code === 'VerificationFailed'
    ) {
      return { status: 'failed', reason: error.reason }
    }
    return { status: 'waiting' }
  }
}

// reportVerificationResult passes on what the H5 page appended to the
// callback URL; the phone that opens it is not signed in.
export async function reportVerificationResult(body: {
  byted_token: string
  result_code: string
}): Promise<void> {
  await api.post('/api/asset-library/verification/callback', body)
}

// Administrators: every user's library.

export async function adminListAssetGroups(params: {
  p: number
  page_size: number
  user_id?: number
  group_type?: AssetGroupType
  keyword?: string
}): Promise<{ items: AdminAssetGroup[]; total: number }> {
  const res = await api.get<{
    success: boolean
    data: { items: AdminAssetGroup[]; total: number }
  }>('/api/asset-library/admin/groups', { params })
  return requireServerSuccess(res.data).data
}

export async function adminListGroupAssets(groupId: string): Promise<Asset[]> {
  const res = await api.get<{ success: boolean; data: Asset[] }>(
    `/api/asset-library/admin/groups/${encodeURIComponent(groupId)}/assets`
  )
  return requireServerSuccess(res.data).data ?? []
}

export async function adminDeleteAssetGroup(id: string): Promise<void> {
  const res = await api.delete<{ success: boolean }>(
    `/api/asset-library/admin/groups/${encodeURIComponent(id)}`
  )
  requireServerSuccess(res.data)
}

export async function adminDeleteAsset(id: string): Promise<void> {
  const res = await api.delete<{ success: boolean }>(
    `/api/asset-library/admin/assets/${encodeURIComponent(id)}`
  )
  requireServerSuccess(res.data)
}
