import { useQuery } from '@tanstack/react-query'
import { getChannels } from '../api/client'
import type { Channel } from '../api/client'

export function useChannels() {
  return useQuery({
    queryKey: ['channels'],
    queryFn: () => getChannels(),
    refetchInterval: 10000,
    select: (data) => data.channels ?? [],
  })
}

export type { Channel }
