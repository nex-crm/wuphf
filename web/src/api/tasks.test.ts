import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import * as client from "./client";
import * as api from "./tasks";

describe("tasks api client", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("getOfficeTasks calls the all-channel tasks contract", async () => {
    const response: api.TaskListResponse = { tasks: [] };
    const getSpy = vi.spyOn(client, "get").mockResolvedValue(response);

    await expect(api.getOfficeTasks({ includeDone: true })).resolves.toEqual(
      response,
    );
    expect(getSpy).toHaveBeenCalledWith("/tasks", {
      viewer_slug: "human",
      all_channels: "true",
      include_done: "true",
    });
  });

  it("createTasks posts to task-plan with the human default actor", async () => {
    const response: api.CreateTasksResponse = { tasks: [] };
    const postSpy = vi.spyOn(client, "post").mockResolvedValue(response);

    await expect(
      api.createTasks([{ title: "Write contract", assignee: "pm" }]),
    ).resolves.toEqual(response);
    expect(postSpy).toHaveBeenCalledWith("/task-plan", {
      channel: "general",
      created_by: "human",
      tasks: [{ title: "Write contract", assignee: "pm" }],
    });
  });

  it("updateTaskStatus includes memory workflow override evidence", async () => {
    const response: api.TaskResponse = {
      task: { id: "task-1", title: "Write contract", status: "done" },
    };
    const postSpy = vi.spyOn(client, "post").mockResolvedValue(response);

    await expect(
      api.updateTaskStatus("task-1", "complete", "general", "human", {
        memoryWorkflowOverride: true,
        memoryWorkflowOverrideReason: "manual review complete",
      }),
    ).resolves.toEqual(response);
    expect(postSpy).toHaveBeenCalledWith("/tasks", {
      action: "complete",
      id: "task-1",
      channel: "general",
      created_by: "human",
      memory_workflow_override: true,
      memory_workflow_override_actor: "human",
      memory_workflow_override_reason: "manual review complete",
      override_reason: "manual review complete",
    });
  });

  it("listAgentLogTasks calls agent-logs with a limit query", async () => {
    const response = { tasks: [] satisfies api.TaskLogSummary[] };
    const getSpy = vi.spyOn(client, "get").mockResolvedValue(response);

    await expect(api.listAgentLogTasks({ limit: 25 })).resolves.toEqual(
      response,
    );
    expect(getSpy).toHaveBeenCalledWith("/agent-logs", { limit: "25" });
  });
});
