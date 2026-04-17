import { useQuery } from '@tanstack/react-query'
import { getRequests, type AgentRequest } from '../api/client'
import { useAppStore } from '../stores/app'

export interface RequestsState {
  all: AgentRequest[]
  pending: AgentRequest[]
  blockingPending: AgentRequest | null
}

const REQUEST_REFETCH_MS = 5_000

export function useRequests(): RequestsState {
  const currentChannel = useAppStore((s) => s.currentChannel)
  const { data } = useQuery({
    queryKey: ['requests', currentChannel],
    queryFn: () => getRequests(currentChannel),
    refetchInterval: REQUEST_REFETCH_MS,
  })

  const all = data?.requests ?? []
  const pending = all.filter((r) => !r.status || r.status === 'open' || r.status === 'pending')
  const blockingPending = pending.find((r) => r.blocking) ?? null

  return { all, pending, blockingPending }
}
