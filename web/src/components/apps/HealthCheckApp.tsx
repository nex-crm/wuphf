import { useQuery } from '@tanstack/react-query'
import { getHealth } from '../../api/client'

export function HealthCheckApp() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['health'],
    queryFn: () => getHealth(),
    refetchInterval: 10_000,
  })

  if (isLoading) {
    return (
      <div style={{ padding: '40px 20px', textAlign: 'center', color: 'var(--text-tertiary)', fontSize: 14 }}>
        Checking health...
      </div>
    )
  }

  if (error) {
    return (
      <div style={{ padding: '40px 20px', textAlign: 'center', color: 'var(--text-tertiary)', fontSize: 14 }}>
        Could not reach health endpoint.
      </div>
    )
  }

  const status = data?.status ?? 'unknown'
  const agents = data?.agents ?? {}
  const agentSlugs = Object.keys(agents).sort()

  const isHealthy = status === 'ok' || status === 'healthy'

  return (
    <>
      <div style={{ padding: '0 0 12px', borderBottom: '1px solid var(--border)', marginBottom: 12 }}>
        <h3 style={{ fontSize: 16, fontWeight: 600, marginBottom: 4 }}>Health Check</h3>
      </div>

      {/* Overall status */}
      <div className="app-card" style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 12 }}>
        <span className={`status-dot ${isHealthy ? 'active' : ''}`} style={{ width: 10, height: 10 }} />
        <div>
          <div style={{ fontWeight: 600, fontSize: 14 }}>Broker Status</div>
          <div className="app-card-meta">
            <span className={isHealthy ? 'badge badge-green' : 'badge badge-yellow'}>
              {status.toUpperCase()}
            </span>
          </div>
        </div>
      </div>

      {/* Agent health */}
      {agentSlugs.length > 0 && (
        <>
          <div style={{
            fontSize: 11,
            fontWeight: 600,
            textTransform: 'uppercase',
            letterSpacing: '0.05em',
            color: 'var(--text-tertiary)',
            padding: '8px 0 6px',
          }}>
            Agents ({agentSlugs.length})
          </div>
          {agentSlugs.map((slug) => {
            const agentInfo = agents[slug] as Record<string, unknown> | undefined
            const agentStatus = agentInfo?.status as string | undefined
            const agentHealthy = agentStatus === 'ok' || agentStatus === 'healthy' || agentStatus === 'active'

            return (
              <div key={slug} className="app-card" style={{ marginBottom: 6, display: 'flex', alignItems: 'center', gap: 8 }}>
                <span className={`status-dot ${agentHealthy ? 'active' : ''}`} />
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div style={{ fontWeight: 500, fontSize: 13 }}>{slug}</div>
                  {agentStatus && (
                    <div className="app-card-meta">{agentStatus}</div>
                  )}
                </div>
              </div>
            )
          })}
        </>
      )}

      {agentSlugs.length === 0 && (
        <div style={{ padding: '20px 0', textAlign: 'center', color: 'var(--text-tertiary)', fontSize: 13 }}>
          No agent health data available.
        </div>
      )}
    </>
  )
}
