import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { directChannelSlug, selectPillState, useAppStore } from "./app";

afterEach(() => {
  useAppStore.setState({
    activeThread: null,
    lastMessageId: null,
    clearedMessageIdsByChannel: {},
    unreadByChannel: {},
    activeAgentSlug: null,
    lastConversationalChannel: null,
    searchOpen: false,
    composerSearchInitialQuery: "",
    composerHelpOpen: false,
    onboardingComplete: false,
    agentActivitySnapshots: {},
    isReconnecting: false,
  });
});

describe("DM channel helpers", () => {
  it("uses the broker canonical direct slug for both ordering directions", () => {
    // Lower lexicographic side comes first; this is what the broker
    // expects on /dm endpoints and what useBrokerEvents matches against
    // when suppressing unread for the active DM.
    expect(directChannelSlug("ceo")).toBe("ceo__human");
    expect(directChannelSlug("pm")).toBe("human__pm");
  });

  it("resets non-route session state for a shred flow", () => {
    useAppStore.setState({
      activeThread: { id: "thread-1", channelSlug: "engineering" },
      lastMessageId: "msg-1",
      clearedMessageIdsByChannel: { general: "msg-0" },
      activeAgentSlug: "ceo",
      lastConversationalChannel: "engineering",
      searchOpen: true,
      composerSearchInitialQuery: "stuck task",
      composerHelpOpen: true,
      onboardingComplete: true,
    });

    useAppStore.getState().resetForOnboarding();

    expect(useAppStore.getState()).toMatchObject({
      activeThread: null,
      lastMessageId: null,
      clearedMessageIdsByChannel: {},
      activeAgentSlug: null,
      lastConversationalChannel: null,
      searchOpen: false,
      composerSearchInitialQuery: "",
      composerHelpOpen: false,
      onboardingComplete: false,
    });
  });
});

describe("channel unread state", () => {
  it("increments and clears unread counts by channel", () => {
    useAppStore.getState().incrementUnread("launch");
    useAppStore.getState().incrementUnread("launch");
    useAppStore.getState().incrementUnread("general");

    expect(useAppStore.getState().unreadByChannel).toMatchObject({
      launch: 2,
      general: 1,
    });

    useAppStore.getState().clearUnread("launch");

    expect(useAppStore.getState().unreadByChannel.launch).toBe(0);
    expect(useAppStore.getState().unreadByChannel.general).toBe(1);
  });
});

describe("channel clear markers", () => {
  it("stores and removes clear markers by normalized channel", () => {
    useAppStore.getState().setChannelClearMarker(" launch ", " msg-2 ");
    useAppStore.getState().setChannelClearMarker("", "msg-general");

    expect(useAppStore.getState().clearedMessageIdsByChannel).toMatchObject({
      launch: "msg-2",
      general: "msg-general",
    });

    useAppStore.getState().setChannelClearMarker("launch", null);

    expect(useAppStore.getState().clearedMessageIdsByChannel.launch).toBe(
      undefined,
    );
    expect(useAppStore.getState().clearedMessageIdsByChannel.general).toBe(
      "msg-general",
    );
  });
});

describe("recordActivitySnapshot", () => {
  let nowSpy: ReturnType<typeof vi.spyOn> | null = null;

  beforeEach(() => {
    let t = 1_700_000_000_000;
    nowSpy = vi.spyOn(Date, "now").mockImplementation(() => t);
    // Each call advances the clock by 1s so successive snapshots have
    // distinct receivedAtMs / haloUntilMs values that the test can assert.
    nowSpy.mockImplementation(() => {
      const v = t;
      t += 1000;
      return v;
    });
  });

  afterEach(() => {
    nowSpy?.mockRestore();
  });

  it("stores the snapshot, stamps receivedAtMs, and sets haloUntilMs forward by ~600ms", () => {
    useAppStore.getState().recordActivitySnapshot({
      slug: "tess",
      activity: "drafting reply",
      kind: "routine",
    });

    const snap = useAppStore.getState().agentActivitySnapshots.tess;
    expect(snap.activity).toBe("drafting reply");
    expect(snap.kind).toBe("routine");
    expect(snap.haloUntilMs - snap.receivedAtMs).toBe(600);
  });

  it("bumps haloUntilMs on each routine event so the halo reblooms", () => {
    useAppStore
      .getState()
      .recordActivitySnapshot({ slug: "tess", kind: "routine" });
    const first =
      useAppStore.getState().agentActivitySnapshots.tess.haloUntilMs;

    useAppStore
      .getState()
      .recordActivitySnapshot({ slug: "tess", kind: "routine" });
    const second =
      useAppStore.getState().agentActivitySnapshots.tess.haloUntilMs;

    expect(second).toBeGreaterThan(first);
  });

  it("does NOT bump haloUntilMs on stuck snapshots — stuck must not visually read as alive", () => {
    useAppStore
      .getState()
      .recordActivitySnapshot({ slug: "rita", kind: "routine" });
    const beforeStuck =
      useAppStore.getState().agentActivitySnapshots.rita.haloUntilMs;

    useAppStore.getState().recordActivitySnapshot({
      slug: "rita",
      kind: "stuck",
      activity: "stuck on terraform lock",
    });

    const afterStuck = useAppStore.getState().agentActivitySnapshots.rita;
    expect(afterStuck.haloUntilMs).toBe(beforeStuck);
    expect(afterStuck.kind).toBe("stuck");
    expect(afterStuck.activity).toBe("stuck on terraform lock");
  });

  it("ignores snapshots with an empty or missing slug", () => {
    useAppStore.getState().recordActivitySnapshot({
      slug: "",
      activity: "noise",
    });
    expect(useAppStore.getState().agentActivitySnapshots).toEqual({});
  });
});

describe("selectPillState", () => {
  it("returns idle when no snapshot exists for the slug", () => {
    expect(
      selectPillState({ agentActivitySnapshots: {} }, "ghost", Date.now()),
    ).toBe("idle");
  });

  it("returns stuck when the snapshot kind is stuck", () => {
    const now = 1_700_000_000_000;
    const state = {
      agentActivitySnapshots: {
        rita: {
          slug: "rita",
          kind: "stuck" as const,
          receivedAtMs: now - 1000,
          haloUntilMs: now - 1000,
        },
      },
    };
    expect(selectPillState(state, "rita", now)).toBe("stuck");
  });
});

describe("setIsReconnecting", () => {
  it("only emits a state change when the value actually flips", () => {
    const sub = vi.fn();
    const unsub = useAppStore.subscribe(sub);
    try {
      useAppStore.getState().setIsReconnecting(false);
      expect(sub).not.toHaveBeenCalled();

      useAppStore.getState().setIsReconnecting(true);
      expect(sub).toHaveBeenCalledTimes(1);

      useAppStore.getState().setIsReconnecting(true);
      expect(sub).toHaveBeenCalledTimes(1);
    } finally {
      unsub();
    }
  });
});

describe("setTheme", () => {
  it("updates DOM + store even when localStorage.setItem throws", () => {
    // Simulates Safari private browsing / sandboxed-iframe (write-only block).
    // The previous setTheme threw uncaught here and broke the dark-mode
    // toggle entirely; the guard makes the DOM + store update succeed and
    // logs a breadcrumb instead.
    const setItemSpy = vi
      .spyOn(window.localStorage, "setItem")
      .mockImplementation(() => {
        throw new DOMException("QuotaExceededError", "QuotaExceededError");
      });
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});

    try {
      useAppStore.getState().setTheme("nex-dark");

      expect(setItemSpy).toHaveBeenCalledWith("wuphf-theme", "nex-dark");
      expect(useAppStore.getState().theme).toBe("nex-dark");
      expect(document.documentElement.getAttribute("data-theme")).toBe(
        "nex-dark",
      );
      expect(warnSpy).toHaveBeenCalledWith(
        expect.stringContaining("setTheme: localStorage.setItem failed"),
        expect.any(DOMException),
      );
    } finally {
      setItemSpy.mockRestore();
      warnSpy.mockRestore();
      // Reset DOM + store so other tests don't inherit dark theme.
      document.documentElement.setAttribute("data-theme", "nex");
      useAppStore.setState({ theme: "nex" });
    }
  });
});
