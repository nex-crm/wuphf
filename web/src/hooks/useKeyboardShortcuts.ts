import { useEffect } from 'react'
import { useAppStore } from '../stores/app'

/** Global keyboard shortcuts matching legacy behavior. */
export function useKeyboardShortcuts() {
  const setSearchOpen = useAppStore((s) => s.setSearchOpen)
  const setActiveAgentSlug = useAppStore((s) => s.setActiveAgentSlug)
  const setActiveThreadId = useAppStore((s) => s.setActiveThreadId)

  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      // Cmd+K or Ctrl+K → open search
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault()
        setSearchOpen(true)
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
  }, [setSearchOpen, setActiveAgentSlug, setActiveThreadId])
}
