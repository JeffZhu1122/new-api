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
import { useQuery } from '@tanstack/react-query'

import { requireServerSuccess } from '@/lib/server-error-message'

import { getUserGroupModels } from '../api'

export const USER_GROUP_MODELS_QUERY_KEY = ['user-group-models']

// Groups the user may select with the models each can call, shared by the
// Groups & Models sheet and the hint under the key group picker.
export function useUserGroupModels() {
  return useQuery({
    queryKey: USER_GROUP_MODELS_QUERY_KEY,
    queryFn: async () =>
      requireServerSuccess(await getUserGroupModels()).data ?? [],
    staleTime: 60_000,
  })
}
