import { z } from "zod";
export function registerTaskTools(server, client) {
    server.tool("create_task", "Create a new task, optionally linked to records and assigned to users.", {
        title: z.string().min(1).describe("Task title"),
        description: z.string().optional().describe("Task description"),
        priority: z.enum(["low", "medium", "high", "urgent"]).optional().describe("Task priority (default: unspecified)"),
        due_date: z.string().optional().describe("Due date in RFC3339 format"),
        entity_ids: z.array(z.string()).optional().describe("Associated record IDs"),
        assignee_ids: z.array(z.string()).optional().describe("Assigned user IDs"),
    }, { readOnlyHint: false }, async ({ title, description, priority, due_date, entity_ids, assignee_ids }) => {
        const body = { title };
        if (description !== undefined)
            body.description = description;
        if (priority !== undefined)
            body.priority = priority;
        if (due_date !== undefined)
            body.due_date = due_date;
        if (entity_ids)
            body.entity_ids = entity_ids;
        if (assignee_ids)
            body.assignee_ids = assignee_ids;
        const result = await client.post("/v1/tasks", body);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("list_tasks", "List tasks with optional filtering by record, assignee, completion status, or search query.", {
        entity_id: z.string().optional().describe("Filter by associated record ID"),
        assignee_id: z.string().optional().describe("Filter by assignee user ID"),
        search: z.string().optional().describe("Search task titles"),
        is_completed: z.boolean().optional().describe("Filter by completion status"),
        limit: z.number().optional().describe("Max results (1-500, default: 100)"),
        offset: z.number().optional().describe("Pagination offset"),
    }, { readOnlyHint: true }, async ({ entity_id, assignee_id, search, is_completed, limit, offset }) => {
        const params = new URLSearchParams();
        if (entity_id)
            params.set("entity_id", entity_id);
        if (assignee_id)
            params.set("assignee_id", assignee_id);
        if (search)
            params.set("search", search);
        if (is_completed !== undefined)
            params.set("is_completed", String(is_completed));
        if (limit !== undefined)
            params.set("limit", String(limit));
        if (offset !== undefined)
            params.set("offset", String(offset));
        const qs = params.toString();
        const path = `/v1/tasks${qs ? `?${qs}` : ""}`;
        const result = await client.get(path);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("get_task", "Get a single task by ID.", { task_id: z.string().describe("Task ID") }, { readOnlyHint: true }, async ({ task_id }) => {
        const result = await client.get(`/v1/tasks/${encodeURIComponent(task_id)}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("update_task", "Update a task's fields. All fields are optional — only provided fields are changed.", {
        task_id: z.string().describe("Task ID to update"),
        title: z.string().optional().describe("New title"),
        description: z.string().optional().describe("New description (empty string clears it)"),
        priority: z.string().optional().describe("New priority (empty string clears it)"),
        due_date: z.string().optional().describe("New due date in RFC3339 format"),
        is_completed: z.boolean().optional().describe("Mark complete/incomplete"),
        entity_ids: z.array(z.string()).optional().describe("Replace associated record IDs"),
        assignee_ids: z.array(z.string()).optional().describe("Replace assignee user IDs"),
    }, { readOnlyHint: false }, async ({ task_id, title, description, priority, due_date, is_completed, entity_ids, assignee_ids }) => {
        const body = {};
        if (title !== undefined)
            body.title = title;
        if (description !== undefined)
            body.description = description;
        if (priority !== undefined)
            body.priority = priority;
        if (due_date !== undefined)
            body.due_date = due_date;
        if (is_completed !== undefined)
            body.is_completed = is_completed;
        if (entity_ids !== undefined)
            body.entity_ids = entity_ids;
        if (assignee_ids !== undefined)
            body.assignee_ids = assignee_ids;
        const result = await client.patch(`/v1/tasks/${encodeURIComponent(task_id)}`, body);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("delete_task", "Archive a task (soft delete). Cannot be undone via API.", { task_id: z.string().describe("Task ID to delete") }, { readOnlyHint: false, destructiveHint: true }, async ({ task_id }) => {
        const result = await client.delete(`/v1/tasks/${encodeURIComponent(task_id)}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
}
//# sourceMappingURL=tasks.js.map