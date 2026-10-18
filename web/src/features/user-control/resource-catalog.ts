import { useQuery } from '@tanstack/react-query'
import { createContext } from 'react'

import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

export interface CatalogChannel {
  id: number
  name: string
  type: number
  status: number
  models: string
  group: string
}
export const ResourceCatalogContext = createContext<{
  channels: CatalogChannel[]
  models: string[]
}>({ channels: [], models: [] })

interface CatalogData {
  channels: CatalogChannel[]
  models: string[]
  plans: { id: number; title: string }[]
}
export function useResourceCatalog(enabled = true) {
  return useQuery({
    queryKey: ['user-control', 'resource-catalog'],
    enabled,
    staleTime: 60_000,
    queryFn: async () => {
      const res = await api.get('/api/user-control/resource-catalog')
      requireServerSuccess(res.data)
      return res.data.data as CatalogData
    },
  })
}
