import { useState, useRef, useCallback } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  getPolicies,
  createPolicy,
  deletePolicy,
  type Policy,
} from '../../api/client'
import { showNotice } from '../ui/Toast'

const SECTIONS = [
  { key: 'human_directed', label: 'Human-directed', icon: '\uD83D\uDC64' },
  { key: 'auto_detected', label: 'Auto-detected', icon: '\uD83E\uDD16' },
] as const

export function PoliciesApp() {
  const queryClient = useQueryClient()
  const [formOpen, setFormOpen] = useState(false)
  const [ruleText, setRuleText] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)

  const { data, isLoading, error } = useQuery({
    queryKey: ['policies'],
    queryFn: () => getPolicies(),
    refetchInterval: 15_000,
  })

  const invalidate = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: ['policies'] })
  }, [queryClient])

  const handleSave = useCallback(() => {
    const trimmed = ruleText.trim()
    if (!trimmed) return
    createPolicy('human_directed', trimmed)
      .then(() => {
        setRuleText('')
        setFormOpen(false)
        invalidate()
      })
      .catch((e: Error) => showNotice('Save failed: ' + e.message, 'error'))
  }, [ruleText, invalidate])

  const handleDelete = useCallback(
    (id: string) => {
      deletePolicy(id)
        .then(() => invalidate())
        .catch((e: Error) => showNotice('Delete failed: ' + e.message, 'error'))
    },
    [invalidate],
  )

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'Enter') handleSave()
      if (e.key === 'Escape') {
        setFormOpen(false)
        setRuleText('')
      }
    },
    [handleSave],
  )

  const activePolicies = (data?.policies ?? []).filter((p) => p.active !== false)

  return (
    <>
      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '16px 20px 8px' }}>
        <div style={{ fontSize: 13, color: 'var(--text-secondary)' }}>
          Office operating rules. Set explicitly or auto-detected from working patterns.
        </div>
        <button
          className="btn btn-secondary btn-sm"
          onClick={() => {
            setFormOpen((v) => !v)
            setTimeout(() => inputRef.current?.focus(), 50)
          }}
        >
          + Add rule
        </button>
      </div>

      {/* Inline add form */}
      {formOpen && (
        <div style={{ padding: '8px 20px 12px', borderBottom: '1px solid var(--border)' }}>
          <input
            ref={inputRef}
            className="input"
            type="text"
            placeholder='e.g. "Always ask before deploying to production"'
            value={ruleText}
            onChange={(e) => setRuleText(e.target.value)}
            onKeyDown={handleKeyDown}
            style={{ marginBottom: 8 }}
          />
          <div style={{ display: 'flex', gap: 8 }}>
            <button className="btn btn-primary btn-sm" onClick={handleSave}>
              Save
            </button>
            <button
              className="btn btn-secondary btn-sm"
              onClick={() => {
                setFormOpen(false)
                setRuleText('')
              }}
            >
              Cancel
            </button>
          </div>
        </div>
      )}

      {/* Policy list */}
      {isLoading && (
        <div style={{ padding: 20, textAlign: 'center', color: 'var(--text-tertiary)', fontSize: 14 }}>
          Loading...
        </div>
      )}

      {error && (
        <div style={{ padding: '40px 20px', textAlign: 'center', color: 'var(--text-tertiary)', fontSize: 14 }}>
          Failed to load policies.
        </div>
      )}

      {!isLoading && !error && activePolicies.length === 0 && (
        <div style={{ padding: '40px 20px', textAlign: 'center', color: 'var(--text-tertiary)', fontSize: 14 }}>
          No policies set. Add one above.
        </div>
      )}

      {!isLoading && !error && activePolicies.length > 0 && (
        <div style={{ padding: '8px 0' }}>
          {SECTIONS.map((section) => {
            const sectionPolicies = activePolicies.filter((p) => p.source === section.key)
            if (sectionPolicies.length === 0) return null
            return (
              <div key={section.key}>
                <div style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 6,
                  padding: '6px 20px',
                  fontSize: 11,
                  fontWeight: 600,
                  color: 'var(--text-tertiary)',
                  textTransform: 'uppercase',
                  letterSpacing: '0.08em',
                }}>
                  <span>{section.icon}</span>
                  <span>{section.label}</span>
                </div>
                {sectionPolicies.map((policy) => (
                  <PolicyRow key={policy.id} policy={policy} onDelete={handleDelete} />
                ))}
              </div>
            )
          })}
        </div>
      )}
    </>
  )
}

interface PolicyRowProps {
  policy: Policy
  onDelete: (id: string) => void
}

function PolicyRow({ policy, onDelete }: PolicyRowProps) {
  return (
    <div className="app-card" style={{ margin: '0 12px 6px', display: 'flex', alignItems: 'center', gap: 8 }}>
      <span style={{ fontSize: 14, flexShrink: 0 }}>{'\uD83D\uDCCB'}</span>
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ fontWeight: 500, fontSize: 13 }}>{policy.rule}</div>
        <div className="app-card-meta">
          <span className="badge badge-green" style={{ fontSize: 10 }}>ACTIVE</span>
        </div>
      </div>
      <button
        style={{
          background: 'none',
          border: 'none',
          cursor: 'pointer',
          color: 'var(--text-tertiary)',
          fontSize: 16,
          padding: '0 4px',
          lineHeight: 1,
          flexShrink: 0,
        }}
        title="Deactivate"
        onClick={() => onDelete(policy.id)}
      >
        {'\u00D7'}
      </button>
    </div>
  )
}
