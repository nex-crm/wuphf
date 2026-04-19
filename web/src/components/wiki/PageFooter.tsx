/** Wikipedia-style page footer: last-edited line + actions + dim git note. */

interface PageFooterProps {
  lastEditedBy: string
  lastEditedTs: string
  articlePath: string
  actions?: Array<{ label: string; onClick?: () => void }>
}

const DEFAULT_ACTIONS = [
  { label: 'View git history' },
  { label: 'Cite this page' },
  { label: 'Download as markdown' },
  { label: 'Export PDF' },
  { label: 'Clone wiki locally' },
]

export default function PageFooter({
  lastEditedBy,
  lastEditedTs,
  articlePath,
  actions = DEFAULT_ACTIONS,
}: PageFooterProps) {
  return (
    <div className="wk-page-footer">
      <div>
        This article was last edited on{' '}
        <span className="wk-last-edit-ts">{formatFull(lastEditedTs)}</span> by{' '}
        <span className="wk-last-edit-name">{lastEditedBy}</span>. Text is available under the
        terms of your local workspace, written by your agent team.
      </div>
      <div className="wk-actions">
        {actions.map((action) => (
          <a
            key={action.label}
            href="#"
            onClick={(e) => {
              if (action.onClick) {
                e.preventDefault()
                action.onClick()
              }
            }}
          >
            {action.label}
          </a>
        ))}
      </div>
      <div className="wk-dim">
        Every edit is a real git commit authored by the named agent.{' '}
        <code>git log team/{articlePath}.md</code> shows the full trail.
      </div>
    </div>
  )
}

function formatFull(iso: string): string {
  try {
    const d = new Date(iso)
    if (Number.isNaN(d.getTime())) return iso
    const date = d.toISOString().slice(0, 10)
    const hours = String(d.getUTCHours()).padStart(2, '0')
    const mins = String(d.getUTCMinutes()).padStart(2, '0')
    return `${date} at ${hours}:${mins} UTC`
  } catch {
    return iso
  }
}
