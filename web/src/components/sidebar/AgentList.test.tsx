import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { OfficeMember } from "../../api/client";
import { useAppStore } from "../../stores/app";

// Mock the data hooks BEFORE importing AgentList so the module under test
// picks up the mocked module bindings.
vi.mock("../../hooks/useMembers", () => ({
  useOfficeMembers: vi.fn(),
  useOfficeMembersMeta: vi.fn(),
  useChannelMembers: vi.fn(),
}));

vi.mock("../../hooks/useFirstRunNudge", () => ({
  useFirstRunNudge: vi.fn(),
}));

vi.mock("../../hooks/useOverflow", () => ({
  useOverflow: () => ({ current: null }),
}));

vi.mock("../../hooks/useConfig", () => ({
  useDefaultHarness: () => "claude-code",
}));

vi.mock("../../routes/useCurrentRoute", () => ({
  useCurrentRoute: () => ({ kind: "unknown" }),
}));

vi.mock("../agents/AgentWizard", () => ({
  AgentWizard: () => null,
  useAgentWizard: () => ({ open: false, show: () => {}, hide: () => {} }),
}));

vi.mock("../ui/PixelAvatar", () => ({
  PixelAvatar: () => null,
}));

vi.mock("../ui/HarnessBadge", () => ({
  HarnessBadge: () => null,
}));

// AgentEventPeek renders into document.body via createPortal and pulls in
// timers — stub it out for AgentList's integration tests so we focus on
// the chevron + row wiring contract here. AgentEventPeek has its own
// dedicated test file.
vi.mock("./AgentEventPeek", () => ({
  AgentEventPeek: () => null,
}));

// Mock useAgentEventPeek so the per-row peek state is deterministic. We
// flip `isOpen` in tests by tracking calls to `toggle` via the mock
// implementation rather than going through the real Zustand store.
vi.mock("../../hooks/useAgentEventPeek", () => {
  const peekState = {
    isOpen: false,
    current: undefined,
    history: [],
    open: vi.fn(),
    close: vi.fn(),
    toggle: vi.fn(),
    hoverHandlers: { onMouseEnter: vi.fn(), onMouseLeave: vi.fn() },
    longPressHandlers: {
      onTouchStart: vi.fn(),
      onTouchEnd: vi.fn(),
      onTouchCancel: vi.fn(),
      onTouchMove: vi.fn(),
    },
  };
  return {
    useAgentEventPeek: vi.fn(() => peekState),
    usePeekIsOpen: vi.fn(() => false),
  };
});

import { useAgentEventPeek } from "../../hooks/useAgentEventPeek";
import { useFirstRunNudge } from "../../hooks/useFirstRunNudge";
import { useOfficeMembers } from "../../hooks/useMembers";
import { AgentList } from "./AgentList";

const useOfficeMembersMock = vi.mocked(useOfficeMembers);
const useFirstRunNudgeMock = vi.mocked(useFirstRunNudge);
const useAgentEventPeekMock = vi.mocked(useAgentEventPeek);

function setMembers(members: OfficeMember[]) {
  useOfficeMembersMock.mockReturnValue({
    data: members,
    isLoading: false,
    isError: false,
    error: null,
  } as unknown as ReturnType<typeof useOfficeMembers>);
}

function defaultPeekState(overrides: Partial<{ isOpen: boolean }> = {}) {
  return {
    isOpen: false,
    current: undefined,
    history: [],
    open: vi.fn(),
    close: vi.fn(),
    toggle: vi.fn(),
    hoverHandlers: { onMouseEnter: vi.fn(), onMouseLeave: vi.fn() },
    longPressHandlers: {
      onTouchStart: vi.fn(),
      onTouchEnd: vi.fn(),
      onTouchCancel: vi.fn(),
      onTouchMove: vi.fn(),
    },
    ...overrides,
  } as unknown as ReturnType<typeof useAgentEventPeek>;
}

function renderList() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <AgentList />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  useFirstRunNudgeMock.mockReturnValue({ showNudge: false });
  useAgentEventPeekMock.mockImplementation(() => defaultPeekState());
  useAppStore.setState({ agentActivitySnapshots: {} });
});

afterEach(() => {
  vi.clearAllMocks();
  useAppStore.setState({ agentActivitySnapshots: {} });
});

describe("<AgentList>", () => {
  it("renders all agent rows when snapshots are present", () => {
    setMembers([
      { slug: "tess", name: "Tess", role: "engineer", task: "watching tests" },
      { slug: "ava", name: "Ava", role: "designer", task: "moving pixels" },
    ]);

    useAppStore.setState({
      agentActivitySnapshots: {
        tess: {
          slug: "tess",
          activity: "drafting reply",
          kind: "routine",
          receivedAtMs: Date.now() - 5000,
          haloUntilMs: Date.now() - 4400,
        },
        ava: {
          slug: "ava",
          activity: "tweaking spacing",
          kind: "routine",
          receivedAtMs: Date.now() - 5000,
          haloUntilMs: Date.now() - 4400,
        },
      },
    });

    const { container } = renderList();
    const rows = container.querySelectorAll(".sidebar-agent-row");
    expect(rows.length).toBe(2);

    const pills = container.querySelectorAll(".sidebar-agent-pill");
    expect(pills.length).toBe(2);
    expect(pills[0].textContent).toBe("drafting reply");
    expect(pills[1].textContent).toBe("tweaking spacing");
  });

  it("REGRESSION: renders rows correctly with zero SSE snapshots, falling back to member.task", () => {
    // No SSE deployment OR initial paint before first activity event.
    // Pill must NOT render empty — it falls back to the task seed.
    setMembers([
      { slug: "tess", name: "Tess", role: "engineer", task: "watching tests" },
    ]);

    const { container } = renderList();

    const pill = container.querySelector(".sidebar-agent-pill");
    expect(pill).not.toBeNull();
    expect(pill?.textContent).toBe("watching tests");
    // Idle data-state because no snapshot — but with visible fallback text.
    expect(pill?.getAttribute("data-state")).toBe("idle");
  });

  it("renders Office-voice idle copy when no SSE snapshot AND no member.task", () => {
    // Tutorial 3 acceptance: rail must render Office voice immediately, never
    // a blank pill.
    setMembers([{ slug: "devon", name: "Devon", role: "engineer" }]);

    const { container } = renderList();

    const pill = container.querySelector(".sidebar-agent-pill");
    expect(pill).not.toBeNull();
    expect(pill?.textContent?.length ?? 0).toBeGreaterThan(0);
  });

  it("starts the shared 1Hz scheduler exactly once per AgentList mount", () => {
    const setIntervalSpy = vi.spyOn(globalThis, "setInterval");
    setMembers([
      { slug: "tess", name: "Tess", role: "engineer", task: "watching tests" },
      { slug: "ava", name: "Ava", role: "designer", task: "moving pixels" },
      { slug: "sam", name: "Sam", role: "pm", task: "combing Linear" },
    ]);

    renderList();

    // ONE setInterval for the whole rail — not one per row. This is the
    // explicit C2 contract from eng review (per-agent timers would fan out
    // into a CPU drag at 10+ agents).
    expect(setIntervalSpy).toHaveBeenCalledTimes(1);
  });

  it("CRITICAL: scheduler clears its interval on AgentList unmount (no timer leak)", () => {
    const clearIntervalSpy = vi.spyOn(globalThis, "clearInterval");
    setMembers([
      { slug: "tess", name: "Tess", role: "engineer", task: "watching tests" },
    ]);

    const { unmount } = renderList();
    unmount();

    // Without the cleanup, the interval keeps ticking after AgentList is
    // gone (dev hot-reload, route changes, multi-tab) — the eng review
    // flagged this as the CRITICAL test gap to backfill.
    expect(clearIntervalSpy).toHaveBeenCalled();
  });

  it("renders the first-run nudge under the FIRST agent row only when showNudge=true", () => {
    useFirstRunNudgeMock.mockReturnValue({ showNudge: true });
    setMembers([
      {
        slug: "devon",
        name: "Devon",
        role: "engineer",
        task: "watching tests",
      },
      { slug: "lila", name: "Lila", role: "designer", task: "moving pixels" },
    ]);

    const { container } = renderList();

    const nudges = container.querySelectorAll(
      '[data-testid="first-run-nudge"]',
    );
    expect(nudges.length).toBe(1);
    expect(nudges[0].textContent).toBe("→ tag @devon in #general");

    // Confirm the nudge is anchored to the first row, not the second.
    const [firstRow] = container.querySelectorAll(".sidebar-agent-row");
    const [firstNudge] = nudges;
    expect(firstRow.contains(firstNudge)).toBe(true);
  });

  it("does NOT render the nudge when showNudge=false", () => {
    useFirstRunNudgeMock.mockReturnValue({ showNudge: false });
    setMembers([
      {
        slug: "devon",
        name: "Devon",
        role: "engineer",
        task: "watching tests",
      },
    ]);

    const { container } = renderList();
    expect(
      container.querySelectorAll('[data-testid="first-run-nudge"]').length,
    ).toBe(0);
  });

  // ─── Tier 2 chevron / peek wiring ────────────────────────────────────────

  it("renders a peek chevron for every agent row, collapsed by default", () => {
    setMembers([
      { slug: "tess", name: "Tess", role: "engineer", task: "watching tests" },
      { slug: "ava", name: "Ava", role: "designer", task: "moving pixels" },
    ]);

    const { container } = renderList();
    const triggers = container.querySelectorAll(".sidebar-agent-peek-trigger");
    expect(triggers.length).toBe(2);
    for (const t of triggers) {
      expect(t.getAttribute("aria-expanded")).toBe("false");
      expect(t.getAttribute("aria-haspopup")).toBe("dialog");
    }
  });

  it("REGRESSION: button[data-agent-slug] still resolves to the row button (NOT the chevron)", () => {
    // The e2e harness selects rows via `button[data-agent-slug]`. The
    // chevron is a sibling button without that attribute; it uses
    // data-testid instead.
    setMembers([
      { slug: "tess", name: "Tess", role: "engineer", task: "watching tests" },
    ]);

    const { container } = renderList();

    const slugButtons = container.querySelectorAll("button[data-agent-slug]");
    expect(slugButtons.length).toBe(1);
    expect(slugButtons[0].classList.contains("sidebar-agent")).toBe(true);

    const chevron = container.querySelector(
      '[data-testid="peek-trigger-tess"]',
    );
    expect(chevron).not.toBeNull();
    expect(chevron?.hasAttribute("data-agent-slug")).toBe(false);
  });

  it("clicking the chevron does NOT call setActiveAgentSlug (e.stopPropagation)", () => {
    const setActiveAgentSlug = vi.fn();
    useAppStore.setState({ setActiveAgentSlug });

    setMembers([
      { slug: "tess", name: "Tess", role: "engineer", task: "watching tests" },
    ]);

    const { container } = renderList();
    const chevron = container.querySelector(
      '[data-testid="peek-trigger-tess"]',
    );
    expect(chevron).not.toBeNull();
    fireEvent.click(chevron as Element);

    expect(setActiveAgentSlug).not.toHaveBeenCalled();
  });

  it("clicking the chevron calls peek.toggle and flips aria-expanded to true on the next render", () => {
    const toggle = vi.fn();
    let isOpen = false;
    useAgentEventPeekMock.mockImplementation(() =>
      defaultPeekState({ isOpen }),
    );

    setMembers([
      { slug: "tess", name: "Tess", role: "engineer", task: "watching tests" },
    ]);

    const { container, rerender } = renderList();
    const chevron = container.querySelector(
      '[data-testid="peek-trigger-tess"]',
    );
    expect(chevron?.getAttribute("aria-expanded")).toBe("false");

    // Rewire mock so the click handler calls our spy AND so the next
    // render reflects the open state.
    useAgentEventPeekMock.mockImplementation(() => {
      const state = defaultPeekState({ isOpen });
      (state as unknown as { toggle: () => void }).toggle = () => {
        toggle();
        isOpen = true;
      };
      return state;
    });

    rerender(
      <QueryClientProvider client={new QueryClient()}>
        <AgentList />
      </QueryClientProvider>,
    );
    const chevron2 = container.querySelector(
      '[data-testid="peek-trigger-tess"]',
    );
    fireEvent.click(chevron2 as Element);
    expect(toggle).toHaveBeenCalledTimes(1);

    rerender(
      <QueryClientProvider client={new QueryClient()}>
        <AgentList />
      </QueryClientProvider>,
    );
    const chevron3 = container.querySelector(
      '[data-testid="peek-trigger-tess"]',
    );
    expect(chevron3?.getAttribute("aria-expanded")).toBe("true");
  });
});
