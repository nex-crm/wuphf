import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createDM, getConfig, postMessage, post, setMemory } from '../../api/client'
import { directChannelSlug, useAppStore } from '../../stores/app'
import { useOfficeMembers } from '../../hooks/useMembers'
import { useCommands } from '../../hooks/useCommands'
import { showNotice } from '../ui/Toast'
import { confirm } from '../ui/ConfirmDialog'
import { openProviderSwitcher } from '../ui/ProviderSwitcher'
import { Autocomplete, applyAutocomplete, type AutocompleteItem } from './Autocomplete'

/** How many sent messages to keep in per-channel history. */
const COMPOSER_HISTORY_LIMIT = 20

/** sessionStorage key shape: `wuphf:composer-history:<channel>`. */
function historyKey(channel: string): string {
  return `wuphf:composer-history:${channel || 'general'}`
}

function readHistory(channel: string): string[] {
  try {
    const raw = sessionStorage.getItem(historyKey(channel))
    if (!raw) return []
    const parsed = JSON.parse(raw) as unknown
    if (!Array.isArray(parsed)) return []
    return parsed.filter((v): v is string => typeof v === 'string' && v.length > 0)
  } catch {
    return []
  }
}

function writeHistory(channel: string, entries: string[]): void {
  try {
    sessionStorage.setItem(historyKey(channel), JSON.stringify(entries))
  } catch {
    // sessionStorage disabled / quota exceeded — silently drop history rather
    // than blowing up the send flow. The user still sees their message land.
  }
}

/**
 * Append a sent message to the per-channel history, trimming to the most
 * recent COMPOSER_HISTORY_LIMIT entries. Skips duplicates of the latest
 * entry so rapid resends do not pollute recall.
 */
function pushHistory(channel: string, message: string): void {
  const trimmed = message.trim()
  if (!trimmed) return
  const current = readHistory(channel)
  if (current.length > 0 && current[current.length - 1] === trimmed) return
  const next = [...current, trimmed].slice(-COMPOSER_HISTORY_LIMIT)
  writeHistory(channel, next)
}

/** Routing prefix for `/ask`: mirrors TUI cmdAsk which always goes to the lead. */
function askPrefix(leadSlug: string | undefined): string {
  const slug = (leadSlug || 'ceo').trim().toLowerCase() || 'ceo'
  return `@${slug} `
}

/** Pick the team-lead slug: configured first, else first built-in agent, else 'ceo'. */
function resolveLeadSlug(
  configured: string | undefined,
  members: { slug?: string; built_in?: boolean }[],
): string {
  const explicit = (configured ?? '').trim().toLowerCase()
  if (explicit) return explicit
  const builtin = members.find((m) => m.built_in && m.slug && m.slug !== 'human' && m.slug !== 'you')
  if (builtin?.slug) return builtin.slug
  return 'ceo'
}

interface SlashHandlers {
  /** Team lead slug used for `/ask` routing. */
  leadSlug: string | undefined
  /** Send the given text as a normal message (bypasses slash parsing). */
  sendAsMessage: (text: string) => void
}

/**
 * Handle slash commands. Returns true if the input was consumed as a command.
 *
 * Some commands (e.g. `/ask`) rewrite the input and invoke sendAsMessage so
 * the broker sees a normal user message with the right @mention routing.
 */
function handleSlashCommand(input: string, handlers: SlashHandlers): boolean {
  const parts = input.split(/\s+/)
  const cmd = parts[0].toLowerCase()
  const args = parts.slice(1).join(' ').trim()
  const store = useAppStore.getState()

  switch (cmd) {
    case '/clear':
      showNotice('Messages cleared', 'info')
      return true
    case '/help':
      store.setComposerHelpOpen(true)
      return true
    case '/requests':
      store.setCurrentApp('requests')
      return true
    case '/policies':
      store.setCurrentApp('policies')
      return true
    case '/skills':
      store.setCurrentApp('skills')
      return true
    case '/calendar':
      store.setCurrentApp('calendar')
      return true
    case '/tasks':
      store.setCurrentApp('tasks')
      return true
    case '/recover':
    case '/doctor':
      store.setCurrentApp('health-check')
      return true
    case '/threads':
      store.setCurrentApp('threads')
      return true
    case '/provider':
      openProviderSwitcher()
      return true
    case '/search':
      store.setComposerSearchInitialQuery(args)
      store.setSearchOpen(true)
      return true
    case '/ask': {
      if (!args) {
        showNotice('Usage: /ask <question>', 'info')
        return true
      }
      // TUI's cmdAsk always routes to the team lead. Mirror that by
      // prefixing an @mention so the broker's routing picks up the lead.
      handlers.sendAsMessage(askPrefix(handlers.leadSlug) + args)
      return true
    }
    case '/remember': {
      if (!args) {
        showNotice('Usage: /remember <fact>', 'info')
        return true
      }
      // Broker /memory requires namespace + key + value. Use a stable
      // human-owned namespace and a short timestamp key so repeated
      // /remember calls do not collide.
      const key = `note-${Date.now().toString(36)}`
      setMemory('human-notes', key, args)
        .then(() =>
          showNotice(
            'Stored in memory: ' + (args.length > 40 ? args.slice(0, 40) + '…' : args),
            'success',
          ),
        )
        .catch((e: Error) => showNotice('Remember failed: ' + e.message, 'error'))
      return true
    }
    case '/focus':
      post('/focus-mode', { focus_mode: true })
        .then(() => showNotice('Switched to delegation mode', 'success'))
        .catch(() => showNotice('Failed to switch mode', 'error'))
      return true
    case '/collab':
      post('/focus-mode', { focus_mode: false })
        .then(() => showNotice('Switched to collaborative mode', 'success'))
        .catch(() => showNotice('Failed to switch mode', 'error'))
      return true
    case '/pause':
      post('/signals', { kind: 'pause', summary: 'Human paused all agents' })
        .then(() => showNotice('All agents paused', 'success'))
        .catch((e: Error) => showNotice('Pause failed: ' + e.message, 'error'))
      return true
    case '/resume':
      post('/signals', { kind: 'resume', summary: 'Human resumed agents' })
        .then(() => showNotice('Agents resumed', 'success'))
        .catch((e: Error) => showNotice('Resume failed: ' + e.message, 'error'))
      return true
    case '/reset':
      confirm({
        title: 'Reset the office?',
        message: 'Clears channels back to #general and drops in-memory state. Persisted tasks and requests stay on the broker.',
        confirmLabel: 'Reset',
        danger: true,
        onConfirm: () =>
          post('/reset', {})
            .then(() => {
              store.setLastMessageId(null)
              store.setCurrentChannel('general')
              showNotice('Office reset', 'success')
            })
            .catch((e: Error) => showNotice('Reset failed: ' + e.message, 'error')),
      })
      return true
    case '/1o1': {
      if (!args) {
        showNotice('Usage: /1o1 <agent-slug>', 'info')
        return true
      }
      const slug = args.trim().toLowerCase()
      createDM(slug)
        .then((data) => {
          const ch = data.slug || directChannelSlug(slug)
          store.enterDM(slug, ch)
        })
        .catch(() => showNotice('Agent not found: ' + args.trim(), 'error'))
      return true
    }
    case '/task': {
      const taskParts = args.split(/\s+/)
      const action = (taskParts[0] || '').toLowerCase()
      const taskId = taskParts[1] || ''
      const extra = taskParts.slice(2).join(' ')
      if (!action || !taskId) {
        showNotice('Usage: /task <claim|release|complete|block|approve> <task-id>', 'info')
        return true
      }
      const body: Record<string, string> = { action, id: taskId, channel: store.currentChannel }
      if (action === 'claim') body.owner = 'human'
      if (extra) body.details = extra
      post('/tasks', body)
        .then(() => showNotice(`Task ${taskId} → ${action}`, 'success'))
        .catch((e: Error) => showNotice('Task action failed: ' + e.message, 'error'))
      return true
    }
    case '/cancel': {
      if (!args) { showNotice('Usage: /cancel <task-id>', 'info'); return true }
      post('/tasks', { action: 'release', id: args.trim(), channel: store.currentChannel })
        .then(() => showNotice('Task ' + args.trim() + ' cancelled', 'success'))
        .catch(() => showNotice('Cancel failed', 'error'))
      return true
    }
    default:
      return false
  }
}

/**
 * History recall state. `draftStash` holds whatever the operator had typed
 * before the first Ctrl+P so we can restore it when they walk forward past
 * the end of history.
 */
interface HistoryState {
  /** -1 when live, else index into the cached history array. */
  index: number
  /** Draft text to restore when stepping past the end. */
  draftStash: string | null
  /** Snapshot taken at recall start; kept so mid-recall writes don't churn it. */
  entries: string[]
}

function emptyHistoryState(): HistoryState {
  return { index: -1, draftStash: null, entries: [] }
}

export function Composer() {
  const currentChannel = useAppStore((s) => s.currentChannel)
  const [text, setText] = useState('')
  const [caret, setCaret] = useState(0)
  const [acItems, setAcItems] = useState<AutocompleteItem[]>([])
  const [acIdx, setAcIdx] = useState(0)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const queryClient = useQueryClient()
  const { data: cfg } = useQuery({
    queryKey: ['config'],
    queryFn: getConfig,
    staleTime: 60_000,
  })
  const { data: members = [] } = useOfficeMembers()
  const leadSlug = useMemo(
    () => resolveLeadSlug(cfg?.team_lead_slug, members),
    [cfg?.team_lead_slug, members],
  )
  // Broker-backed slash-command registry. Falls back to the hardcoded
  // list if the broker is unreachable so the composer is never worse
  // than before this plumbing landed.
  const commands = useCommands()

  const historyRef = useRef<HistoryState>(emptyHistoryState())

  // Reset recall when switching channels so Ctrl+P replays *this* channel.
  useEffect(() => {
    historyRef.current = emptyHistoryState()
  }, [currentChannel])

  const resetRecall = useCallback(() => {
    historyRef.current = emptyHistoryState()
  }, [])

  const pickAutocomplete = useCallback((item: AutocompleteItem) => {
    const next = applyAutocomplete(text, caret, item)
    setText(next.text)
    requestAnimationFrame(() => {
      const el = textareaRef.current
      if (!el) return
      el.focus()
      el.setSelectionRange(next.caret, next.caret)
      setCaret(next.caret)
    })
  }, [text, caret])

  const sendMutation = useMutation({
    mutationFn: (content: string) => postMessage(content, currentChannel),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['messages', currentChannel] })
    },
    onError: (err: unknown) => {
      const message = err instanceof Error ? err.message : 'Failed to send message'
      // The broker blocks chat with 409 + "request pending; answer required" when
      // an agent is waiting on the human. The InterviewBar above the composer
      // already shows the question, so the user has somewhere to act. Never yank
      // them away from the textbox they are typing in.
      if (/request pending|answer required/i.test(message)) {
        showNotice('Answer the interview above to send messages.', 'info')
        return
      }
      showNotice(message, 'error')
    },
  })

  /**
   * Clear the composer, shrink the textarea, and cancel any pending recall.
   * Called after every successful send or consumed command.
   */
  const resetComposer = useCallback(() => {
    setText('')
    resetRecall()
    if (textareaRef.current) {
      textareaRef.current.style.height = 'auto'
    }
  }, [resetRecall])

  const handleSend = useCallback(() => {
    const trimmed = text.trim()
    if (!trimmed || sendMutation.isPending) return

    // Handle slash commands
    if (trimmed.startsWith('/')) {
      const consumed = handleSlashCommand(trimmed, {
        leadSlug,
        sendAsMessage: (rewritten) => {
          sendMutation.mutate(rewritten)
        },
      })
      if (consumed) {
        // Persist the *raw* command to history so Ctrl+P replays `/ask foo`,
        // not the rewritten `@ceo foo`. Matches user expectation.
        pushHistory(currentChannel, trimmed)
        resetComposer()
        return
      }
    }

    pushHistory(currentChannel, trimmed)
    sendMutation.mutate(trimmed)
    resetComposer()
  }, [text, sendMutation, leadSlug, currentChannel, resetComposer])

  /**
   * Walk backward through history. On first invocation, snapshot the live
   * draft so Ctrl+N can restore it. Returns true if recall succeeded.
   */
  const recallPrevious = useCallback((): boolean => {
    const state = historyRef.current
    if (state.index === -1) {
      const entries = readHistory(currentChannel)
      if (entries.length === 0) return false
      state.entries = entries
      state.draftStash = text
      state.index = entries.length
    }
    if (state.index <= 0) return false
    state.index -= 1
    setText(state.entries[state.index])
    return true
  }, [currentChannel, text])

  /**
   * Walk forward through history. When we run off the end, restore the
   * original draft and clear recall state.
   */
  const recallNext = useCallback((): boolean => {
    const state = historyRef.current
    if (state.index === -1) return false
    if (state.index < state.entries.length - 1) {
      state.index += 1
      setText(state.entries[state.index])
      return true
    }
    setText(state.draftStash ?? '')
    historyRef.current = emptyHistoryState()
    return true
  }, [])

  const moveCaretToEnd = useCallback(() => {
    requestAnimationFrame(() => {
      const el = textareaRef.current
      if (!el) return
      const end = el.value.length
      el.setSelectionRange(end, end)
      setCaret(end)
    })
  }, [])

  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    // Autocomplete navigation runs first
    if (acItems.length > 0) {
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        setAcIdx((i) => (i + 1) % acItems.length)
        return
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault()
        setAcIdx((i) => (i - 1 + acItems.length) % acItems.length)
        return
      }
      if (e.key === 'Enter' || e.key === 'Tab') {
        e.preventDefault()
        const pick = acItems[acIdx] ?? acItems[0]
        if (pick) pickAutocomplete(pick)
        return
      }
      if (e.key === 'Escape') {
        e.preventDefault()
        setAcItems([])
        return
      }
    }

    // History recall — Ctrl+P / Ctrl+N (TUI parity: internal/tui/interaction.go:56-58)
    if (e.ctrlKey && !e.metaKey && !e.altKey) {
      if (e.key === 'p' || e.key === 'P') {
        if (recallPrevious()) {
          e.preventDefault()
          moveCaretToEnd()
          return
        }
      }
      if (e.key === 'n' || e.key === 'N') {
        if (recallNext()) {
          e.preventDefault()
          moveCaretToEnd()
          return
        }
      }
    }

    // Slack-style: empty-draft ArrowUp recalls the last message.
    if (
      e.key === 'ArrowUp'
      && !e.shiftKey
      && !e.ctrlKey
      && !e.metaKey
      && !e.altKey
      && text === ''
    ) {
      if (recallPrevious()) {
        e.preventDefault()
        moveCaretToEnd()
        return
      }
    }

    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSend()
    }
  }, [handleSend, acItems, acIdx, pickAutocomplete, recallPrevious, recallNext, text, moveCaretToEnd])

  const handleAcItems = useCallback((items: AutocompleteItem[]) => {
    setAcItems(items)
    setAcIdx((idx) => Math.min(idx, Math.max(items.length - 1, 0)))
  }, [])

  const syncCaret = useCallback(() => {
    const el = textareaRef.current
    if (el) setCaret(el.selectionStart ?? 0)
  }, [])

  const handleInput = useCallback(() => {
    const el = textareaRef.current
    if (el) {
      el.style.height = 'auto'
      el.style.height = Math.min(el.scrollHeight, 120) + 'px'
    }
  }, [])

  return (
    <div className="composer">
      <Autocomplete
        value={text}
        caret={caret}
        selectedIdx={acIdx}
        onItems={handleAcItems}
        onPick={pickAutocomplete}
        commands={commands}
      />
      <div className="composer-inner">
        <textarea
          ref={textareaRef}
          className="composer-input"
          placeholder={`Message #${currentChannel}`}
          value={text}
          onChange={(e) => {
            setText(e.target.value)
            setCaret(e.target.selectionStart ?? 0)
            handleInput()
            // Any manual edit cancels history recall.
            if (historyRef.current.index !== -1) {
              resetRecall()
            }
          }}
          onKeyDown={handleKeyDown}
          onKeyUp={syncCaret}
          onClick={syncCaret}
          rows={1}
        />
        <button
          className="composer-send"
          disabled={!text.trim() || sendMutation.isPending}
          onClick={handleSend}
          aria-label="Send message"
        >
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="m22 2-7 20-4-9-9-4Z" />
            <path d="M22 2 11 13" />
          </svg>
        </button>
      </div>
    </div>
  )
}

// Re-export helpers for testing.
export const __test__ = {
  historyKey,
  readHistory,
  writeHistory,
  pushHistory,
  resolveLeadSlug,
  askPrefix,
  COMPOSER_HISTORY_LIMIT,
}
