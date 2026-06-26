import { render, renderHook, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { Message, OfficeMember } from "../../api/client";
import {
  ARTIFACT_SKELETON_RECENCY_WINDOW_MS,
  ArtifactSkeleton,
  shouldShowArtifactSkeleton,
  useArtifactSkeletonTrigger,
} from "./ArtifactSkeleton";

const PM_MEMBER: Pick<OfficeMember, "slug" | "status"> = {
  slug: "pm",
  status: "active",
};

function buildMessage(overrides: Partial<Message> = {}): Message {
  return {
    id: "msg-1",
    from: "pm",
    channel: "general",
    content: "Coffee extraction is a 2-axis problem. Full breakdown below.",
    timestamp: "2026-05-29T12:00:00Z",
    ...overrides,
  };
}

const BASE_NOW = Date.parse("2026-05-29T12:00:10Z"); // 10s after the message

describe("<ArtifactSkeleton>", () => {
  it("renders the placeholder label so it reads as a draft, not real content", () => {
    render(<ArtifactSkeleton figureNumber={42} />);
    expect(screen.getByText("drafting figure")).toBeInTheDocument();
    // FIG label is rendered so the placeholder reads as a technical figure
    // being drafted, matching the artifact aesthetic.
    expect(screen.getByText("FIG_042")).toBeInTheDocument();
    const root = screen.getByTestId("artifact-skeleton");
    expect(root).toHaveAttribute("role", "status");
    expect(root).toHaveAttribute("aria-label", "FIG_042 — drafting figure");
  });

  it("respects a custom label so callers can swap copy per surface", () => {
    render(<ArtifactSkeleton label="writing article" />);
    expect(screen.getByText("writing article")).toBeInTheDocument();
  });
});

describe("shouldShowArtifactSkeleton — phrase heuristic", () => {
  it.each([
    // Deictic promises ("…below", "see the …") fire on their own.
    ["...full breakdown below.", true],
    ["See the visual artifact below!", true],
    ["Rendered below.", true],
    ["Here is the breakdown, rendered below.", true],
    // Lead-in + artifact noun at the end.
    ["More in the article.", true],
    ["Here is the full article.", true],
    ["Drafting the diagram.", true],
    ["Building a chart.", true],
    ["See the figure.", true],
    // Pending verb + an artifact noun earlier in the clause.
    ["Article coming.", true],
    ["The chart is coming.", true],
    ["Diagram incoming.", true],
    ["Full breakdown — coming.", true],
    // Common codex-path closers the agent actually emits.
    ["I put together the full breakdown.", true],
    ["Here's the schematic.", true],
    // No promise — must stay silent.
    ["Just FYI, no follow-up.", false],
    ["Talk soon, the weekend is coming.", false],
    ["I really like this article.", false],
    ["Let me know what you think.", false],
    ["", false],
  ])("matches %j → %s", (tail, expected) => {
    const got = shouldShowArtifactSkeleton({
      message: buildMessage({ content: `Some gist. ${tail}` }),
      newerMessages: [],
      members: [PM_MEMBER],
      nowMs: BASE_NOW,
    });
    expect(got).toBe(expected);
  });

  it("requires the promise to be in the trailing clause, not mid-body", () => {
    const got = shouldShowArtifactSkeleton({
      message: buildMessage({
        content:
          "I'll put the full article below shortly, but first some context here.",
      }),
      newerMessages: [],
      members: [PM_MEMBER],
      nowMs: BASE_NOW,
    });
    expect(got).toBe(false);
  });

  it("ignores an artifact noun buried earlier in a long message", () => {
    const got = shouldShowArtifactSkeleton({
      message: buildMessage({
        content:
          "The chart we discussed last week was wrong. I have fixed the numbers and double-checked the totals against the source spreadsheet.",
      }),
      newerMessages: [],
      members: [PM_MEMBER],
      nowMs: BASE_NOW,
    });
    expect(got).toBe(false);
  });
});

describe("shouldShowArtifactSkeleton — 60s recency window", () => {
  it("shows for fresh (<60s) messages", () => {
    expect(
      shouldShowArtifactSkeleton({
        message: buildMessage(),
        newerMessages: [],
        members: [PM_MEMBER],
        nowMs: BASE_NOW,
      }),
    ).toBe(true);
  });

  it("ages out at exactly the recency window", () => {
    const expiredNow =
      Date.parse("2026-05-29T12:00:00Z") + ARTIFACT_SKELETON_RECENCY_WINDOW_MS;
    expect(
      shouldShowArtifactSkeleton({
        message: buildMessage(),
        newerMessages: [],
        members: [PM_MEMBER],
        nowMs: expiredNow,
      }),
    ).toBe(false);
  });

  it("never shows for a message dated in the future (clock skew safeguard)", () => {
    expect(
      shouldShowArtifactSkeleton({
        message: buildMessage({ timestamp: "2026-05-29T12:01:00Z" }),
        newerMessages: [],
        members: [PM_MEMBER],
        nowMs: BASE_NOW,
      }),
    ).toBe(false);
  });
});

describe("shouldShowArtifactSkeleton — unmount on marker arrival", () => {
  it("unmounts when a newer message from the same agent carries the marker", () => {
    expect(
      shouldShowArtifactSkeleton({
        message: buildMessage(),
        newerMessages: [
          {
            from: "pm",
            content: "Here it is.\n\nvisual-artifact:ra_0123456789abcdef",
          },
        ],
        members: [PM_MEMBER],
        nowMs: BASE_NOW,
      }),
    ).toBe(false);
  });

  it("does NOT unmount for an unrelated newer message from a different agent", () => {
    expect(
      shouldShowArtifactSkeleton({
        message: buildMessage(),
        newerMessages: [
          {
            from: "ceo",
            content: "FYI\n\nvisual-artifact:ra_aaaaaaaaaaaaaaaa",
          },
        ],
        members: [PM_MEMBER, { slug: "ceo", status: "active" }],
        nowMs: BASE_NOW,
      }),
    ).toBe(true);
  });

  it("does not show when the message ALREADY embeds an artifact marker", () => {
    expect(
      shouldShowArtifactSkeleton({
        message: buildMessage({
          content:
            "Coffee extraction below.\n\nvisual-artifact:ra_0123456789abcdef",
        }),
        newerMessages: [],
        members: [PM_MEMBER],
        nowMs: BASE_NOW,
      }),
    ).toBe(false);
  });
});

describe("useArtifactSkeletonTrigger — ticker arming", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  it("does not arm the 5s ticker for a future-dated (clock-skewed) message", () => {
    // A message dated in the future has a negative delta. Without the
    // deltaMs >= 0 guard it would still install a long-lived interval that
    // keeps re-rendering even though shouldShowArtifactSkeleton returns
    // false. Assert no interval is armed at all.
    vi.useFakeTimers();
    const fixedNow = Date.parse("2026-05-29T12:00:00Z");
    vi.setSystemTime(fixedNow);
    const setIntervalSpy = vi.spyOn(globalThis, "setInterval");

    const { result } = renderHook(() =>
      useArtifactSkeletonTrigger({
        enabled: true,
        message: buildMessage({ timestamp: "2026-05-29T12:01:00Z" }),
        channelMessages: [],
        members: [PM_MEMBER as OfficeMember],
      }),
    );

    expect(result.current).toBe(false);
    expect(setIntervalSpy).not.toHaveBeenCalled();
  });

  it("arms the ticker for a fresh (in-window) message", () => {
    vi.useFakeTimers();
    const fixedNow = Date.parse("2026-05-29T12:00:10Z"); // 10s after message
    vi.setSystemTime(fixedNow);
    const setIntervalSpy = vi.spyOn(globalThis, "setInterval");

    renderHook(() =>
      useArtifactSkeletonTrigger({
        enabled: true,
        message: buildMessage(),
        channelMessages: [],
        members: [PM_MEMBER as OfficeMember],
      }),
    );

    expect(setIntervalSpy).toHaveBeenCalled();
  });
});

describe("shouldShowArtifactSkeleton — author guards", () => {
  it("never shows for human messages (from=human)", () => {
    expect(
      shouldShowArtifactSkeleton({
        message: buildMessage({ from: "human" }),
        newerMessages: [],
        members: [{ slug: "human", status: "active" }],
        nowMs: BASE_NOW,
      }),
    ).toBe(false);
  });

  it("never shows for the local user (from=you)", () => {
    expect(
      shouldShowArtifactSkeleton({
        message: buildMessage({ from: "you" }),
        newerMessages: [],
        members: [],
        nowMs: BASE_NOW,
      }),
    ).toBe(false);
  });

  it("never shows for human:<name> author tags", () => {
    expect(
      shouldShowArtifactSkeleton({
        message: buildMessage({ from: "human:nazz" }),
        newerMessages: [],
        members: [],
        nowMs: BASE_NOW,
      }),
    ).toBe(false);
  });

  it("does not show when the authoring agent is not currently active", () => {
    expect(
      shouldShowArtifactSkeleton({
        message: buildMessage(),
        newerMessages: [],
        members: [{ slug: "pm", status: "lurking" }],
        nowMs: BASE_NOW,
      }),
    ).toBe(false);
  });

  it("does not show when the authoring agent is missing from the office roster", () => {
    expect(
      shouldShowArtifactSkeleton({
        message: buildMessage(),
        newerMessages: [],
        members: [],
        nowMs: BASE_NOW,
      }),
    ).toBe(false);
  });
});
