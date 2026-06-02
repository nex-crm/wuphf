import { useMutation, useQueryClient } from "@tanstack/react-query";

import { post } from "../api/client";
import type { Task, TaskResponse } from "../api/tasks";

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
 * path createSubTask uses — so the broker applies LifecycleStateDrafting
 * by construction. The previous /task-plan route skipped drafting and
 * landed the task straight at status=in_progress, which silently
 * bypassed the CEO scoping interview (issueScopingFrameworkBlock would
 * see a fully-formed task and never ask the office-hours questions).
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
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["issues"] });
      void queryClient.invalidateQueries({ queryKey: ["office-tasks"] });
      void queryClient.invalidateQueries({ queryKey: ["lifecycle"] });
    },
  });
}
