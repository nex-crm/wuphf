import { create } from 'zustand'

export type Theme = 'nex'

export interface ChannelMeta {
  type: 'O' | 'D' | 'G'
  name?: string
  members?: string[]
  agentSlug?: string
}

const DM_SLUG_PREFIX = 'dm-human-'

/**
 * Resolve a channel slug into DM info, or null if not a DM.
 *
 * Prefers explicit channelMeta (written by enterDM), falls back to the
 * server's `dm-human-<agent>` naming convention so deep-links and page
 * reloads still classify DMs correctly before metadata is hydrated.
 */
export function isDMChannel(
  slug: string,
  meta: Record<string, ChannelMeta>,
): { agentSlug: string } | null {
  const m = meta[slug]
  if (m?.type === 'D' && m.agentSlug) return { agentSlug: m.agentSlug }
  if (slug.startsWith(DM_SLUG_PREFIX)) {
    return { agentSlug: slug.slice(DM_SLUG_PREFIX.length) }
  }
  return null
}

export interface AppStore {
  // Connection
  brokerConnected: boolean
  setBrokerConnected: (v: boolean) => void

  // Navigation
  currentChannel: string
  setCurrentChannel: (ch: string) => void
  currentApp: string | null // null = messages view
  setCurrentApp: (app: string | null) => void

  // Channel metadata (DM info, etc.)
  channelMeta: Record<string, ChannelMeta>
  setChannelMeta: (slug: string, meta: ChannelMeta) => void

  // Theme
  theme: Theme
  setTheme: (t: Theme) => void

  // Sidebar
  sidebarAgentsOpen: boolean
  toggleSidebarAgents: () => void
  sidebarCollapsed: boolean
  toggleSidebarCollapsed: () => void

  // Thread panel
  activeThreadId: string | null
  setActiveThreadId: (id: string | null) => void

  // DM entry: opens the given DM channel and records {type: 'D', agentSlug}
  // in channelMeta so downstream views can resolve the paired agent.
  enterDM: (agentSlug: string, channelSlug: string) => void

  // Message polling state
  lastMessageId: string | null
  setLastMessageId: (id: string | null) => void

  // Agent panel
  activeAgentSlug: string | null
  setActiveAgentSlug: (slug: string | null) => void

  // Search
  searchOpen: boolean
  setSearchOpen: (v: boolean) => void

  // Onboarding
  onboardingComplete: boolean
  setOnboardingComplete: (v: boolean) => void

  // Wiki
  wikiPath: string | null
  setWikiPath: (path: string | null) => void

  // Notebooks
  notebookAgentSlug: string | null
  notebookEntrySlug: string | null
  setNotebookRoute: (agentSlug: string | null, entrySlug: string | null) => void
}

export const useAppStore = create<AppStore>((set, get) => ({
  brokerConnected: false,
  setBrokerConnected: (v) => set({ brokerConnected: v }),

  currentChannel: 'general',
  setCurrentChannel: (ch) => set({ currentChannel: ch, currentApp: null }),
  currentApp: null,
  setCurrentApp: (app) => {
    if (!app) {
      set({ currentApp: null })
      return
    }

    const { currentChannel, channelMeta } = get()
    if (isDMChannel(currentChannel, channelMeta)) {
      set({ currentApp: app, currentChannel: 'general' })
      return
    }

    set({ currentApp: app })
  },

  channelMeta: {},
  setChannelMeta: (slug, meta) =>
    set({ channelMeta: { ...get().channelMeta, [slug]: meta } }),

  theme: 'nex',
  setTheme: (t) => {
    document.documentElement.setAttribute('data-theme', t)
    set({ theme: t })
  },

  sidebarAgentsOpen: true,
  toggleSidebarAgents: () => set({ sidebarAgentsOpen: !get().sidebarAgentsOpen }),
  sidebarCollapsed: false,
  toggleSidebarCollapsed: () => set({ sidebarCollapsed: !get().sidebarCollapsed }),

  activeThreadId: null,
  setActiveThreadId: (id) => set({ activeThreadId: id }),

  enterDM: (agentSlug, channelSlug) =>
    set((s) => ({
      currentChannel: channelSlug,
      currentApp: null,
      channelMeta: {
        ...s.channelMeta,
        [channelSlug]: { ...s.channelMeta[channelSlug], type: 'D', agentSlug },
      },
    })),

  lastMessageId: null,
  setLastMessageId: (id) => set({ lastMessageId: id }),

  activeAgentSlug: null,
  setActiveAgentSlug: (slug) => set({ activeAgentSlug: slug }),

  searchOpen: false,
  setSearchOpen: (v) => set({ searchOpen: v }),

  onboardingComplete: false,
  setOnboardingComplete: (v) => set({ onboardingComplete: v }),

  wikiPath: null,
  setWikiPath: (path) => set({ wikiPath: path }),

  notebookAgentSlug: null,
  notebookEntrySlug: null,
  setNotebookRoute: (agentSlug, entrySlug) =>
    set({ notebookAgentSlug: agentSlug, notebookEntrySlug: entrySlug }),
}))
