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
import { createContext, useContext } from 'react'

import { getUserGroups, getUserModels } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import { getApiKeyOwnerGroups, getApiKeyOwnerModels } from '../api'

// The user whose API keys the keys feature manages. null is the signed-in
// user; the root user sets it to manage another user's keys.
export type ApiKeyOwner = { id: number; username: string }

export const ApiKeyOwnerContext = createContext<ApiKeyOwner | null>(null)

export function useApiKeyOwner(): ApiKeyOwner | null {
  return useContext(ApiKeyOwnerContext)
}

// Groups the key owner may bind a key to.
export function apiKeyOwnerGroupsQuery(owner: ApiKeyOwner | null) {
  return {
    queryKey: owner ? ['api-key-owner', owner.id, 'groups'] : ['user-groups'],
    queryFn: async () =>
      requireServerSuccess(
        owner ? await getApiKeyOwnerGroups(owner.id) : await getUserGroups()
      ),
  }
}

// Models the key owner can call, for the model limit picker.
export function apiKeyOwnerModelsQuery(owner: ApiKeyOwner | null) {
  return {
    queryKey: owner ? ['api-key-owner', owner.id, 'models'] : ['user-models'],
    queryFn: async () =>
      requireServerSuccess(
        owner ? await getApiKeyOwnerModels(owner.id) : await getUserModels()
      ),
  }
}
