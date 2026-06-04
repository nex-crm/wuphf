/**
 * StepMeet tests — the welcome step doubles as the email-capture entry point.
 *
 * Pins the funnel wiring that is easy to break silently:
 *   - the optional email field renders alongside the office name,
 *   - an anonymous "view" ping fires once when the step mounts,
 *   - an anonymous "start" ping fires once, on the first keystroke only,
 *   - typing patches answers.email through setAnswers.
 *
 * The analytics module is mocked, so these assert the call contract, not any
 * network behavior (that lives in lib/analytics.test.ts).
 */

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { OnboardingAnswers } from "../wizardSteps";
import { StepMeet } from "./StepMeet";

const recordOnboardingEmailViewed = vi.fn();
const recordOnboardingEmailStarted = vi.fn();

// Partial mock: spy on the two record functions, but keep the real
// isValidEmail (StepMeet uses it for the non-blocking invalid-email hint).
vi.mock("../../../../lib/analytics", async (importActual) => {
  const actual =
    await importActual<typeof import("../../../../lib/analytics")>();
  return {
    ...actual,
    recordOnboardingEmailViewed: () => recordOnboardingEmailViewed(),
    recordOnboardingEmailStarted: () => recordOnboardingEmailStarted(),
  };
});

function makeAnswers(
  overrides: Partial<OnboardingAnswers> = {},
): OnboardingAnswers {
  return {
    companyName: "",
    ownerName: "",
    ownerRole: "",
    email: "",
    keepInTouch: true,
    blueprintId: "",
    pickedAgents: [],
    startFromScratch: false,
    agentName: "",
    agentInstructions: "",
    firstIssue: "",
    ...overrides,
  };
}

function renderStep(
  setAnswers = vi.fn(),
  answers: Partial<OnboardingAnswers> = {},
) {
  return render(
    <StepMeet
      active={true}
      answers={makeAnswers(answers)}
      setAnswers={setAnswers}
      blueprints={[]}
    />,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
});

afterEach(() => {
  cleanup();
});

describe("StepMeet email capture", () => {
  it("renders the optional email field next to the office name", () => {
    renderStep();
    expect(screen.getByTestId("onboarding-office-name")).toBeTruthy();
    expect(screen.getByTestId("onboarding-owner-email")).toBeTruthy();
  });

  it("fires a single anonymous viewed event on mount", () => {
    renderStep();
    expect(recordOnboardingEmailViewed).toHaveBeenCalledTimes(1);
  });

  it("fires the started event only on the first keystroke", () => {
    const setAnswers = vi.fn();
    renderStep(setAnswers);
    const input = screen.getByTestId("onboarding-owner-email");

    fireEvent.change(input, { target: { value: "m" } });
    fireEvent.change(input, { target: { value: "ma" } });

    expect(recordOnboardingEmailStarted).toHaveBeenCalledTimes(1);
    expect(setAnswers).toHaveBeenLastCalledWith({ email: "ma" });
  });

  it("does not fire the started event for whitespace-only input", () => {
    renderStep();
    fireEvent.change(screen.getByTestId("onboarding-owner-email"), {
      target: { value: "   " },
    });
    expect(recordOnboardingEmailStarted).not.toHaveBeenCalled();
  });

  it("shows the normal hint and no invalid state for an empty field", () => {
    renderStep();
    const input = screen.getByTestId("onboarding-owner-email");
    expect(input.getAttribute("aria-invalid")).toBe("false");
    expect(
      screen.getByTestId("onboarding-owner-email-hint").className,
    ).not.toContain("onboarding-meet-email-hint--invalid");
  });

  it("warns (without gating) when the email is non-empty but invalid", () => {
    renderStep(vi.fn(), { email: "not-an-email" });
    const input = screen.getByTestId("onboarding-owner-email");
    const hint = screen.getByTestId("onboarding-owner-email-hint");
    expect(input.getAttribute("aria-invalid")).toBe("true");
    expect(hint.className).toContain("onboarding-meet-email-hint--invalid");
    expect(hint.textContent).toMatch(/does not look like an email/i);
  });

  it("treats a valid email as not invalid", () => {
    renderStep(vi.fn(), { email: "maya@nex.ai" });
    expect(
      screen.getByTestId("onboarding-owner-email").getAttribute("aria-invalid"),
    ).toBe("false");
  });
});
