import { useCallback, useEffect } from 'react'
import { useAppStore } from '../../stores/app'
import { SLASH_COMMANDS } from '../messages/Autocomplete'
import { Kbd, KbdSequence, MOD_KEY } from './Kbd'

/**
 * One keyboard-shortcut row for the help modal.
 */
interface Keybinding {
  keys: string[] | string[][]
  description: string
}

/**
 * Global shortcuts wired in `useKeyboardShortcuts` + Wizard + SearchModal.
 * These are the anywhere-in-the-app keys; composer/feed specifics live
 * below.
 */
const GLOBAL_KEYS: Keybinding[] = [
  { keys: ['?'], description: 'Toggle this keyboard reference' },
  { keys: [MOD_KEY, 'K'], description: 'Command palette — channels, agents, commands, search' },
  { keys: [MOD_KEY, '/'], description: 'Focus the composer' },
  { keys: [MOD_KEY, '1'], description: 'Jump to channel 1' },
  { keys: [MOD_KEY, '9'], description: 'Jump to channel 9 (1–9 supported)' },
  { keys: ['Esc'], description: 'Close the top-most modal, panel, or thread' },
  { keys: ['Tab'], description: 'Move focus forward between interactive elements' },
  { keys: ['Shift', 'Tab'], description: 'Move focus backward' },
]

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

const WIZARD_KEYS: Keybinding[] = [
  { keys: ['Enter'], description: 'Advance to the next step when ready' },
  { keys: ['Shift', 'Enter'], description: 'New line inside the first-task editor' },
  { keys: ['Tab'], description: 'Move between fields, tiles, and actions' },
  { keys: ['Esc'], description: 'Close an inline panel (Nex signup, etc.)' },
]

const PALETTE_KEYS: Keybinding[] = [
  { keys: ['↑'], description: 'Previous result' },
  { keys: ['↓'], description: 'Next result' },
  { keys: ['Enter'], description: 'Open selected result' },
  { keys: ['Esc'], description: 'Close the palette' },
]

const NAV_KEYS: Keybinding[] = [
  { keys: ['j'], description: 'Scroll feed down one message' },
  { keys: ['k'], description: 'Scroll feed up one message' },
  { keys: ['Ctrl', 'D'], description: 'Half-page down' },
  { keys: ['Ctrl', 'U'], description: 'Half-page up' },
  { keys: [['g'], ['g']], description: 'Jump to top of feed' },
  { keys: ['Shift', 'G'], description: 'Jump to bottom of feed' },
  { keys: ['/'], description: 'Open search / command palette' },
]

interface HelpModalProps {
  open: boolean
  onClose: () => void
}

/**
 * Full-screen help surface opened by the `/help` slash command or the
 * global `?` shortcut. Renders the complete SLASH_COMMANDS list alongside
 * composer, wizard, palette, and feed keybindings so operators never
 * have to leave the app to find a shortcut.
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
            <p className="help-subtitle">
              Run the whole app without a mouse. Press <Kbd size="sm">?</Kbd> anytime
              to open this pane.
            </p>
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
            <h3 className="help-section-title">Global</h3>
            <KeybindingList items={GLOBAL_KEYS} />
          </section>

          <section className="help-section">
            <h3 className="help-section-title">Command palette</h3>
            <KeybindingList items={PALETTE_KEYS} />
          </section>

          <section className="help-section">
            <h3 className="help-section-title">Onboarding wizard</h3>
            <KeybindingList items={WIZARD_KEYS} />
          </section>

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
      {items.map((item) => {
        const flatKey = Array.isArray(item.keys[0])
          ? (item.keys as string[][]).flat().join('+')
          : (item.keys as string[]).join('+')
        return (
          <li key={flatKey + item.description} className="help-row">
            <span className="help-keys">
              <KbdSequence keys={item.keys} size="sm" />
            </span>
            <span className="help-cmd-desc">{item.description}</span>
          </li>
        )
      })}
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
