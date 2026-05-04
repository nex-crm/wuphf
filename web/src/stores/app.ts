import { create } from "zustand";

export type Theme = "nex" | "nex-dark" | "noir-gold";

const _storedTheme = ((): Theme => {
  try {
    const v = localStorage.getItem("wuphf-theme");
    if (v === "nex" || v === "nex-dark" || v === "noir-gold") return v;
  } catch {}
  return "nex";
})();
if (typeof document !== "undefined") {
  document.documentElement.setAttribute("data-theme", _storedTheme);
}

interface SidebarSectionsState {
  agents: boolean;
  channels: boolean;
  apps: boolean;
}

const SIDEBAR_SECTIONS_KEY = "wuphf-sidebar-sections";
const SIDEBAR_BG_KEY = "wuphf-sidebar-bg";

const _storedSidebarSections = ((): SidebarSectionsState => {
  const def: SidebarSectionsState = {
    agents: true,
    channels: true,
    apps: true,
  };
  try {
    const raw = localStorage.getItem(SIDEBAR_SECTIONS_KEY);
    if (!raw) return def;
    const parsed = JSON.parse(raw) as Partial<SidebarSectionsState>;
    return {
      agents: parsed.agents ?? def.agents,
      channels: parsed.channels ?? def.channels,
      apps: parsed.apps ?? def.apps,
    };
  } catch {
    return def;
  }
})();

const _storedSidebarBg = ((): string | null => {
  try {
    const v = localStorage.getItem(SIDEBAR_BG_KEY);
    return v?.trim() ? v : null;
  } catch {
    return null;
  }
})();

function persistSidebarSections(state: SidebarSectionsState): void {
  try {
    localStorage.setItem(SIDEBAR_SECTIONS_KEY, JSON.stringify(state));
  } catch {}
}

/**
 * Build the broker's canonical direct-message channel slug for an agent.
 * The broker pairs `<lower>__<higher>` for stable ordering across sides;
 * we pass `humanSlug="human"` to match what `/dm` API endpoints expect.
 */
export function directChannelSlug(
  agentSlug: string,
  humanSlug = "human",
): string {
  const a = humanSlug.trim().toLowerCase();
  const b = agentSlug.trim().toLowerCase();
  return a > b ? `${b}__${a}` : `${a}__${b}`;
}

export interface AppStore {
  // Connection
  brokerConnected: boolean;
  setBrokerConnected: (v: boolean) => void;

  // Theme
  theme: Theme;
  setTheme: (t: Theme) => void;

  // Sidebar
  sidebarAgentsOpen: boolean;
  toggleSidebarAgents: () => void;
  sidebarChannelsOpen: boolean;
  toggleSidebarChannels: () => void;
  sidebarAppsOpen: boolean;
  toggleSidebarApps: () => void;
  sidebarCollapsed: boolean;
  toggleSidebarCollapsed: () => void;
  sidebarBg: string | null;
  setSidebarBg: (color: string | null) => void;

  // Thread panel
  activeThreadId: string | null;
  setActiveThreadId: (id: string | null) => void;

  // Per-thread collapsed state in the main feed. The key is the parent
  // message id. Default is expanded (entry absent or false); toggling
  // stores `true` so the inline replies hide.
  collapsedThreads: Record<string, boolean>;
  toggleThreadCollapsed: (parentId: string) => void;

  // Message polling state
  lastMessageId: string | null;
  setLastMessageId: (id: string | null) => void;
  clearedMessageIdsByChannel: Record<string, string>;
  setChannelClearMarker: (channel: string, messageId: string | null) => void;
  unreadByChannel: Record<string, number>;
  incrementUnread: (channel: string) => void;
  clearUnread: (channel: string) => void;

  // Agent panel
  activeAgentSlug: string | null;
  setActiveAgentSlug: (slug: string | null) => void;

  // Search
  searchOpen: boolean;
  setSearchOpen: (v: boolean) => void;
  /**
   * Query to prefill in the SearchModal on next open. Set by the composer
   * `/search <query>` command and cleared by the modal when consumed.
   */
  composerSearchInitialQuery: string;
  setComposerSearchInitialQuery: (q: string) => void;

  // Help modal — /help slash command surface
  composerHelpOpen: boolean;
  setComposerHelpOpen: (v: boolean) => void;

  // /connect integration wizard. Bare /connect opens the provider picker
  // (mode = "provider", parity with the TUI's `/connect` 4-option picker).
  // `/connect telegram` skips the picker and lands on the Telegram token
  // step (mode = "telegram"). Other modes can be added when more
  // integrations get web wizards.
  telegramConnectOpen: boolean;
  telegramConnectMode: "provider" | "telegram";
  openConnectWizard: (mode: "provider" | "telegram") => void;
  setTelegramConnectOpen: (v: boolean) => void;

  // Onboarding
  onboardingComplete: boolean;
  setOnboardingComplete: (v: boolean) => void;
  resetForOnboarding: () => void;
}

export const useAppStore = create<AppStore>((set, get) => ({
  brokerConnected: false,
  setBrokerConnected: (v) => set({ brokerConnected: v }),

  theme: _storedTheme,
  setTheme: (t) => {
    // Same try/catch shape as the read path above. Safari private browsing
    // and sandboxed-iframe contexts both throw on localStorage writes; the
    // toggle should still update the DOM + store even if persistence fails,
    // so the user gets the visible state change for the current session.
    // console.warn keeps a breadcrumb so a user reporting "theme doesn't
    // stick" has something diagnosable in DevTools.
    try {
      localStorage.setItem("wuphf-theme", t);
    } catch (err) {
      console.warn(
        "setTheme: localStorage.setItem failed; theme will not persist across reloads",
        err,
      );
    }
    document.documentElement.setAttribute("data-theme", t);
    set({ theme: t });
  },

  sidebarAgentsOpen: _storedSidebarSections.agents,
  toggleSidebarAgents: () => {
    const next = !get().sidebarAgentsOpen;
    set({ sidebarAgentsOpen: next });
    persistSidebarSections({
      agents: next,
      channels: get().sidebarChannelsOpen,
      apps: get().sidebarAppsOpen,
    });
  },
  sidebarChannelsOpen: _storedSidebarSections.channels,
  toggleSidebarChannels: () => {
    const next = !get().sidebarChannelsOpen;
    set({ sidebarChannelsOpen: next });
    persistSidebarSections({
      agents: get().sidebarAgentsOpen,
      channels: next,
      apps: get().sidebarAppsOpen,
    });
  },
  sidebarAppsOpen: _storedSidebarSections.apps,
  toggleSidebarApps: () => {
    const next = !get().sidebarAppsOpen;
    set({ sidebarAppsOpen: next });
    persistSidebarSections({
      agents: get().sidebarAgentsOpen,
      channels: get().sidebarChannelsOpen,
      apps: next,
    });
  },
  sidebarCollapsed: false,
  toggleSidebarCollapsed: () =>
    set({ sidebarCollapsed: !get().sidebarCollapsed }),
  sidebarBg: _storedSidebarBg,
  setSidebarBg: (color) => {
    try {
      if (color) localStorage.setItem(SIDEBAR_BG_KEY, color);
      else localStorage.removeItem(SIDEBAR_BG_KEY);
    } catch {}
    set({ sidebarBg: color });
  },

  activeThreadId: null,
  setActiveThreadId: (id) => set({ activeThreadId: id }),

  collapsedThreads: {},
  toggleThreadCollapsed: (parentId) =>
    set((s) => ({
      collapsedThreads: {
        ...s.collapsedThreads,
        [parentId]: !s.collapsedThreads[parentId],
      },
    })),

  lastMessageId: null,
  setLastMessageId: (id) => set({ lastMessageId: id }),
  clearedMessageIdsByChannel: {},
  setChannelClearMarker: (channel, messageId) => {
    const ch = channel.trim() || "general";
    const id = messageId?.trim() || "";
    set((state) => {
      const next = { ...state.clearedMessageIdsByChannel };
      if (id) next[ch] = id;
      else delete next[ch];
      return { clearedMessageIdsByChannel: next };
    });
  },
  unreadByChannel: {},
  incrementUnread: (channel) => {
    const ch = channel.trim() || "general";
    set((state) => ({
      unreadByChannel: {
        ...state.unreadByChannel,
        [ch]: (state.unreadByChannel[ch] ?? 0) + 1,
      },
    }));
  },
  clearUnread: (channel) => {
    const ch = channel.trim() || "general";
    set((state) => {
      if ((state.unreadByChannel[ch] ?? 0) === 0) return state;
      return {
        unreadByChannel: { ...state.unreadByChannel, [ch]: 0 },
      };
    });
  },

  activeAgentSlug: null,
  setActiveAgentSlug: (slug) => set({ activeAgentSlug: slug }),

  searchOpen: false,
  setSearchOpen: (v) => set({ searchOpen: v }),
  composerSearchInitialQuery: "",
  setComposerSearchInitialQuery: (q) => set({ composerSearchInitialQuery: q }),

  composerHelpOpen: false,
  setComposerHelpOpen: (v) => set({ composerHelpOpen: v }),

  telegramConnectOpen: false,
  telegramConnectMode: "provider",
  openConnectWizard: (mode) =>
    set({ telegramConnectOpen: true, telegramConnectMode: mode }),
  setTelegramConnectOpen: (v) => set({ telegramConnectOpen: v }),

  onboardingComplete: false,
  setOnboardingComplete: (v) => set({ onboardingComplete: v }),
  resetForOnboarding: () =>
    set({
      unreadByChannel: {},
      activeThreadId: null,
      lastMessageId: null,
      clearedMessageIdsByChannel: {},
      activeAgentSlug: null,
      searchOpen: false,
      composerSearchInitialQuery: "",
      composerHelpOpen: false,
      // Close the /connect wizard during an onboarding reset for the same
      // reason searchOpen / composerHelpOpen are: any modal left open here
      // would float over the onboarding flow.
      telegramConnectOpen: false,
      onboardingComplete: false,
    }),
}));
