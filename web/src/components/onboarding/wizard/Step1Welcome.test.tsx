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
});
