import { afterEach, describe, expect, it, vi } from "vitest";

import { directChannelSlug, isDMChannel, useAppStore } from "./app";

afterEach(() => {
  useAppStore.setState({
    currentChannel: "general",
    currentApp: null,
    activeThreadId: null,
    lastMessageId: null,
    activeAgentSlug: null,
    searchOpen: false,
    composerSearchInitialQuery: "",
    composerHelpOpen: false,
    onboardingComplete: false,
    wikiPath: null,
    wikiLookupQuery: null,
    notebookAgentSlug: null,
    notebookEntrySlug: null,
  });
});

describe("DM channel helpers", () => {
  it("uses the broker canonical direct slug", () => {
    expect(directChannelSlug("ceo")).toBe("ceo__human");
    expect(directChannelSlug("pm")).toBe("human__pm");
  });

  it("recognizes canonical and legacy DM slugs", () => {
    expect(isDMChannel("ceo__human", {})).toEqual({ agentSlug: "ceo" });
    expect(isDMChannel("human__pm", {})).toEqual({ agentSlug: "pm" });
    expect(isDMChannel("dm-ceo", {})).toEqual({ agentSlug: "ceo" });
    expect(isDMChannel("dm-human-ceo", {})).toEqual({ agentSlug: "ceo" });
  });

  it("resets navigation and onboarding state for a shred flow", () => {
    useAppStore.setState({
      currentChannel: "ceo__human",
      currentApp: "settings",
      activeThreadId: "thread-1",
      lastMessageId: "msg-1",
      activeAgentSlug: "ceo",
      searchOpen: true,
      composerSearchInitialQuery: "stuck task",
      composerHelpOpen: true,
      onboardingComplete: true,
      wikiPath: "companies/acme",
      wikiLookupQuery: "who owns renewal?",
      notebookAgentSlug: "ceo",
      notebookEntrySlug: "handoff",
    });

    useAppStore.getState().resetForOnboarding();

    expect(useAppStore.getState()).toMatchObject({
      currentChannel: "general",
      currentApp: null,
      activeThreadId: null,
      lastMessageId: null,
      activeAgentSlug: null,
      searchOpen: false,
      composerSearchInitialQuery: "",
      composerHelpOpen: false,
      onboardingComplete: false,
      wikiPath: null,
      wikiLookupQuery: null,
      notebookAgentSlug: null,
      notebookEntrySlug: null,
    });
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
