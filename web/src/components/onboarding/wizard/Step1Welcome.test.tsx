import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ONBOARDING_COPY } from "../../../lib/constants";
import { WelcomeStep } from "./Step1Welcome";

describe("WelcomeStep", () => {
  it("renders the configured headline + subhead from ONBOARDING_COPY", () => {
    render(<WelcomeStep onNext={() => {}} />);
    expect(
      screen.getByRole("heading", { name: ONBOARDING_COPY.step1_headline }),
    ).toBeInTheDocument();
    expect(screen.getByText(ONBOARDING_COPY.step1_subhead)).toBeInTheDocument();
  });

  it("fires onNext when the primary CTA is clicked", () => {
    const onNext = vi.fn();
    render(<WelcomeStep onNext={onNext} />);
    fireEvent.click(screen.getByRole("button"));
    expect(onNext).toHaveBeenCalledTimes(1);
  });
});
