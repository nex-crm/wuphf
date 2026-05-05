import { afterEach, describe, expect, it, vi } from "vitest";

import { directChannelSlug, useAppStore } from "./app";

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
