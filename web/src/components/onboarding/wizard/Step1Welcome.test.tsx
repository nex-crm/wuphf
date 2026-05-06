import type { FormEvent } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ONBOARDING_COPY } from "../../../lib/constants";
import { WelcomeStep } from "./Step1Welcome";

describe("WelcomeStep", () => {
  it("clicking the CTA advances without submitting a surrounding form", () => {
    const onNext = vi.fn();
    const onSubmit = vi.fn((event: FormEvent<HTMLFormElement>) =>
      event.preventDefault(),
    );
    render(
      <form onSubmit={onSubmit}>
        <WelcomeStep onNext={onNext} />
      </form>,
    );

    fireEvent.click(
      screen.getByRole("button", { name: ONBOARDING_COPY.step1_cta }),
    );

    expect(onNext).toHaveBeenCalledTimes(1);
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("does not show the reset link when no draft exists", () => {
    render(<WelcomeStep onNext={vi.fn()} />);
    expect(screen.queryByTestId("welcome-reset-trigger")).toBeNull();
  });

  it("shows reset link when a saved draft exists and confirms inline", () => {
    const onResetDraft = vi.fn();
    render(
      <WelcomeStep
        onNext={vi.fn()}
        hasSavedDraft={true}
        onResetDraft={onResetDraft}
      />,
    );

    const trigger = screen.getByTestId("welcome-reset-trigger");
    fireEvent.click(trigger);

    // Inline confirmation, not window.confirm
    const confirm = screen.getByTestId("welcome-reset-confirm");
    fireEvent.click(confirm);
    expect(onResetDraft).toHaveBeenCalledTimes(1);
  });

  it("cancel returns from confirmation without resetting", () => {
    const onResetDraft = vi.fn();
    render(
      <WelcomeStep
        onNext={vi.fn()}
        hasSavedDraft={true}
        onResetDraft={onResetDraft}
      />,
    );

    fireEvent.click(screen.getByTestId("welcome-reset-trigger"));
    fireEvent.click(screen.getByTestId("welcome-reset-cancel"));
    expect(onResetDraft).not.toHaveBeenCalled();
    expect(screen.getByTestId("welcome-reset-trigger")).toBeInTheDocument();
  });
});
