import { useQuery } from '@tanstack/react-query'
import { getOfficeMembers, getMembers } from '../api/client'
import type { OfficeMember } from '../api/client'

export function useOfficeMembers() {
  return useQuery({
    queryKey: ['office-members'],
    queryFn: () => getOfficeMembers(),
    refetchInterval: 5000,
    select: (data) => data.members ?? [],
  })
}

export function useChannelMembers(channel: string) {
  return useQuery({
    queryKey: ['channel-members', channel],
    queryFn: () => getMembers(channel),
    refetchInterval: 5000,
    select: (data) => data.members ?? [],
  })
}

export type { OfficeMember }
