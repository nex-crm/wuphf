import { useQuery } from '@tanstack/react-query'
import { useAppStore } from '../../stores/app'
import { useOfficeMembers } from '../../hooks/useMembers'
import { getHealth } from '../../api/client'

interface HealthSnapshot {
  status: string
  provider?: string
  agents?: Record<string, unknown>
}

/**
 * Bottom status bar mirroring the legacy IIFE: shows the active channel/app,
 * mode (office vs 1:1), agent count, broker connection, and runtime provider.
 */
export function StatusBar() {
  const currentChannel = useAppStore((s) => s.currentChannel)
  const currentApp = useAppStore((s) => s.currentApp)
  const dmMode = useAppStore((s) => s.dmMode)
  const dmAgentSlug = useAppStore((s) => s.dmAgentSlug)
  const brokerConnected = useAppStore((s) => s.brokerConnected)
  const { data: members = [] } = useOfficeMembers()

  const { data: health } = useQuery<HealthSnapshot>({
    queryKey: ['health'],
    queryFn: () => getHealth() as Promise<HealthSnapshot>,
    refetchInterval: 15_000,
    enabled: brokerConnected,
  })

  const agentCount = members.filter(
    (m) => m.slug && m.slug !== 'human' && m.slug !== 'you' && m.slug !== 'system',
  ).length

  const channelLabel = currentApp
    ? currentApp
    : dmMode && dmAgentSlug
      ? `@${dmAgentSlug}`
      : `# ${currentChannel}`
  const modeLabel = dmMode ? '1:1' : 'office'
  const provider = health?.provider

  return (
    <div className="status-bar">
      <span className="status-bar-item">{channelLabel}</span>
      <span className="status-bar-item">{modeLabel}</span>
      <span className="status-bar-spacer" />
      <span className="status-bar-item">{agentCount} agent{agentCount === 1 ? '' : 's'}</span>
      {provider && (
        <span className="status-bar-item" title={`Runtime provider: ${provider}`}>
          {'\u2699 '}{provider}
        </span>
      )}
      <span
        className={`status-bar-item status-bar-conn${brokerConnected ? '' : ' disconnected'}`}
      >
        {brokerConnected ? 'connected' : 'disconnected'}
      </span>
    </div>
  )
}
