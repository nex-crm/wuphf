import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
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

import { useFirstRunNudge } from "../../hooks/useFirstRunNudge";
import { useOfficeMembers } from "../../hooks/useMembers";
import { AgentList } from "./AgentList";

const useOfficeMembersMock = vi.mocked(useOfficeMembers);
const useFirstRunNudgeMock = vi.mocked(useFirstRunNudge);

function setMembers(members: OfficeMember[]) {
  useOfficeMembersMock.mockReturnValue({
    data: members,
    isLoading: false,
    isError: false,
    error: null,
  } as unknown as ReturnType<typeof useOfficeMembers>);
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
});
