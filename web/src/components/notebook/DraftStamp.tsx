/**
 * Rotated red DRAFT stamp — absolutely positioned in the top-right of the
 * entry article. Critical accessibility: `role="img"` with a spelled-out
 * aria-label so screen readers announce the most important state signal.
 *
 * Visual geometry per DESIGN-NOTEBOOK.md DRAFT stamp section.
 */

interface DraftStampProps {
  /** Override the aria-label; default covers both the stamp symbol and state. */
  label?: string
}

export default function DraftStamp({ label }: DraftStampProps) {
  return (
    <div
      className="nb-draft-stamp"
      role="img"
      aria-label={label ?? 'Draft entry, not yet reviewed'}
    >
      DRAFT
    </div>
  )
}
