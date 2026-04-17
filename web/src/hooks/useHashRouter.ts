import { useEffect, useRef } from 'react'
import { useAppStore } from '../stores/app'

type Route =
  | { view: 'channel'; channel: string }
  | { view: 'dm'; agent: string }
  | { view: 'app'; app: string }

function parseHash(hash: string): Route {
  const cleaned = hash.replace(/^#\/?/, '')
  const parts = cleaned.split('/').filter(Boolean)
  if (parts[0] === 'channels' && parts[1]) {
    return { view: 'channel', channel: decodeURIComponent(parts[1]) }
  }
  if (parts[0] === 'dm' && parts[1]) {
    return { view: 'dm', agent: decodeURIComponent(parts[1]) }
  }
  if (parts[0] === 'apps' && parts[1]) {
    return { view: 'app', app: decodeURIComponent(parts[1]) }
  }
  if (parts[0] === 'threads') {
    return { view: 'app', app: 'threads' }
  }
  return { view: 'channel', channel: 'general' }
}

function stateToHash(state: {
  currentApp: string | null
  currentChannel: string
  dmMode: boolean
  dmAgentSlug: string | null
}): string {
  if (state.dmMode && state.dmAgentSlug) {
    return `#/dm/${encodeURIComponent(state.dmAgentSlug)}`
  }
  if (state.currentApp) {
    return `#/apps/${encodeURIComponent(state.currentApp)}`
  }
  return `#/channels/${encodeURIComponent(state.currentChannel || 'general')}`
}

/**
 * Two-way sync between the Zustand app store and the location hash.
 *
 *   #/channels/<slug> ↔ currentChannel, currentApp=null, dmMode=false
 *   #/dm/<agent>      ↔ dmMode=true, dmAgentSlug=<agent>
 *   #/apps/<id>       ↔ currentApp=<id>
 *
 * Lets the user bookmark any screen and share URLs. Silent fallback to
 * the channel view if the hash is malformed.
 */
export function useHashRouter() {
  const currentApp = useAppStore((s) => s.currentApp)
  const currentChannel = useAppStore((s) => s.currentChannel)
  const dmMode = useAppStore((s) => s.dmMode)
  const dmAgentSlug = useAppStore((s) => s.dmAgentSlug)
  const setCurrentApp = useAppStore((s) => s.setCurrentApp)
  const setCurrentChannel = useAppStore((s) => s.setCurrentChannel)
  const enterDM = useAppStore((s) => s.enterDM)
  const exitDM = useAppStore((s) => s.exitDM)
  const setLastMessageId = useAppStore((s) => s.setLastMessageId)

  // Avoid ping-ponging: skip the next hashchange or store-sync when we
  // were the one that caused it.
  const ignoreNextHashChange = useRef(false)
  const ignoreNextStoreSync = useRef(false)

  // Apply current hash on mount and when it changes
  useEffect(() => {
    function applyHash() {
      if (ignoreNextHashChange.current) {
        ignoreNextHashChange.current = false
        return
      }
      const route = parseHash(window.location.hash)
      ignoreNextStoreSync.current = true
      if (route.view === 'dm') {
        // Broker may need the actual channel; reuse the dm-human-<slug>
        // convention the server uses by default.
        enterDM(route.agent, `dm-human-${route.agent}`)
      } else if (route.view === 'app') {
        exitDM()
        setCurrentApp(route.app)
      } else {
        exitDM()
        setCurrentApp(null)
        setCurrentChannel(route.channel)
        setLastMessageId(null)
      }
    }

    applyHash()
    window.addEventListener('hashchange', applyHash)
    return () => window.removeEventListener('hashchange', applyHash)
  }, [enterDM, exitDM, setCurrentApp, setCurrentChannel, setLastMessageId])

  // Push store changes back into the hash
  useEffect(() => {
    if (ignoreNextStoreSync.current) {
      ignoreNextStoreSync.current = false
      return
    }
    const next = stateToHash({ currentApp, currentChannel, dmMode, dmAgentSlug })
    if (next !== window.location.hash) {
      ignoreNextHashChange.current = true
      // Use replaceState for the initial sync so we don't spam history,
      // then push afterwards.
      window.history.replaceState(null, '', next)
    }
  }, [currentApp, currentChannel, dmMode, dmAgentSlug])
}
