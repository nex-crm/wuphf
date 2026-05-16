import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { FirstTaskScreen } from "./FirstTaskScreen";

describe("FirstTaskScreen", () => {
  it("renders the submitted task text", () => {
    render(
      <FirstTaskScreen
        taskText="Analyze Q1 revenue and draft a summary"
        onWatchTask={vi.fn()}
        onSkipToOffice={vi.fn()}
      />,
    );
    expect(screen.getByTestId("first-task-preview")).toHaveTextContent(
      "Analyze Q1 revenue and draft a summary",
    );
  });

  it("calls onWatchTask when the primary CTA is clicked", async () => {
    const onWatchTask = vi.fn();
    render(
      <FirstTaskScreen
        taskText="Draft weekly report"
        onWatchTask={onWatchTask}
        onSkipToOffice={vi.fn()}
      />,
    );
    await userEvent.click(screen.getByTestId("first-task-watch"));
    expect(onWatchTask).toHaveBeenCalledOnce();
  });

  it("calls onSkipToOffice when the secondary CTA is clicked", async () => {
    const onSkipToOffice = vi.fn();
    render(
      <FirstTaskScreen
        taskText="Draft weekly report"
        onWatchTask={vi.fn()}
        onSkipToOffice={onSkipToOffice}
      />,
    );
    await userEvent.click(screen.getByTestId("first-task-skip"));
    expect(onSkipToOffice).toHaveBeenCalledOnce();
  });

  it("renders a graceful empty state when taskText is empty (PR 3 blocked path)", () => {
    render(
      <FirstTaskScreen
        taskText=""
        onWatchTask={vi.fn()}
        onSkipToOffice={vi.fn()}
      />,
    );
    expect(screen.queryByTestId("first-task-preview")).toBeNull();
    expect(screen.getByTestId("first-task-empty")).toBeInTheDocument();
    expect(screen.getByTestId("first-task-screen")).toHaveTextContent(
      "#general",
    );
  });

  it("renders a graceful empty state when taskText is omitted", () => {
    render(
      <FirstTaskScreen onWatchTask={vi.fn()} onSkipToOffice={vi.fn()} />,
    );
    expect(screen.queryByTestId("first-task-preview")).toBeNull();
    expect(screen.getByTestId("first-task-empty")).toBeInTheDocument();
  });

  it("renders a graceful empty state when taskText is whitespace-only", () => {
    render(
      <FirstTaskScreen
        taskText="   "
        onWatchTask={vi.fn()}
        onSkipToOffice={vi.fn()}
      />,
    );
    expect(screen.queryByTestId("first-task-preview")).toBeNull();
    expect(screen.getByTestId("first-task-empty")).toBeInTheDocument();
  });
});
