/**
 * TaskActivityFeed — the Activity rail is a STATE-CHANGE AUDIT, not a second
 * copy of the chat. It must surface only lifecycle transitions, human-interview
 * requests, and sub-issue creations. Comments live in the chat stream
 * (TaskCommentCard) and generic `action` log entries are noise, so both are
 * filtered out of the feed.
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { TaskActivityEvent } from "../../api/tasks";

const tasksApi = vi.hoisted(() => ({ getTaskActivity: vi.fn() }));

vi.mock("../../api/tasks", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/tasks")>("../../api/tasks");
  return { ...actual, getTaskActivity: tasksApi.getTaskActivity };
});

// Imported after the mock is registered so the component binds the stub.
const { TaskActivityFeed } = await import("./TaskActivityFeed");

const EVENTS: TaskActivityEvent[] = [
  {
    id: "ev-lifecycle",
    kind: "lifecycle",
    timestamp: "2026-06-09T10:00:00Z",
    actor: "ceo",
    summary: "AUDIT_LIFECYCLE_LINE",
    lifecycle: { from: "drafting", to: "running" },
  },
  {
    id: "ev-request",
    kind: "request",
    timestamp: "2026-06-09T10:01:00Z",
    actor: "engineer",
    summary: "AUDIT_REQUEST_LINE",
    request: { request_id: "req-1", status: "open", question: "Approve?" },
  },
  {
    id: "ev-subissue",
    kind: "sub_issue",
    timestamp: "2026-06-09T10:02:00Z",
    actor: "ceo",
    summary: "AUDIT_SUBISSUE_LINE",
    sub_issue: { sub_issue_id: "task-9", title: "Child task" },
  },
  {
    id: "ev-turn",
    kind: "turn",
    timestamp: "2026-06-09T10:02:30Z",
    actor: "engineer",
    summary: "AUDIT_TURN_LINE",
    context_used: ["learning:l-7", "wiki:companies/acme"],
  },
  {
    id: "ev-comment",
    kind: "comment",
    timestamp: "2026-06-09T10:03:00Z",
    actor: "human",
    summary: "CHAT_ONLY_COMMENT_LINE",
  },
  {
    id: "ev-action",
    kind: "action",
    timestamp: "2026-06-09T10:04:00Z",
    actor: "ceo",
    summary: "GENERIC_ACTION_LINE",
  },
];

function renderFeed() {
  const client = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
        refetchInterval: false,
        refetchOnWindowFocus: false,
        refetchOnReconnect: false,
        refetchOnMount: false,
      },
    },
  });
  return render(
    <QueryClientProvider client={client}>
      <TaskActivityFeed taskId="task-1" />
    </QueryClientProvider>,
  );
}

describe("<TaskActivityFeed>", () => {
  afterEach(() => {
    tasksApi.getTaskActivity.mockReset();
  });

  it("shows lifecycle, request, and sub-issue events", async () => {
    tasksApi.getTaskActivity.mockResolvedValue({
      task_id: "task-1",
      events: EVENTS,
    });
    renderFeed();

    expect(await screen.findByText("AUDIT_LIFECYCLE_LINE")).toBeInTheDocument();
    expect(screen.getByText("AUDIT_REQUEST_LINE")).toBeInTheDocument();
    expect(screen.getByText("AUDIT_SUBISSUE_LINE")).toBeInTheDocument();
  });

  it("shows turn events with the context-used manifest (B4 transparency)", async () => {
    tasksApi.getTaskActivity.mockResolvedValue({
      task_id: "task-1",
      events: EVENTS,
    });
    renderFeed();

    expect(await screen.findByText("AUDIT_TURN_LINE")).toBeInTheDocument();
    expect(
      screen.getByText(/context: learning:l-7, wiki:companies\/acme/),
    ).toBeInTheDocument();
  });

  it("hides comments — they belong in the chat, not the audit", async () => {
    tasksApi.getTaskActivity.mockResolvedValue({
      task_id: "task-1",
      events: EVENTS,
    });
    renderFeed();

    // Wait for the kept events to land before asserting absence.
    await screen.findByText("AUDIT_LIFECYCLE_LINE");
    expect(
      screen.queryByText("CHAT_ONLY_COMMENT_LINE"),
    ).not.toBeInTheDocument();
  });

  it("hides generic action log entries", async () => {
    tasksApi.getTaskActivity.mockResolvedValue({
      task_id: "task-1",
      events: EVENTS,
    });
    renderFeed();

    await screen.findByText("AUDIT_LIFECYCLE_LINE");
    expect(screen.queryByText("GENERIC_ACTION_LINE")).not.toBeInTheDocument();
  });

  it("shows the empty state when only filtered-out kinds are present", async () => {
    tasksApi.getTaskActivity.mockResolvedValue({
      task_id: "task-1",
      events: EVENTS.filter((e) => e.kind === "comment" || e.kind === "action"),
    });
    renderFeed();

    expect(await screen.findByText(/No activity yet/i)).toBeInTheDocument();
  });

  it("renders lifecycle enums as plain labels, never raw snake_case (E1)", async () => {
    // ICP-eval v3 [17:33:18]: "blocked_on_pr_merge" read as engineering
    // jargon to a RevOps operator on a human surface.
    tasksApi.getTaskActivity.mockResolvedValue({
      task_id: "task-1",
      events: [
        {
          id: "ev-blocked",
          kind: "lifecycle",
          timestamp: "2026-06-09T10:00:00Z",
          actor: "ceo",
          lifecycle: { from: "running", to: "blocked_on_pr_merge" },
        },
      ] satisfies TaskActivityEvent[],
    });
    renderFeed();

    expect(
      await screen.findByText("Blocked on review merge"),
    ).toBeInTheDocument();
    expect(screen.getByText("Running")).toBeInTheDocument();
    expect(screen.queryByText(/blocked_on_pr_merge/)).not.toBeInTheDocument();
  });

  it("never renders raw process exhaust in turn summaries (E1)", async () => {
    // ICP-eval v3 [18:14:10]: raw "signal: killed: signal: killed" lines
    // rendered verbatim in the Activity rail.
    tasksApi.getTaskActivity.mockResolvedValue({
      task_id: "task-1",
      events: [
        {
          id: "ev-killed-turn",
          kind: "turn",
          timestamp: "2026-06-09T10:05:00Z",
          actor: "engineer",
          summary: "signal: killed: signal: killed",
        },
      ] satisfies TaskActivityEvent[],
    });
    renderFeed();

    expect(
      await screen.findByText("Turn was interrupted before finishing."),
    ).toBeInTheDocument();
    expect(screen.queryByText(/signal: killed/)).not.toBeInTheDocument();
  });
});
