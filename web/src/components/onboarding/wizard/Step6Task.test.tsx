import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { TaskStep } from "./Step6Task";
import type { TaskTemplate } from "./types";

const templates: TaskTemplate[] = [
  { id: "draft-plan", name: "Draft a plan", description: "kick off planning" },
  {
    id: "first-customer",
    name: "Email first customer",
    description: "send outreach",
    prompt: "Email the first 5 customers about beta",
  },
];

function renderTask(
  overrides: Partial<{
    taskTemplates: TaskTemplate[];
    selectedTaskTemplate: string | null;
    taskText: string;
    submitting: boolean;
    onSelectTaskTemplate: (id: string | null) => void;
    onChangeTaskText: (v: string) => void;
    onNext: () => void;
    onSkip: () => void;
    onBack: () => void;
  }> = {},
) {
  const props = {
    taskTemplates: overrides.taskTemplates ?? templates,
    selectedTaskTemplate: overrides.selectedTaskTemplate ?? null,
    onSelectTaskTemplate: overrides.onSelectTaskTemplate ?? (() => {}),
    taskText: overrides.taskText ?? "",
    onChangeTaskText: overrides.onChangeTaskText ?? (() => {}),
    onNext: overrides.onNext ?? (() => {}),
    onSkip: overrides.onSkip ?? (() => {}),
    onBack: overrides.onBack ?? (() => {}),
    submitting: overrides.submitting ?? false,
  };
  return render(<TaskStep {...props} />);
}

describe("TaskStep", () => {
  it("typing in the textarea fires onChangeTaskText", () => {
    const onChangeTaskText = vi.fn();
    renderTask({ onChangeTaskText });
    fireEvent.change(
      document.querySelector("#wiz-task-input") as HTMLTextAreaElement,
      { target: { value: "ship the thing" } },
    );
    expect(onChangeTaskText).toHaveBeenCalledWith("ship the thing");
  });

  it("clicking a suggestion selects it and fills text from prompt (or name)", () => {
    const onSelectTaskTemplate = vi.fn();
    const onChangeTaskText = vi.fn();
    renderTask({ onSelectTaskTemplate, onChangeTaskText });

    fireEvent.click(screen.getByText("Email first customer"));
    expect(onSelectTaskTemplate).toHaveBeenCalledWith("first-customer");
    // 'first-customer' has a prompt set; that's what should fill the textarea.
    expect(onChangeTaskText).toHaveBeenCalledWith(
      "Email the first 5 customers about beta",
    );
  });

  it("falls back to template name when prompt is missing", () => {
    const onChangeTaskText = vi.fn();
    renderTask({ onChangeTaskText });
    fireEvent.click(screen.getByText("Draft a plan"));
    expect(onChangeTaskText).toHaveBeenCalledWith("Draft a plan");
  });

  it("re-clicking a selected suggestion deselects it (does not refill text)", () => {
    const onSelectTaskTemplate = vi.fn();
    const onChangeTaskText = vi.fn();
    renderTask({
      selectedTaskTemplate: "draft-plan",
      onSelectTaskTemplate,
      onChangeTaskText,
    });
    fireEvent.click(screen.getByText("Draft a plan"));
    expect(onSelectTaskTemplate).toHaveBeenCalledWith(null);
    // No refill on deselect — the user keeps whatever they typed.
    expect(onChangeTaskText).not.toHaveBeenCalled();
  });

  it("disables the Skip button while submitting", () => {
    renderTask({ submitting: true });
    expect(screen.getByRole("button", { name: /Skip/i })).toBeDisabled();
  });

  it("hides the suggestions panel when there are no task templates", () => {
    renderTask({ taskTemplates: [] });
    expect(
      screen.queryByText(/Suggested sequence for this blueprint/i),
    ).not.toBeInTheDocument();
  });
});
