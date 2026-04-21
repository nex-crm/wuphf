import { useQuery } from '@tanstack/react-query'
import { getAllRequests, type AgentRequest } from '../api/client'

export interface RequestsState {
  all: AgentRequest[]
  pending: AgentRequest[]
  blockingPending: AgentRequest | null
}

const REQUEST_REFETCH_MS = 5_000

// Global view of requests across every channel the human can access. The
// broker rejects new messages with 409 whenever ANY blocking request is
// pending, so the overlay + inline interview bar must reflect that same
// cross-channel state — otherwise the human sees "nothing blocking" here
// while sending stays blocked by a request in a channel they aren't viewing.
export function useRequests(): RequestsState {
  const { data } = useQuery({
    queryKey: ['requests', 'all'],
    queryFn: () => getAllRequests(),
    refetchInterval: REQUEST_REFETCH_MS,
  })

  const all = data?.requests ?? []
  const pending = all.filter((r) => !r.status || r.status === 'open' || r.status === 'pending')
  const blockingPending = pending.find((r) => r.blocking) ?? null

  return { all, pending, blockingPending }
}
