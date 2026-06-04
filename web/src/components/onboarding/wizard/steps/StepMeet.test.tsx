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

vi.mock("../../../../lib/analytics", () => ({
  recordOnboardingEmailViewed: () => recordOnboardingEmailViewed(),
  recordOnboardingEmailStarted: () => recordOnboardingEmailStarted(),
}));

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

function renderStep(setAnswers = vi.fn()) {
  return render(
    <StepMeet
      active={true}
      answers={makeAnswers()}
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
});
