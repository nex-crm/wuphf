import { render } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { useAppStore } from "../../stores/app";
import { AgentEventPill } from "./AgentEventPill";

afterEach(() => {
  useAppStore.setState({ agentActivitySnapshots: {} });
});

describe("<AgentEventPill>", () => {
  it("renders fallbackTask when no snapshot has arrived yet (Tutorial 3, Priya)", () => {
    const { container } = render(
      <AgentEventPill
        slug="devon"
        agentRole="engineer"
        fallbackTask="reading channel context"
      />,
    );
    const pill = container.querySelector(".sidebar-agent-pill");
    expect(pill).not.toBeNull();
    expect(pill?.textContent).toBe("reading channel context");
    // No snapshot -> derived state is idle, but fallbackTask wins over the
    // idle dictionary on initial paint.
    expect(pill?.getAttribute("data-state")).toBe("idle");
  });

  it("renders role-keyed Office-voice idle copy when there is no snapshot AND no fallback", () => {
    const { container } = render(
      <AgentEventPill slug="devon" agentRole="engineer" />,
    );
    const pill = container.querySelector(".sidebar-agent-pill");
    // Engineer role -> dictionary copy. Just assert the visible text is
    // a non-empty string, since the rotation index depends on Date.now().
    expect(pill?.textContent?.length ?? 0).toBeGreaterThan(0);
    expect(pill?.getAttribute("data-state")).toBe("idle");
  });

  it("renders snapshot.activity in the holding state when a routine snapshot is present", () => {
    useAppStore.setState({
      agentActivitySnapshots: {
        tess: {
          slug: "tess",
          activity: "drafting reply",
          kind: "routine",
          // Past the 600ms halo decay so we land in "holding", not "halo".
          receivedAtMs: Date.now() - 5000,
          haloUntilMs: Date.now() - 4400,
        },
      },
    });

    const { container } = render(
      <AgentEventPill
        slug="tess"
        agentRole="engineer"
        fallbackTask="watching tests"
      />,
    );
    const pill = container.querySelector(".sidebar-agent-pill");
    expect(pill?.textContent).toBe("drafting reply");
    expect(pill?.getAttribute("data-state")).toBe("holding");
  });

  it("renders the stuck variant with bordered chrome and an assertive aria-live announcement", () => {
    useAppStore.setState({
      agentActivitySnapshots: {
        rita: {
          slug: "rita",
          activity: "stuck on terraform lock",
          kind: "stuck",
          receivedAtMs: Date.now(),
          haloUntilMs: 0,
        },
      },
    });

    const { container } = render(
      <AgentEventPill
        slug="rita"
        agentRole="devops"
        fallbackTask="planning compute module"
      />,
    );

    const pill = container.querySelector(".sidebar-agent-pill");
    expect(pill?.getAttribute("data-state")).toBe("stuck");
    expect(pill?.textContent).toBe("stuck on terraform lock");

    const announcement = container.querySelector('[aria-live="assertive"]');
    expect(announcement).not.toBeNull();
    expect(announcement?.textContent).toBe(
      "rita blocked: stuck on terraform lock",
    );
  });

  it("truncates pill text to 48 characters with an ellipsis", () => {
    const longText =
      "this is a very long activity string that should definitely exceed the 48 character cap";
    useAppStore.setState({
      agentActivitySnapshots: {
        sam: {
          slug: "sam",
          activity: longText,
          kind: "routine",
          receivedAtMs: Date.now() - 5000,
          haloUntilMs: Date.now() - 4400,
        },
      },
    });

    const { container } = render(<AgentEventPill slug="sam" agentRole="pm" />);
    const pill = container.querySelector(".sidebar-agent-pill");
    expect(pill?.textContent?.length).toBe(48);
    expect(pill?.textContent?.endsWith("…")).toBe(true);
  });

  it("falls back to generalist Office copy for an unknown role with no snapshot", () => {
    // Tutorial 3, Priya hires `lila` with role "Some Random Title" — must
    // not crash, must not render empty.
    const { container } = render(
      <AgentEventPill slug="lila" agentRole="Some Random Title" />,
    );
    const pill = container.querySelector(".sidebar-agent-pill");
    expect(pill?.textContent?.length ?? 0).toBeGreaterThan(0);
  });
});
