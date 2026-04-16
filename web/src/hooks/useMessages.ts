import { useQuery } from '@tanstack/react-query'
import { getMessages, getThreadMessages } from '../api/client'
import type { Message } from '../api/client'

export function useMessages(channel: string, sinceId?: string | null) {
  return useQuery({
    queryKey: ['messages', channel, sinceId],
    queryFn: () => getMessages(channel, sinceId),
    refetchInterval: 2000,
    select: (data) => data.messages ?? [],
  })
}

export function useThreadMessages(channel: string, threadId: string | null) {
  return useQuery({
    queryKey: ['thread-messages', channel, threadId],
    queryFn: () => getThreadMessages(channel, threadId!),
    enabled: !!threadId,
    refetchInterval: 3000,
    select: (data) => data.messages ?? [],
  })
}

export type { Message }
