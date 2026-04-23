import { useQuery } from '@tanstack/react-query'
import { getConfig } from '../api/client'
import type { HarnessKind } from '../lib/harness'

const DEFAULT_HARNESS: HarnessKind = 'claude-code'

/**
 * Returns the install-wide default harness kind, used to render the avatar
 * badge for agents that have no explicit provider binding.
 */
export function useDefaultHarness(): HarnessKind {
  const { data } = useQuery({
    queryKey: ['config'],
    queryFn: getConfig,
    staleTime: 60_000,
  })
  const raw = data?.llm_provider
  if (raw === 'claude-code' || raw === 'codex' || raw === 'opencode') return raw
  return DEFAULT_HARNESS
}
