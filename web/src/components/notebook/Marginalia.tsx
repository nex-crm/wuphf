/**
 * Marginalia — right-gutter Caveat-font Q/NEXT/TODO callouts.
 * Collapses to inline callout on tablet + mobile via CSS.
 *
 * Most marginalia renders through `EntryBody`'s blockquote handler, but
 * this standalone component lets programmatic callers (e.g., promoted-back
 * badges, review hints) drop anchored callouts.
 */

import type { ReactNode } from 'react'

interface MarginaliaProps {
  tag?: 'Q' | 'Next' | 'TODO' | string
  children: ReactNode
}

export default function Marginalia({ tag = 'Q', children }: MarginaliaProps) {
  return (
    <aside className="nb-margin" role="note" aria-label={`Margin note: ${tag}`}>
      <span className="nb-margin-tag">{tag}:</span>
      {children}
    </aside>
  )
}
