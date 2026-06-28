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
      screen.getByRole("button", { name: /see the drafted tool/i }),
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
      screen.getByRole("button", { name: /see the drafted tool/i }),
    ).toBeEnabled();
    expect(
      screen.queryByRole("button", { name: /skip ahead/i }),
    ).not.toBeInTheDocument();
  });
});
