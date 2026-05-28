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

function defaultPeekState(
  overrides: Partial<ReturnType<typeof useAgentEventPeek>> = {},
) {
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

  // ─── Row + peek wiring ────────────────────────────────────────
  //
  // The Tier-2 chevron trigger was removed in the sidebar redesign; peek is
  // now reachable through hover + long-press only. AgentEventPeek itself,
  // the hook, and the hover/long-press path are still wired — the tests
  // below verify the row-click / peek-close contract that survives.

  it("REGRESSION: button[data-agent-slug] resolves to the row button only", () => {
    // The e2e harness selects rows via `button[data-agent-slug]`. There is
    // no longer a sibling chevron button, so only the .sidebar-agent row
    // should carry that data attribute.
    setMembers([
      { slug: "tess", name: "Tess", role: "engineer", task: "watching tests" },
    ]);

    const { container } = renderList();

    const slugButtons = container.querySelectorAll("button[data-agent-slug]");
    expect(slugButtons.length).toBe(1);
    expect(slugButtons[0].classList.contains("sidebar-agent")).toBe(true);

    // The chevron is gone — its test id should no longer be in the DOM.
    expect(
      container.querySelector('[data-testid="peek-trigger-tess"]'),
    ).toBeNull();
  });

  it("clicking the row (Tier 3 escalation) closes any open peek before navigating", () => {
    // Plan §Disclosure tiers: "Quick activation always wins; the long-press
    // threshold is what differentiates peek from navigate." If peek is open
    // when the user taps the row, the workspace should be the only surface
    // visible after the tap — close the peek as part of escalation.
    const close = vi.fn();
    const setActiveAgentSlug = vi.fn();
    useAppStore.setState({ setActiveAgentSlug });
    useAgentEventPeekMock.mockImplementation(() =>
      defaultPeekState({ isOpen: true, close }),
    );

    setMembers([
      { slug: "tess", name: "Tess", role: "engineer", task: "watching tests" },
    ]);

    const { container } = renderList();
    const row = container.querySelector(
      'button[data-agent-slug="tess"]',
    ) as HTMLButtonElement;
    expect(row).not.toBeNull();
    fireEvent.click(row);

    expect(close).toHaveBeenCalledTimes(1);
    expect(setActiveAgentSlug).toHaveBeenCalledWith("tess");
  });

  // ─── presence badge ──────────────────────────────────────────────────────
  it("renders the online badge on rows whose member has online=true", () => {
    setMembers([
      {
        slug: "tess",
        name: "Tess",
        role: "engineer",
        online: true,
        last_seen_at: "2026-05-07T00:00:00Z",
      },
      {
        slug: "ava",
        name: "Ava",
        role: "designer",
        online: false,
        last_seen_at: "2026-05-06T22:00:00Z",
      },
      // Built-in member without an adapter session — no presence record at all.
      // The badge must not render and the absence of an "offline" marker is
      // intentional: not-connected and never-connected are the same shape on
      // the avatar, only differentiated inside the peek card.
      { slug: "devon", name: "Devon", role: "engineer" },
    ]);

    const { container } = renderList();
    expect(
      container.querySelector('[data-testid="online-badge-tess"]'),
    ).not.toBeNull();
    expect(
      container.querySelector('[data-testid="online-badge-ava"]'),
    ).toBeNull();
    expect(
      container.querySelector('[data-testid="online-badge-devon"]'),
    ).toBeNull();
  });
});
