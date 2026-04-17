import { useRef, useState, useCallback } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { postMessage, post } from '../../api/client'
import { useAppStore } from '../../stores/app'
import { showNotice } from '../ui/Toast'

/** Handle slash commands. Returns true if the input was a command. */
function handleSlashCommand(input: string): boolean {
  const parts = input.split(/\s+/)
  const cmd = parts[0].toLowerCase()
  const args = parts.slice(1).join(' ')
  const store = useAppStore.getState()

  switch (cmd) {
    case '/clear':
      showNotice('Messages cleared', 'info')
      return true
    case '/help':
      showNotice('Commands: /clear /help /focus /collab /requests /tasks /policies /skills /calendar /search /1o1 /task /cancel /pause /resume /reset /recover', 'info')
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
    case '/search':
      store.setSearchOpen(true)
      return true
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
      post('/reset', {})
        .then(() => {
          store.setLastMessageId(null)
          store.setCurrentChannel('general')
          showNotice('Office reset', 'success')
        })
        .catch(() => showNotice('Reset failed', 'error'))
      return true
    case '/1o1': {
      if (!args) {
        showNotice('Usage: /1o1 <agent-slug>', 'info')
        return true
      }
      const slug = args.trim().toLowerCase()
      post<{ channel?: string }>('/channels/dm', { members: ['human', slug], type: 'direct' })
        .then((data) => {
          const ch = data.channel || `dm-${slug}`
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

export function Composer() {
  const currentChannel = useAppStore((s) => s.currentChannel)
  const setCurrentApp = useAppStore((s) => s.setCurrentApp)
  const [text, setText] = useState('')
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const queryClient = useQueryClient()

  const sendMutation = useMutation({
    mutationFn: (content: string) => postMessage(content, currentChannel),
    onSuccess: () => {
      setText('')
      if (textareaRef.current) {
        textareaRef.current.style.height = 'auto'
      }
      queryClient.invalidateQueries({ queryKey: ['messages', currentChannel] })
    },
    onError: (err: unknown) => {
      const message = err instanceof Error ? err.message : 'Failed to send message'
      // The broker blocks chat with 409 + "request pending; answer required" when
      // an agent is waiting on the human. Surface the question instead of a dead
      // toast that the user has no way to act on.
      if (/request pending|answer required/i.test(message)) {
        showNotice('Answer the pending request to resume chat.', 'error')
        setCurrentApp('requests')
        return
      }
      showNotice(message, 'error')
    },
  })

  const handleSend = useCallback(() => {
    const trimmed = text.trim()
    if (!trimmed || sendMutation.isPending) return

    // Handle slash commands
    if (trimmed.startsWith('/')) {
      if (handleSlashCommand(trimmed)) {
        setText('')
        return
      }
    }

    sendMutation.mutate(trimmed)
  }, [text, sendMutation])

  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSend()
    }
  }, [handleSend])

  const handleInput = useCallback(() => {
    const el = textareaRef.current
    if (el) {
      el.style.height = 'auto'
      el.style.height = Math.min(el.scrollHeight, 120) + 'px'
    }
  }, [])

  return (
    <div className="composer">
      <div className="composer-inner">
        <textarea
          ref={textareaRef}
          className="composer-input"
          placeholder={`Message #${currentChannel}`}
          value={text}
          onChange={(e) => { setText(e.target.value); handleInput() }}
          onKeyDown={handleKeyDown}
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
