import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";
import React from "react";
import { render, cleanup } from "ink-testing-library";
import { TaskBoardView } from "../../../src/tui/views/task-board.js";
import type { TaskCard } from "../../../src/tui/views/task-board.js";

function strip(s: string): string {
  return s.replace(/\x1b\[[0-9;]*m/g, "");
}

afterEach(() => {
  cleanup();
});

const sampleTasks: TaskCard[] = [
  { id: "t1", title: "Design wireframes", priority: "urgent", status: "todo", record: "Acme Corp", due: "Mar 20" },
  { id: "t2", title: "Write API spec", priority: "high", status: "todo", record: "Project X" },
  { id: "t3", title: "Build dashboard", priority: "medium", status: "in_progress", record: "Acme Corp", due: "Mar 25" },
  { id: "t4", title: "Fix login bug", priority: "low", status: "in_progress" },
  { id: "t5", title: "Deploy v1", priority: "high", status: "done", due: "Mar 15" },
  { id: "t6", title: "Setup CI", priority: "low", status: "done" },
];

describe("TaskBoardView", () => {
  // ── Column headers ──

  it("renders all three column headers with icons", () => {
    const { lastFrame } = render(<TaskBoardView tasks={sampleTasks} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("To Do"), "should show To Do column");
    assert.ok(frame.includes("In Progress"), "should show In Progress column");
    assert.ok(frame.includes("Done"), "should show Done column");
  });

  it("shows task counts in column headers", () => {
    const { lastFrame } = render(<TaskBoardView tasks={sampleTasks} />);
    const frame = strip(lastFrame() ?? "");
    // To Do: 2, In Progress: 2, Done: 2
    const matches = frame.match(/\(2\)/g);
    assert.ok(matches && matches.length >= 3, "should show (2) count for each column");
  });

  // ── Title ──

  it("renders board title with total count", () => {
    const { lastFrame } = render(<TaskBoardView tasks={sampleTasks} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Task Board"), "should show board title");
    assert.ok(frame.includes("(6 tasks)"), "should show total task count");
  });

  // ── Task cards ──

  it("renders task titles", () => {
    const { lastFrame } = render(<TaskBoardView tasks={sampleTasks} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Design wireframes"), "should show first task title");
    assert.ok(frame.includes("Build dashboard"), "should show in-progress task title");
    assert.ok(frame.includes("Deploy v1"), "should show done task title");
  });

  it("renders record names on cards", () => {
    const { lastFrame } = render(<TaskBoardView tasks={sampleTasks} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Acme Corp"), "should show record name");
    assert.ok(frame.includes("Project X"), "should show second record name");
  });

  it("renders due dates on cards", () => {
    const { lastFrame } = render(<TaskBoardView tasks={sampleTasks} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Mar 20"), "should show due date");
    assert.ok(frame.includes("Mar 25"), "should show second due date");
  });

  // ── Priority badges ──

  it("renders urgent priority badge (!!!)", () => {
    const { lastFrame } = render(<TaskBoardView tasks={sampleTasks} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("!!!"), "should show !!! for urgent priority");
  });

  it("renders high priority badge (!!)", () => {
    const { lastFrame } = render(
      <TaskBoardView tasks={[{ id: "t1", title: "High task", priority: "high", status: "todo" }]} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("!!"), "should show !! for high priority");
  });

  it("renders medium priority badge (!)", () => {
    const { lastFrame } = render(
      <TaskBoardView tasks={[{ id: "t1", title: "Med task", priority: "medium", status: "in_progress" }]} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("!"), "should show ! for medium priority");
  });

  it("renders low priority badge (\u00B7)", () => {
    const { lastFrame } = render(
      <TaskBoardView tasks={[{ id: "t1", title: "Low task", priority: "low", status: "done" }]} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("\u00B7"), "should show \u00B7 for low priority");
  });

  // ── Empty state ──

  it("shows 'No tasks' when a column is empty", () => {
    const { lastFrame } = render(
      <TaskBoardView tasks={[{ id: "t1", title: "Only todo", priority: "low", status: "todo" }]} />,
    );
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("No tasks"), "should show 'No tasks' for empty columns");
  });

  it("renders empty board with zero tasks", () => {
    const { lastFrame } = render(<TaskBoardView tasks={[]} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("Task Board"), "should still show title");
    assert.ok(frame.includes("(0 tasks)"), "should show 0 count");
    // All columns should show "No tasks"
    const noTasksMatches = frame.match(/No tasks/g);
    assert.ok(noTasksMatches && noTasksMatches.length >= 3, "all three columns should show No tasks");
  });

  // ── Navigation ──

  it("shows escape hint", () => {
    const { lastFrame } = render(<TaskBoardView tasks={[]} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("[Esc=back]"), "should show escape navigation hint");
  });

  // ── Column icons ──

  it("renders column icons", () => {
    const { lastFrame } = render(<TaskBoardView tasks={[]} />);
    const frame = strip(lastFrame() ?? "");
    assert.ok(frame.includes("\u25CB"), "should show \u25CB icon for To Do");
    assert.ok(frame.includes("\u25D4"), "should show \u25D4 icon for In Progress");
    assert.ok(frame.includes("\u25CF"), "should show \u25CF icon for Done");
  });
});
