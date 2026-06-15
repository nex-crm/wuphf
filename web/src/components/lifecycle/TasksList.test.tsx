import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import type { OfficeStatsTasks } from "../../api/platform";
import type { Task } from "../../api/tasks";
import type { InboxItem } from "../../lib/types/inbox";
import { TasksList } from "./TasksList";

function makeTask(overrides: Partial<Task>): Task {
  return {
    id: "task-1",
    title: "Task",
    status: "open",
    ...overrides,
  };
}

function renderList(
  tasks: Task[],
  stats?: OfficeStatsTasks,
  inboxItems?: InboxItem[],
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <TasksList
        initialTasks={tasks}
        initialStats={stats}
        initialInboxItems={inboxItems}
      />
    </QueryClientProvider>,
  );
}

describe("<TasksList>", () => {
  it("shows spec-level issue tasks and hides ordinary execution tasks", () => {
    renderList([
      makeTask({
        id: "task-issue",
        title: "Spec the agent issue app",
        task_type: "issue",
      }),
      makeTask({
        id: "task-follow-up",
        title: "Fix button spacing",
        task_type: "follow_up",
      }),
    ]);

    expect(screen.getByText("Spec the agent issue app")).toBeInTheDocument();
    expect(screen.queryByText("Fix button spacing")).not.toBeInTheDocument();
  });

  it("shows the empty state when only small tasks exist", () => {
    renderList([
      makeTask({
        id: "task-small",
        title: "Reply with status",
        task_type: "follow_up",
      }),
    ]);

    expect(screen.getByTestId("issues-list-empty")).toHaveTextContent(
      "No tasks yet.",
    );
    expect(screen.queryByText("Reply with status")).not.toBeInTheDocument();
  });

  it("renders the seven user-facing stage columns", () => {
    renderList([
      makeTask({ id: "task-issue", title: "A spec", task_type: "issue" }),
    ]);

    for (const stage of [
      "scheduled",
      "backlog",
      "in_progress",
      "blocked",
      "needs_human",
      "done",
      "archive",
    ]) {
      expect(
        screen.getByTestId(`issues-kanban-column-${stage}`),
      ).toBeInTheDocument();
    }
  });

  it("groups tasks into their derived stage columns", () => {
    renderList([
      makeTask({
        id: "task-running",
        title: "Running task",
        task_type: "issue",
        lifecycle_state: "running",
      }),
      makeTask({
        id: "task-decision",
        title: "Decision task",
        task_type: "issue",
        lifecycle_state: "decision",
      }),
      makeTask({
        id: "task-archived",
        title: "Archived task",
        task_type: "issue",
        lifecycle_state: "archived",
      }),
      makeTask({
        id: "task-approved",
        title: "Approved task",
        task_type: "issue",
        lifecycle_state: "approved",
      }),
    ]);

    const inProgress = screen.getByTestId("issues-kanban-column-in_progress");
    expect(inProgress).toHaveTextContent("Running task");

    const needsHuman = screen.getByTestId("issues-kanban-column-needs_human");
    expect(needsHuman).toHaveTextContent("Decision task");

    const archive = screen.getByTestId("issues-kanban-column-archive");
    expect(archive).toHaveTextContent("Archived task");

    const done = screen.getByTestId("issues-kanban-column-done");
    expect(done).toHaveTextContent("Approved task");
  });

  it("lane header counts consume the shared /office/stats payload (C1)", () => {
    // One running card locally, but the shared stats payload reports the
    // office-wide truth — the lane header must render the stats number,
    // not a private re-count (the v1 "header 1 blocked vs Blocked lane 0"
    // drift came from two surfaces deriving counts differently).
    renderList(
      [
        makeTask({
          id: "task-running",
          title: "Running task",
          task_type: "issue",
          lifecycle_state: "running",
        }),
      ],
      {
        backlog: 2,
        active: 4,
        blocked: 1,
        review: 1,
        needs_human: 3,
        done: 5,
        archive: 0,
      },
    );

    const countFor = (stage: string) =>
      screen
        .getByTestId(`issues-kanban-column-${stage}`)
        .querySelector(".issues-kanban-column-count")?.textContent;

    expect(countFor("backlog")).toBe("2");
    expect(countFor("in_progress")).toBe("4");
    expect(countFor("blocked")).toBe("1");
    expect(countFor("needs_human")).toBe("3");
    expect(countFor("done")).toBe("5");
    expect(countFor("archive")).toBe("0");
  });

  it("nests sub-tasks under their parent card, in the parent's lane", () => {
    renderList([
      makeTask({
        id: "task-parent",
        title: "Ship the Q3 launch",
        task_type: "issue",
        lifecycle_state: "running",
      }),
      makeTask({
        id: "task-child-a",
        title: "Draft the announcement copy",
        task_type: "issue",
        parent_issue_id: "task-parent",
        // A done child still renders under the parent in the parent's lane,
        // NOT in the Done lane — the parent's lane is where it shows.
        lifecycle_state: "approved",
      }),
      makeTask({
        id: "task-child-b",
        title: "Set up the landing page",
        task_type: "issue",
        parent_issue_id: "task-parent",
        lifecycle_state: "running",
      }),
    ]);

    // Parent sits in the In-progress lane; both children nest under it there.
    const inProgress = screen.getByTestId("issues-kanban-column-in_progress");
    expect(inProgress).toHaveTextContent("Ship the Q3 launch");
    expect(inProgress).toHaveTextContent("Draft the announcement copy");
    expect(inProgress).toHaveTextContent("Set up the landing page");

    // The done child is NOT promoted to the Done lane — it stays nested.
    const done = screen.getByTestId("issues-kanban-column-done");
    expect(done).not.toHaveTextContent("Draft the announcement copy");

    // Children render in the dedicated sub-task list of their parent.
    const subtaskList = screen.getByTestId("issue-subtasks-task-parent");
    expect(subtaskList).toHaveTextContent("Draft the announcement copy");
    expect(subtaskList).toHaveTextContent("Set up the landing page");
    expect(screen.getAllByTestId("issue-subtask-row")).toHaveLength(2);

    // Sub-tasks are never standalone top-level cards.
    expect(screen.getAllByTestId("issue-row")).toHaveLength(1);
  });

  it("treats a whitespace-only parent_issue_id as top-level, not a hidden row", () => {
    // isIssueTask and the parent/child grouping must agree on what counts as a
    // sub-task. A whitespace-only parent_issue_id trims to empty, so it's a
    // top-level Task and must render as a card — never vanish from both the
    // top-level rows and the nested rows.
    renderList([
      makeTask({
        id: "task-ws",
        title: "Whitespace parent id task",
        task_type: "issue",
        parent_issue_id: "   ",
        lifecycle_state: "running",
      }),
    ]);

    expect(screen.getByText("Whitespace parent id task")).toBeInTheDocument();
    expect(screen.getAllByTestId("issue-row")).toHaveLength(1);
    expect(screen.queryByTestId("issue-subtask-row")).not.toBeInTheDocument();
  });

  it("surfaces a parent when only a sub-task matches the filter", async () => {
    const user = userEvent.setup();
    renderList([
      makeTask({
        id: "task-parent",
        title: "Ship the Q3 launch",
        task_type: "issue",
        lifecycle_state: "running",
      }),
      makeTask({
        id: "task-child",
        title: "Wire up Stripe webhooks",
        task_type: "issue",
        parent_issue_id: "task-parent",
        lifecycle_state: "running",
      }),
    ]);

    await user.type(screen.getByTestId("issues-list-search"), "stripe");

    // The parent surfaces because its child matches, and the child is shown.
    expect(screen.getByText("Ship the Q3 launch")).toBeInTheDocument();
    expect(screen.getByText("Wire up Stripe webhooks")).toBeInTheDocument();
  });

  it("folds blocking requests and pending reviews into the Needs-human lane", () => {
    // The standalone Inbox was consolidated into the board: its non-task
    // attention items (agent questions + promotion reviews) render as cards
    // next to the decision-state tasks already in the Needs-human lane, and
    // the lane header count includes them.
    const inboxItems: InboxItem[] = [
      {
        kind: "request",
        requestId: "req-1",
        title: "Approve the Q3 budget?",
        request: {
          kind: "decision",
          question: "Approve the Q3 budget?",
          from: "ceo",
          blocking: true,
        },
      },
      {
        kind: "review",
        reviewId: "rev-1",
        title: "Promote onboarding playbook",
        review: {
          state: "pending",
          reviewerSlug: "pam",
          sourceSlug: "alex",
          targetPath: "playbooks/onboarding.md",
        },
      },
    ];

    renderList(
      [
        makeTask({
          id: "task-decision",
          title: "Decision task",
          task_type: "issue",
          lifecycle_state: "decision",
        }),
      ],
      {
        backlog: 0,
        active: 0,
        blocked: 0,
        review: 0,
        needs_human: 1,
        done: 0,
        archive: 0,
      },
      inboxItems,
    );

    const needsHuman = screen.getByTestId("issues-kanban-column-needs_human");
    expect(needsHuman).toHaveTextContent("Decision task");
    expect(needsHuman).toHaveTextContent("Approve the Q3 budget?");
    expect(needsHuman).toHaveTextContent("Promote onboarding playbook");
    expect(screen.getByTestId("attention-request-row")).toBeInTheDocument();
    expect(screen.getByTestId("attention-review-row")).toBeInTheDocument();

    // 1 decision task (from stats) + 2 folded attention items.
    const count = needsHuman.querySelector(
      ".issues-kanban-column-count",
    )?.textContent;
    expect(count).toBe("3");
  });
});
