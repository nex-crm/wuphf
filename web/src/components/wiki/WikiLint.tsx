import { useCallback, useEffect, useState } from 'react'
import { runLint, resolveContradiction, type LintFinding, type LintReport } from '../../api/wiki'
import ResolveContradictionModal from './ResolveContradictionModal'

/**
 * WikiLint — the /wiki/lint surface.
 *
 * Displays the most recent lint report findings. Each finding shows:
 *   - Severity label (text + aria-label — never color alone per §9.3)
 *   - Type + entity slug as a wikilink
 *   - Summary
 *   - For contradictions: Resolve button that opens ResolveContradictionModal
 *
 * Mirrors WikiAudit.tsx in layout and data-loading pattern.
 */
interface WikiLintProps {
  onNavigate: (path: string | null) => void
}

export default function WikiLint({ onNavigate }: WikiLintProps) {
  const [report, setReport] = useState<LintReport | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [resolveTarget, setResolveTarget] = useState<{ finding: LintFinding; idx: number } | null>(null)

  const loadReport = useCallback(() => {
    setLoading(true)
    setError(null)
    runLint()
      .then((r) => setReport(r))
      .catch((err: unknown) => setError(err instanceof Error ? err.message : 'Failed to run lint'))
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    loadReport()
  }, [loadReport])

  const countBySev = (sev: string) =>
    (report?.findings ?? []).filter((f) => f.severity === sev).length

  return (
    <main className="wk-audit" data-testid="wk-lint">
      <header className="wk-audit-header">
        <div>
          <h1 className="wk-audit-title">Wiki lint</h1>
          <p className="wk-audit-strapline">
            Daily health check across all wiki entities — contradictions, orphans, stale claims,
            missing cross-refs, and dedup review.
          </p>
        </div>
        <div className="wk-audit-stats" aria-live="polite">
          {loading
            ? 'Running lint…'
            : error
              ? 'Error'
              : report
                ? `${countBySev('critical')} critical · ${countBySev('warning')} warnings · ${countBySev('info')} info`
                : ''}
        </div>
      </header>

      <section className="wk-audit-filters" aria-label="Actions">
        <button
          type="button"
          className="wk-audit-export"
          onClick={loadReport}
          disabled={loading}
        >
          {loading ? 'Running…' : 'Run lint now'}
        </button>
        {report && (
          <span className="wk-audit-strapline" style={{ alignSelf: 'center' }}>
            Report date: {report.date}
          </span>
        )}
      </section>

      {loading && !report ? (
        <div className="wk-loading">Running lint checks…</div>
      ) : error ? (
        <div className="wk-error">Error: {error}</div>
      ) : report && report.findings.length === 0 ? (
        <div className="wk-audit-empty" data-testid="wk-lint-empty">
          No findings — wiki is healthy.
        </div>
      ) : report ? (
        <table className="wk-audit-table">
          <thead>
            <tr>
              <th scope="col">Severity</th>
              <th scope="col">Type</th>
              <th scope="col">Entity</th>
              <th scope="col">Summary</th>
              <th scope="col">Action</th>
            </tr>
          </thead>
          <tbody>
            {report.findings.map((f, idx) => (
              <tr
                key={`${f.type}-${f.entity_slug ?? ''}-${idx}`}
                className={`wk-audit-row ${findingRowClass(f.severity)}`}
              >
                <td className="wk-audit-when">
                  <span
                    className={`wk-lint-severity wk-lint-severity--${f.severity}`}
                    aria-label={severityLabel(f.severity) + ' finding'}
                  >
                    {severityLabel(f.severity)}
                  </span>
                </td>
                <td className="wk-audit-msg">{f.type.replace(/_/g, ' ')}</td>
                <td className="wk-audit-author">
                  {f.entity_slug ? (
                    <a
                      href={`#/wiki/${encodeURI(f.entity_slug)}`}
                      onClick={(ev) => {
                        ev.preventDefault()
                        onNavigate(f.entity_slug ?? null)
                      }}
                      className="wk-wikilink"
                      data-wikilink="true"
                    >
                      {f.entity_slug}
                    </a>
                  ) : (
                    <span className="wk-audit-paths-empty">—</span>
                  )}
                </td>
                <td className="wk-audit-msg">{f.summary}</td>
                <td>
                  {f.type === 'contradictions' && f.resolve_actions ? (
                    <button
                      type="button"
                      className="wk-editor-save"
                      style={{ padding: '4px 10px', fontSize: 12 }}
                      onClick={() => setResolveTarget({ finding: f, idx })}
                    >
                      Resolve
                    </button>
                  ) : (
                    <span aria-hidden="true">—</span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : null}

      {resolveTarget && report && (
        <ResolveContradictionModal
          finding={resolveTarget.finding}
          findingIdx={resolveTarget.idx}
          reportDate={report.date}
          onClose={() => setResolveTarget(null)}
          onResolved={() => {
            setResolveTarget(null)
            loadReport()
          }}
        />
      )}
    </main>
  )
}

function severityLabel(sev: string): string {
  switch (sev) {
    case 'critical':
      return 'Critical'
    case 'warning':
      return 'Warning'
    case 'info':
      return 'Info'
    default:
      return sev
  }
}

function findingRowClass(sev: string): string {
  switch (sev) {
    case 'critical':
      return 'is-recovery' // reuse existing red-ish row style
    case 'warning':
      return 'is-bootstrap' // amber
    default:
      return 'is-agent'
  }
}
