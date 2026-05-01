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
  it("maps each readiness status to the expected row state", () => {
    const { container } = renderReady();
    expect(container.querySelector(".readiness-glyph.ready")).toHaveTextContent(
      "✓",
    );
    expect(container.querySelector(".readiness-glyph.next")).toHaveTextContent(
      "—",
    );
    expect(
      container.querySelector(".readiness-glyph.missing"),
    ).toHaveTextContent("!");
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

  it("surfaces submitError and makes Retry submit the same task again", () => {
    const onSubmit = vi.fn();
    renderReady({ submitError: "broker offline", onSubmit });
    expect(screen.getByTestId("onboarding-submit-error")).toHaveTextContent(
      /broker offline/i,
    );
    const retry = screen.getByTestId("onboarding-submit-button");
    expect(retry).toHaveTextContent(/Retry/i);
    fireEvent.click(retry);
    expect(onSubmit).toHaveBeenCalledTimes(1);
  });

  it("Back button fires onBack", () => {
    const onBack = vi.fn();
    renderReady({ onBack });
    fireEvent.click(screen.getByRole("button", { name: /^Back$/i }));
    expect(onBack).toHaveBeenCalledTimes(1);
  });
});
