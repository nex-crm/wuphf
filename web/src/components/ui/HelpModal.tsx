import { useCallback, useEffect } from 'react'
import { useAppStore } from '../../stores/app'
import { SLASH_COMMANDS } from '../messages/Autocomplete'

/**
 * One keyboard-shortcut row for the help modal.
 */
interface Keybinding {
  keys: string[]
  description: string
}

/**
 * Mirrors TUI operator guidance. The composer parity PR ships Ctrl+P/N history
 * and Esc handling; sibling PRs ship feed vim nav (j/k/g/G) and the graph app.
 * Listing them here even before they merge gives operators one place to learn
 * the full keymap.
 */
const COMPOSER_KEYS: Keybinding[] = [
  { keys: ['Enter'], description: 'Send message' },
  { keys: ['Shift', 'Enter'], description: 'Newline inside composer' },
  { keys: ['Ctrl', 'P'], description: 'Recall previous message in this channel' },
  { keys: ['Ctrl', 'N'], description: 'Forward through recalled history / restore draft' },
  { keys: ['↑'], description: 'Recall previous when composer is empty' },
  { keys: ['Esc'], description: 'Close autocomplete, mention, modal, or help' },
]

const NAV_KEYS: Keybinding[] = [
  { keys: ['j'], description: 'Scroll feed down one message' },
  { keys: ['k'], description: 'Scroll feed up one message' },
  { keys: ['Ctrl', 'D'], description: 'Half-page down' },
  { keys: ['Ctrl', 'U'], description: 'Half-page up' },
  { keys: ['g', 'g'], description: 'Jump to top of feed' },
  { keys: ['Shift', 'G'], description: 'Jump to bottom of feed' },
  { keys: ['/'], description: 'Open search / command palette' },
  { keys: ['?'], description: 'Toggle keyboard cheat sheet' },
]

interface HelpModalProps {
  open: boolean
  onClose: () => void
}

/**
 * Full-screen help surface opened by the `/help` slash command. Renders the
 * complete SLASH_COMMANDS list alongside composer + feed keybindings so
 * operators never have to leave the app to find a shortcut.
 *
 * Intentionally separate from `KeyboardCheatSheet`: the cheat sheet is a
 * corner popover that toggles on `?`; HelpModal is the full reference.
 */
export function HelpModal({ open, onClose }: HelpModalProps) {
  useEffect(() => {
    if (!open) return
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.preventDefault()
        onClose()
      }
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [open, onClose])

  const handleOverlayClick = useCallback(
    (e: React.MouseEvent) => {
      if (e.target === e.currentTarget) onClose()
    },
    [onClose],
  )

  if (!open) return null

  return (
    <div
      className="help-overlay"
      role="dialog"
      aria-modal="true"
      aria-label="Help — slash commands and keyboard shortcuts"
      onClick={handleOverlayClick}
    >
      <div className="help-modal card">
        <header className="help-header">
          <div>
            <h2 className="help-title">Keyboard + command reference</h2>
            <p className="help-subtitle">Everything the composer and feed understand.</p>
          </div>
          <button
            type="button"
            className="help-close"
            onClick={onClose}
            aria-label="Close help"
          >
            Esc
          </button>
        </header>

        <div className="help-body">
          <section className="help-section">
            <h3 className="help-section-title">Slash commands</h3>
            <ul className="help-list">
              {SLASH_COMMANDS.map((cmd) => (
                <li key={cmd.name} className="help-row">
                  <span className="help-cmd">
                    <span className="help-cmd-icon" aria-hidden>
                      {cmd.icon}
                    </span>
                    <code className="help-cmd-name">{cmd.name}</code>
                  </span>
                  <span className="help-cmd-desc">{cmd.desc}</span>
                </li>
              ))}
            </ul>
          </section>

          <section className="help-section">
            <h3 className="help-section-title">Composer</h3>
            <KeybindingList items={COMPOSER_KEYS} />
          </section>

          <section className="help-section">
            <h3 className="help-section-title">Feed navigation</h3>
            <KeybindingList items={NAV_KEYS} />
            <p className="help-note">
              Vim-style nav and the graph app ship in sibling PRs. This reference
              lists them upfront so your muscle memory does not have to wait.
            </p>
          </section>
        </div>
      </div>
    </div>
  )
}

function KeybindingList({ items }: { items: Keybinding[] }) {
  return (
    <ul className="help-list">
      {items.map((item) => (
        <li key={item.keys.join('+') + item.description} className="help-row">
          <span className="help-keys">
            {item.keys.map((k, i) => (
              <kbd key={`${k}-${i}`} className="help-kbd">
                {k}
              </kbd>
            ))}
          </span>
          <span className="help-cmd-desc">{item.description}</span>
        </li>
      ))}
    </ul>
  )
}

/**
 * Convenience mount for the Shell — reads open state from the store and
 * wires the close handler. Kept here instead of in Shell.tsx so the mount
 * and the dialog live side-by-side.
 */
export function HelpModalHost() {
  const open = useAppStore((s) => s.composerHelpOpen)
  const setOpen = useAppStore((s) => s.setComposerHelpOpen)
  return <HelpModal open={open} onClose={() => setOpen(false)} />
}
