import { useState, type ReactNode } from 'react'
import type { StreamLine } from '../../hooks/useAgentStream'

interface StreamLineViewProps {
  line: StreamLine
  /** Compact mode collapses arrays and objects beyond the first level. */
  compact?: boolean
}

/**
 * Renders one SSE line from the agent stream. JSON gets pretty-printed with
 * key/value coloring; raw text falls through unchanged.
 */
export function StreamLineView({ line, compact = false }: StreamLineViewProps) {
  if (!line.parsed) {
    const text = line.data.length > 400 ? line.data.slice(0, 400) + '\u2026' : line.data
    return <div className="stream-line stream-line-raw">{text}</div>
  }

  return (
    <div className="stream-line stream-line-json">
      <Value value={line.parsed} depth={0} compact={compact} />
    </div>
  )
}

function Value({ value, depth, compact }: { value: unknown; depth: number; compact: boolean }): ReactNode {
  if (value == null) {
    return <span className="sv-null">null</span>
  }
  if (typeof value === 'boolean') {
    return <span className="sv-bool">{String(value)}</span>
  }
  if (typeof value === 'number') {
    return <span className="sv-num">{String(value)}</span>
  }
  if (typeof value === 'string') {
    if (/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}/.test(value)) {
      let display = value
      try {
        display = new Date(value).toLocaleString([], { hour: 'numeric', minute: '2-digit', second: '2-digit' })
      } catch {
        // keep raw
      }
      return <span className="sv-ts" title={value}>{display}</span>
    }
    const truncated = depth > 0 && value.length > 200 ? value.slice(0, 200) + '\u2026' : value
    return <span className="sv-str">{truncated}</span>
  }
  if (Array.isArray(value)) {
    if (value.length === 0) return <span className="sv-null">[]</span>
    if (compact && depth >= 1) return <span className="sv-str">[{value.length} items]</span>
    if (depth > 3) return <span className="sv-str">[{value.length} items]</span>
    return <Collapsible label={`[${value.length}]`} startOpen={depth === 0}>
      <div className="sv-array">
        {value.map((item, idx) => (
          <div key={idx} className="sv-array-item">
            <Value value={item} depth={depth + 1} compact={compact} />
          </div>
        ))}
      </div>
    </Collapsible>
  }
  if (typeof value === 'object') {
    const keys = Object.keys(value as Record<string, unknown>)
    if (keys.length === 0) return <span className="sv-null">{'{}'}</span>
    if (compact && depth >= 1) return <span className="sv-str">{`{${keys.length} fields}`}</span>
    if (depth > 3) return <span className="sv-str">{`{${keys.length} fields}`}</span>
    return <Collapsible label={`{${keys.length}}`} startOpen={depth === 0}>
      <div className="sv-obj">
        {keys.map((k) => (
          <div key={k} className="sv-obj-row">
            <span className="sv-key">{k}</span>
            <Value value={(value as Record<string, unknown>)[k]} depth={depth + 1} compact={compact} />
          </div>
        ))}
      </div>
    </Collapsible>
  }
  return <span className="sv-str">{String(value)}</span>
}

function Collapsible({ label, startOpen, children }: { label: string; startOpen: boolean; children: ReactNode }) {
  const [open, setOpen] = useState(startOpen)
  if (open) {
    return (
      <span className="sv-collapsible">
        <button type="button" className="sv-toggle" onClick={() => setOpen(false)} title="Collapse">▾ {label}</button>
        {children}
      </span>
    )
  }
  return (
    <button type="button" className="sv-toggle" onClick={() => setOpen(true)} title="Expand">▸ {label}</button>
  )
}
