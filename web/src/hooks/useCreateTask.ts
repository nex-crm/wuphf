import { useMutation, useQueryClient } from "@tanstack/react-query";

import { post } from "../api/client";
import type { Task, TaskResponse } from "../api/tasks";
import { track } from "../lib/analytics";

export interface CreateTaskFormInput {
  title: string;
  details?: string;
  channel: string;
  assignee?: string;
  createdBy?: string;
}

export interface CreateTaskResult {
  task?: Task;
}

/**
 * Mutation wrapper for the primary "new Issue" creation surfaces
 * (dialog, command palette, CEO inline card).
 *
 * Routes through POST /tasks (action=create, task_type=issue) — the SAME
 * path createSubTask uses. Creation is the authorization: the broker
 * lands an owner-set Issue RUNNING (owner dispatched) and an ownerless
 * Issue READY (dispatches on assignment). Parking is a separate,
 * deliberate composer action (/task-plan park=true).
 */
export function useCreateTask() {
  const queryClient = useQueryClient();
  return useMutation<CreateTaskResult, Error, CreateTaskFormInput>({
    mutationFn: async (input) => {
      const response = await post<TaskResponse>("/tasks", {
        action: "create",
        channel: input.channel.trim() || "general",
        title: input.title.trim(),
        details: input.details?.trim() || "",
        owner: input.assignee?.trim() || "",
        created_by: input.createdBy?.trim() || "human",
        task_type: "issue",
      });
      return { task: response.task };
    },
    onSuccess: (_result, input) => {
      track("task_created", {
        source: "inline",
        owner_agent: input.assignee?.trim() || "",
        has_details: !!input.details?.trim(),
        start_mode: "start",
      });
      void queryClient.invalidateQueries({ queryKey: ["issues"] });
      void queryClient.invalidateQueries({ queryKey: ["office-tasks"] });
      void queryClient.invalidateQueries({ queryKey: ["lifecycle"] });
    },
  });
}
