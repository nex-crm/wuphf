import { create } from "zustand";

import {
  __internal as agentEventTimerInternal,
  computePillState,
  type PillState,
} from "../lib/agentEventTimer";

export type Theme = "nex" | "nex-dark" | "noir-gold";

/**
 * Snapshot payload for the SSE "activity" event. Lane A may not yet emit
 * `kind`; consumers must default to "routine". Lane A omits the field when
 * the classifier hasn't run, which is acceptable.
 */
export interface AgentActivitySnapshot {
  slug: string;
  status?: string;
  activity?: string;
  detail?: string;
  lastTime?: string;
  totalMs?: number;
  firstEventMs?: number;
  firstTextMs?: number;
  firstToolMs?: number;
  kind?: "routine" | "milestone" | "stuck";
}

/**
 * Stored snapshot — extends the wire payload with client-side timestamps used
 * to drive halo decay and idle/dim transitions.
 */
export interface StoredActivitySnapshot extends AgentActivitySnapshot {
  /** Wall-clock ms when this snapshot was received by the client. */
  receivedAtMs: number;
  /**
   * Wall-clock ms after which the halo glow expires. Stuck snapshots leave
   * this at the previous value (no false halo on stuck).
   */
  haloUntilMs: number;
}

const { HALO_DECAY_MS } = agentEventTimerInternal;

/**
 * Cap on per-slug history depth in agentActivityHistory. The Tier 2 hover
 * peek surfaces the most recent ≤6 prior events; the buffer holds 8 so the
 * peek has a small forward margin if display rules change.
 */
export const MAX_AGENT_HISTORY = 8;

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

  // Thread panel — captures the originating channel alongside the message id
  // so that replies posted while the user has navigated away from the channel
  // (e.g. into /apps/console) still land in the channel where the thread
  // started, instead of the URL's current fallback channel.
  activeThread: { id: string; channelSlug: string } | null;
  setActiveThread: (thread: { id: string; channelSlug: string } | null) => void;

  // Last channel/dm the user visited. Held as a session-scoped fallback so
  // off-conversation surfaces (Console, Requests, sidebar request badge) can
  // surface the user's working channel rather than always defaulting to
  // #general when `useChannelSlug()` is null. Updated from the route effect
  // in MainContent.
  lastConversationalChannel: string | null;
  setLastConversationalChannel: (channelSlug: string | null) => void;

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

  // Agent activity (SSE-driven event bubbles)
  agentActivitySnapshots: Record<string, StoredActivitySnapshot>;
  // Per-slug ring buffer of prior snapshots, newest-first, capped at
  // MAX_AGENT_HISTORY. Powers the Tier 2 hover-peek "Recent" list. The
  // current snapshot lives in agentActivitySnapshots; history holds only
  // what was previously current and got displaced by a newer event.
  agentActivityHistory: Record<string, StoredActivitySnapshot[]>;
  recordActivitySnapshot: (snap: AgentActivitySnapshot) => void;

  // SSE reconnect grace — true after the EventSource has stayed in a
  // not-OPEN state for >5s. Drives the row-dim + bottom-of-rail
  // "Reconnecting…" indicator (eng decision A3).
  isReconnecting: boolean;
  setIsReconnecting: (v: boolean) => void;
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

  activeThread: null,
  setActiveThread: (thread) => set({ activeThread: thread }),

  lastConversationalChannel: null,
  setLastConversationalChannel: (channelSlug) => {
    if (get().lastConversationalChannel === channelSlug) return;
    set({ lastConversationalChannel: channelSlug });
  },

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

  agentActivitySnapshots: {},
  agentActivityHistory: {},
  recordActivitySnapshot: (snap) => {
    if (typeof snap?.slug !== "string" || snap.slug.length === 0) return;
    const { slug } = snap;
    const now = Date.now();
    set((state) => {
      const previous = state.agentActivitySnapshots[slug];
      // Stuck snapshots must NOT bump the halo window — a stuck transition
      // would otherwise visually read as "alive" via the halo glow. Preserve
      // the previous haloUntilMs (or default to a past value if none) so the
      // halo state derives correctly via computePillState.
      const haloUntilMs =
        snap.kind === "stuck"
          ? (previous?.haloUntilMs ?? 0)
          : now + HALO_DECAY_MS;
      // Push the previous current snapshot onto the per-slug history ring
      // buffer (newest-first). The current snapshot itself stays in
      // agentActivitySnapshots; history holds only displaced events. First
      // event for a slug leaves history untouched (no previous to keep).
      const prevHistory = state.agentActivityHistory[slug] ?? [];
      const nextHistory = previous
        ? [previous, ...prevHistory].slice(0, MAX_AGENT_HISTORY)
        : prevHistory;
      return {
        agentActivitySnapshots: {
          ...state.agentActivitySnapshots,
          [slug]: {
            ...snap,
            receivedAtMs: now,
            haloUntilMs,
          },
        },
        agentActivityHistory: {
          ...state.agentActivityHistory,
          [slug]: nextHistory,
        },
      };
    });
  },

  isReconnecting: false,
  setIsReconnecting: (v) => {
    if (get().isReconnecting === v) return;
    set({ isReconnecting: v });
  },

  onboardingComplete: false,
  setOnboardingComplete: (v) => set({ onboardingComplete: v }),
  resetForOnboarding: () =>
    set({
      unreadByChannel: {},
      activeThread: null,
      lastMessageId: null,
      clearedMessageIdsByChannel: {},
      activeAgentSlug: null,
      lastConversationalChannel: null,
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

/**
 * Derive the current pill state for an agent slug at `nowMs`. When no
 * snapshot exists for that slug yet, returns "idle" so the pill renders the
 * Office-voice fallback copy. Pure function: relies entirely on the store
 * snapshot and the injected `nowMs`, so the same call site is deterministic
 * under test.
 */
export function selectPillState(
  state: Pick<AppStore, "agentActivitySnapshots">,
  slug: string,
  nowMs: number,
): PillState {
  const snapshot = state.agentActivitySnapshots[slug];
  if (!snapshot) {
    return "idle";
  }
  return computePillState({
    lastEventMs: snapshot.receivedAtMs,
    nowMs,
    kind: snapshot.kind,
    haloUntilMs: snapshot.haloUntilMs,
  });
}

export interface AgentPeekData {
  current: StoredActivitySnapshot | undefined;
  history: StoredActivitySnapshot[];
}

// Stable empty-history reference so selectAgentPeek does not allocate a fresh
// array on every call. Important if the selector is later subscribed via
// Zustand — equal references avoid spurious re-renders.
const EMPTY_AGENT_HISTORY: readonly StoredActivitySnapshot[] = Object.freeze(
  [],
);

/**
 * Read the current snapshot + per-slug history for the Tier 2 hover peek.
 * Returns an empty history array (not undefined) when nothing has streamed
 * past for that slug yet, so consumers can `.map` without a guard.
 */
export function selectAgentPeek(
  state: Pick<AppStore, "agentActivitySnapshots" | "agentActivityHistory">,
  slug: string,
): AgentPeekData {
  return {
    current: state.agentActivitySnapshots[slug],
    history:
      state.agentActivityHistory[slug] ??
      (EMPTY_AGENT_HISTORY as StoredActivitySnapshot[]),
  };
}
