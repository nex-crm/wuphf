import type { ReactNode } from 'react'

/**
 * Shared keyboard-key badge. Renders real <kbd> semantics so assistive
 * tech announces the key, and uses a single visual treatment across the
 * app — sidebar hints, wizard CTAs, help modal, status bar.
 */
interface KbdProps {
  children: ReactNode
  size?: 'sm' | 'md'
  variant?: 'default' | 'inverse'
  className?: string
}

export function Kbd({ children, size = 'md', variant = 'default', className = '' }: KbdProps) {
  const cls = `kbd kbd-${size} ${variant === 'inverse' ? 'kbd-inverse' : ''} ${className}`.trim()
  return <kbd className={cls}>{children}</kbd>
}

/**
 * One or more keys rendered as a sequence, with a thin "then" separator
 * between chord segments. Pass keys as an array of arrays when needed
 * (e.g. `[['g'], ['g']]` for gg). For simple combos use a single array
 * (e.g. `['⌘', 'K']`).
 */
interface KbdSequenceProps {
  keys: string[] | string[][]
  size?: 'sm' | 'md'
  variant?: 'default' | 'inverse'
  className?: string
}

export function KbdSequence({ keys, size = 'md', variant = 'default', className = '' }: KbdSequenceProps) {
  const chords: string[][] = Array.isArray(keys[0]) ? (keys as string[][]) : [keys as string[]]
  return (
    <span className={`kbd-sequence ${className}`.trim()}>
      {chords.map((chord, i) => (
        <span key={i} className="kbd-chord">
          {i > 0 && <span className="kbd-then" aria-hidden="true">then</span>}
          {chord.map((k, j) => (
            <Kbd key={j} size={size} variant={variant}>
              {k}
            </Kbd>
          ))}
        </span>
      ))}
    </span>
  )
}

/**
 * Platform-aware modifier label. macOS users see the glyph; everyone else
 * sees "Ctrl". We only detect once at module load; swapping OS mid-session
 * is not a real case.
 */
const isMac = typeof navigator !== 'undefined'
  && /Mac|iPod|iPhone|iPad/.test(navigator.platform)

export const MOD_KEY = isMac ? '⌘' : 'Ctrl'
