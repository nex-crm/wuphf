// Regression: the call must open un-finished and reveal progressively, not show
// the whole transcript instantly. Previously `revealed` started at SCRIPT.length
// so the call was "done" on mount and "Skip ahead" never rendered.

import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { CallModal } from "./CallModal";

afterEach(cleanup);

describe("CallModal reveal", () => {
  it("opens un-finished: shows Skip ahead and disables the build CTA", () => {
    render(<CallModal onClose={vi.fn()} onBuild={vi.fn()} />);

    expect(
      screen.getByRole("button", { name: /skip ahead/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /build the agent/i }),
    ).toBeDisabled();
  });

  it("reveals only the first line on mount", () => {
    render(<CallModal onClose={vi.fn()} onBuild={vi.fn()} />);

    // The transcript renders one <b> speaker label per revealed line.
    const transcript = document.querySelector(".opr-call-transcript");
    expect(transcript?.querySelectorAll(".opr-call-line").length).toBe(1);
  });

  it("jumps to the end (enabling the CTA) when Skip ahead is clicked", async () => {
    const { default: userEvent } = await import("@testing-library/user-event");
    const user = userEvent.setup();
    render(<CallModal onClose={vi.fn()} onBuild={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: /skip ahead/i }));

    expect(
      screen.getByRole("button", { name: /build the agent/i }),
    ).toBeEnabled();
    expect(
      screen.queryByRole("button", { name: /skip ahead/i }),
    ).not.toBeInTheDocument();
  });
});

// Modify mode: passing a `tool` reframes the call as demonstrating a CHANGE to an
// existing tool — different dialog label, scoped script, and a "See the change"
// CTA — instead of drafting a brand-new tool.
describe("CallModal modify mode", () => {
  it("frames the call around the existing tool", () => {
    render(
      <CallModal
        onClose={vi.fn()}
        onBuild={vi.fn()}
        tool={{ id: "inbound-routing", name: "Inbound routing" }}
      />,
    );

    expect(
      screen.getByRole("dialog", { name: /demo a change to inbound routing/i }),
    ).toBeInTheDocument();
    // The CTA is the modify label, not the build one.
    expect(
      screen.getByRole("button", { name: /update the agent/i }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /build the agent/i }),
    ).not.toBeInTheDocument();
  });

  it("enables the change CTA after Skip ahead", async () => {
    const { default: userEvent } = await import("@testing-library/user-event");
    const user = userEvent.setup();
    render(
      <CallModal
        onClose={vi.fn()}
        onBuild={vi.fn()}
        tool={{ id: "inbound-routing", name: "Inbound routing" }}
      />,
    );

    expect(
      screen.getByRole("button", { name: /update the agent/i }),
    ).toBeDisabled();

    await user.click(screen.getByRole("button", { name: /skip ahead/i }));

    expect(
      screen.getByRole("button", { name: /update the agent/i }),
    ).toBeEnabled();
  });

  it("hands a scoped capture to the AI when the call ends", async () => {
    const { default: userEvent } = await import("@testing-library/user-event");
    const user = userEvent.setup();
    const onBuild = vi.fn();
    render(
      <CallModal
        onClose={vi.fn()}
        onBuild={onBuild}
        tool={{ id: "inbound-routing", name: "Inbound routing" }}
      />,
    );

    await user.click(screen.getByRole("button", { name: /skip ahead/i }));
    // The captured-context readout is visible once the call is done.
    expect(screen.getByText(/captured from your screen/i)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /update the agent/i }));

    expect(onBuild).toHaveBeenCalledTimes(1);
    const capture = onBuild.mock.calls[0][0];
    expect(capture.mode).toBe("modify");
    expect(capture.toolId).toBe("inbound-routing");
    expect(capture.transcript.length).toBeGreaterThan(0);
  });
});
