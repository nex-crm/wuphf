import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ReadyStep } from "./Step7Ready";
import type { ReadinessCheck } from "./types";

const checks: ReadinessCheck[] = [
  { label: "Runtime", status: "ready", detail: "Claude Code installed" },
  { label: "Memory", status: "next", detail: "Markdown wiki" },
  { label: "Nex key", status: "missing", detail: "Skipped — paste later" },
];

function renderReady(
  overrides: Partial<{
    checks: ReadinessCheck[];
    taskText: string;
    submitting: boolean;
    submitError: string;
    onSkip: () => void;
    onSubmit: () => void;
    onBack: () => void;
  }> = {},
) {
  return render(
    <ReadyStep
      checks={overrides.checks ?? checks}
      taskText={overrides.taskText ?? "ship beta"}
      submitting={overrides.submitting ?? false}
      submitError={overrides.submitError ?? ""}
      onSkip={overrides.onSkip ?? (() => {})}
      onSubmit={overrides.onSubmit ?? (() => {})}
      onBack={overrides.onBack ?? (() => {})}
    />,
  );
}

describe("ReadyStep", () => {
  it("renders one row per readiness check", () => {
    renderReady();
    for (const c of checks) {
      expect(screen.getByText(c.label)).toBeInTheDocument();
      expect(screen.getByText(c.detail)).toBeInTheDocument();
    }
  });

  it("with non-empty taskText, the primary CTA fires onSubmit (not onSkip)", () => {
    const onSubmit = vi.fn();
    const onSkip = vi.fn();
    renderReady({ taskText: "ship beta", onSubmit, onSkip });
    fireEvent.click(screen.getByTestId("onboarding-submit-button"));
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onSkip).not.toHaveBeenCalled();
  });

  it("with empty taskText, the primary CTA fires onSkip (not onSubmit)", () => {
    const onSubmit = vi.fn();
    const onSkip = vi.fn();
    renderReady({ taskText: "   ", onSubmit, onSkip });
    fireEvent.click(screen.getByTestId("onboarding-submit-button"));
    expect(onSkip).toHaveBeenCalledTimes(1);
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("renders 'Starting...' and disables the button while submitting", () => {
    renderReady({ submitting: true });
    const cta = screen.getByTestId("onboarding-submit-button");
    expect(cta).toHaveTextContent(/Starting/i);
    expect(cta).toBeDisabled();
  });

  it("surfaces submitError in an alert and switches CTA to 'Retry'", () => {
    renderReady({ submitError: "broker offline" });
    expect(screen.getByTestId("onboarding-submit-error")).toHaveTextContent(
      /broker offline/i,
    );
    expect(screen.getByTestId("onboarding-submit-button")).toHaveTextContent(
      /Retry/i,
    );
  });

  it("Back button fires onBack", () => {
    const onBack = vi.fn();
    renderReady({ onBack });
    fireEvent.click(screen.getByRole("button", { name: /^Back$/i }));
    expect(onBack).toHaveBeenCalledTimes(1);
  });
});
