import { useEffect } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useAppStore } from '../stores/app'
import { getChannels } from '../api/client'

/** Global keyboard shortcuts matching legacy behavior. */
export function useKeyboardShortcuts() {
  const setSearchOpen = useAppStore((s) => s.setSearchOpen)
  const setActiveAgentSlug = useAppStore((s) => s.setActiveAgentSlug)
  const setActiveThreadId = useAppStore((s) => s.setActiveThreadId)
  const setCurrentApp = useAppStore((s) => s.setCurrentApp)
  const setCurrentChannel = useAppStore((s) => s.setCurrentChannel)
  const setLastMessageId = useAppStore((s) => s.setLastMessageId)
  const queryClient = useQueryClient()

  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      // Cmd+K or Ctrl+K → command palette
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault()
        const state = useAppStore.getState()
        setSearchOpen(!state.searchOpen)
        return
      }

      // Cmd+/ or Ctrl+/ → focus composer
      if ((e.metaKey || e.ctrlKey) && e.key === '/') {
        e.preventDefault()
        const ta = document.querySelector<HTMLTextAreaElement>('.composer-input')
        ta?.focus()
        return
      }

      // Cmd+1..9 → quick-jump to nth channel
      if ((e.metaKey || e.ctrlKey) && e.key >= '1' && e.key <= '9') {
        const target = e.target as HTMLElement | null
        // Don't intercept inside text inputs unless modifier is also present
        if (target?.tagName === 'INPUT' || target?.tagName === 'TEXTAREA') {
          // Only the modifier+digit combo lands here, so still safe.
        }
        const cached = queryClient.getQueryData<{ channels: { slug: string }[] }>(['channels'])
        const channels = cached?.channels
        if (!channels) {
          // Fetch once if cache cold
          getChannels().then((data) => {
            queryClient.setQueryData(['channels'], data)
          }).catch(() => {})
          return
        }
        const idx = parseInt(e.key, 10) - 1
        const ch = channels[idx]
        if (!ch) return
        e.preventDefault()
        setCurrentApp(null)
        setCurrentChannel(ch.slug)
        setLastMessageId(null)
        return
      }

      // Escape → close panels in priority order
      if (e.key === 'Escape') {
        const state = useAppStore.getState()
        if (state.searchOpen) { setSearchOpen(false); return }
        if (state.activeAgentSlug) { setActiveAgentSlug(null); return }
        if (state.activeThreadId) { setActiveThreadId(null); return }
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [setSearchOpen, setActiveAgentSlug, setActiveThreadId, setCurrentApp, setCurrentChannel, setLastMessageId, queryClient])
}
