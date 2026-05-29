import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Message, OfficeMember } from "../../api/client";
import {
  ARTIFACT_SKELETON_RECENCY_WINDOW_MS,
  ArtifactSkeleton,
  shouldShowArtifactSkeleton,
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
    render(<ArtifactSkeleton />);
    expect(screen.getByText("drafting visual…")).toBeInTheDocument();
    expect(screen.getByTestId("artifact-skeleton")).toHaveAttribute(
      "role",
      "status",
    );
  });

  it("respects a custom label so callers can swap copy per surface", () => {
    render(<ArtifactSkeleton label="writing article" />);
    expect(screen.getByText("writing article")).toBeInTheDocument();
  });
});

describe("shouldShowArtifactSkeleton — phrase heuristic", () => {
  it.each([
    ["...full breakdown below.", true],
    ["See the visual artifact below!", true],
    ["More in the article.", true],
    ["Article coming.", true],
    ["Here is the full article.", true],
    ["Full breakdown — coming.", false],
    ["Just FYI, no follow-up.", false],
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

  it("requires the phrase to be at the END of the message, not mid-body", () => {
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
