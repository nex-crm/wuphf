import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { TaskDefinition } from "../../api/tasks";
import { TaskDefinitionView } from "./TaskDefinitionView";

const FULL: TaskDefinition = {
  goal: "Ship the first partner newsletter",
  deliverables: [
    { name: "newsletter draft", format: "markdown in the wiki" },
    { name: "send report" },
  ],
  success_criteria: [
    "Draft approved by the human",
    "newsletter.md exists in the worktree",
  ],
  access_needed: ["mailing-list account"],
  defined_at: "2026-06-10T09:14:00Z",
};

describe("TaskDefinitionView", () => {
  it("renders goal, deliverables with format chips, criteria, and access", () => {
    render(<TaskDefinitionView definition={FULL} />);

    expect(screen.getByText("Goal")).toBeInTheDocument();
    expect(
      screen.getByText("Ship the first partner newsletter"),
    ).toBeInTheDocument();

    expect(screen.getByText("Deliverables")).toBeInTheDocument();
    expect(screen.getByText("newsletter draft")).toBeInTheDocument();
    expect(screen.getByText("markdown in the wiki")).toBeInTheDocument();
    // A deliverable without a format renders the name without a chip.
    expect(screen.getByText("send report")).toBeInTheDocument();

    expect(screen.getByText("Success criteria")).toBeInTheDocument();
    expect(screen.getByText("Draft approved by the human")).toBeInTheDocument();
    expect(
      screen.getByText("newsletter.md exists in the worktree"),
    ).toBeInTheDocument();

    expect(screen.getByText("Access needed")).toBeInTheDocument();
    expect(screen.getByText("mailing-list account")).toBeInTheDocument();
  });

  it("renders only the goal when the optional sections are absent", () => {
    render(
      <TaskDefinitionView definition={{ goal: "Stabilize the auth test" }} />,
    );
    expect(screen.getByText("Stabilize the auth test")).toBeInTheDocument();
    expect(screen.queryByText("Deliverables")).not.toBeInTheDocument();
    expect(screen.queryByText("Success criteria")).not.toBeInTheDocument();
    expect(screen.queryByText("Access needed")).not.toBeInTheDocument();
  });
});
