import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getUsage } from '../../api/client'
import { formatUSD, formatTokens } from '../../lib/format'

export function UsagePanel() {
  const [open, setOpen] = useState(false)
  const { data: usage } = useQuery({
    queryKey: ['usage'],
    queryFn: () => getUsage(),
    refetchInterval: open ? 5000 : false,
  })

  const totalCost = usage?.total?.cost_usd ?? 0
  const agents = usage?.agents ?? {}
  const slugs = Object.keys(agents).sort()

  return (
    <>
      <button
        className={`usage-toggle${open ? ' open' : ''}`}
        onClick={() => setOpen((v) => !v)}
      >
        <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <path d="m9 18 6-6-6-6" />
        </svg>
        Usage
        <span style={{ marginLeft: 'auto', fontWeight: 400, color: 'var(--accent)' }}>
          {formatUSD(totalCost)}
        </span>
      </button>
      {open && (
        <div className="usage-panel open">
          {slugs.length === 0 && totalCost === 0 ? (
            <p style={{ fontSize: 11, color: 'var(--text-tertiary)', padding: '4px 0' }}>
              No usage recorded yet.
            </p>
          ) : (
            <>
              <table className="usage-table">
                <thead>
                  <tr>
                    {['Agent', 'In', 'Out', 'Cache', 'Cost'].map((h) => (
                      <th key={h}>{h}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {slugs.map((slug) => {
                    const a = agents[slug]
                    return (
                      <tr key={slug}>
                        <td>{slug}</td>
                        <td>{formatTokens(a.input_tokens)}</td>
                        <td>{formatTokens(a.output_tokens)}</td>
                        <td>{formatTokens(a.cache_read_tokens)}</td>
                        <td>{formatUSD(a.cost_usd)}</td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
              <div className="usage-total">
                <span>Session: {formatTokens(usage?.session?.total_tokens ?? 0)} tokens</span>
                <span className="usage-total-cost">{formatUSD(totalCost)}</span>
              </div>
            </>
          )}
        </div>
      )}
    </>
  )
}
