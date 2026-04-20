import { create } from 'zustand'

export type Theme = 'nex'

export interface ChannelMeta {
  type: 'O' | 'D' | 'G'
  name?: string
  members?: string[]
  agentSlug?: string
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

  // DM mode
  dmMode: boolean
  dmAgentSlug: string | null
  enterDM: (slug: string, channel: string) => void
  exitDM: () => void

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
}

export const useAppStore = create<AppStore>((set) => ({
  brokerConnected: false,
  setBrokerConnected: (v) => set({ brokerConnected: v }),

  currentChannel: 'general',
  setCurrentChannel: (ch) => set({ currentChannel: ch, currentApp: null }),
  currentApp: null,
  setCurrentApp: (app) =>
    set((s) =>
      app
        ? s.dmMode
          ? { currentApp: app, dmMode: false, dmAgentSlug: null, currentChannel: 'general' }
          : { currentApp: app }
        : { currentApp: null },
    ),

  channelMeta: {},
  setChannelMeta: (slug, meta) =>
    set((s) => ({ channelMeta: { ...s.channelMeta, [slug]: meta } })),

  theme: 'nex',
  setTheme: (t) => {
    document.documentElement.setAttribute('data-theme', t)
    set({ theme: t })
  },

  sidebarAgentsOpen: true,
  toggleSidebarAgents: () => set((s) => ({ sidebarAgentsOpen: !s.sidebarAgentsOpen })),
  sidebarCollapsed: false,
  toggleSidebarCollapsed: () => set((s) => ({ sidebarCollapsed: !s.sidebarCollapsed })),

  activeThreadId: null,
  setActiveThreadId: (id) => set({ activeThreadId: id }),

  dmMode: false,
  dmAgentSlug: null,
  enterDM: (slug, channel) => set({ dmMode: true, dmAgentSlug: slug, currentChannel: channel, currentApp: null }),
  exitDM: () => set({ dmMode: false, dmAgentSlug: null, currentChannel: 'general' }),

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
}))
