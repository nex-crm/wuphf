/**
 * nex task — CRUD operations for tasks.
 */

import { program } from "../cli.js";
import { NexClient } from "../lib/client.js";
import { resolveApiKey, resolveFormat, resolveTimeout } from "../lib/config.js";
import { printOutput } from "../lib/output.js";
import type { Format } from "../lib/output.js";

function getClient(): { client: NexClient; format: Format } {
  const opts = program.opts();
  const client = new NexClient(resolveApiKey(opts.apiKey), resolveTimeout(opts.timeout));
  return { client, format: resolveFormat(opts.format) as Format };
}

const task = program.command("task").description("Manage tasks");

task
  .command("list")
  .description("List tasks")
  .option("--entity <id>", "Filter by entity ID")
  .option("--assignee <id>", "Filter by assignee ID")
  .option("--search <query>", "Search query")
  .option("--completed", "Show completed tasks")
  .option("--limit <n>", "Max tasks to return")
  .action(async (opts: { entity?: string; assignee?: string; search?: string; completed?: boolean; limit?: string }) => {
    const { client, format } = getClient();
    const params = new URLSearchParams();
    if (opts.entity) params.set("entity_id", opts.entity);
    if (opts.assignee) params.set("assignee_id", opts.assignee);
    if (opts.search) params.set("search", opts.search);
    if (opts.completed) params.set("is_completed", "true");
    if (opts.limit) params.set("limit", opts.limit);
    const qs = params.toString();
    const result = await client.get(`/v1/tasks${qs ? `?${qs}` : ""}`);
    printOutput(result, format);
  });

task
  .command("get")
  .description("Get a task by ID")
  .argument("<id>", "Task ID")
  .action(async (id: string) => {
    const { client, format } = getClient();
    const result = await client.get(`/v1/tasks/${encodeURIComponent(id)}`);
    printOutput(result, format);
  });

task
  .command("create")
  .description("Create a task")
  .requiredOption("--title <title>", "Task title")
  .option("--description <description>", "Task description")
  .option("--priority <priority>", "Priority: low, medium, high, urgent")
  .option("--due <date>", "Due date")
  .option("--entities <ids>", "Comma-separated entity IDs")
  .option("--assignees <ids>", "Comma-separated assignee IDs")
  .action(async (opts: { title: string; description?: string; priority?: string; due?: string; entities?: string; assignees?: string }) => {
    const { client, format } = getClient();
    const body: Record<string, unknown> = { title: opts.title };
    if (opts.description) body.description = opts.description;
    if (opts.priority) body.priority = opts.priority;
    if (opts.due) body.due_date = opts.due;
    if (opts.entities) body.entity_ids = opts.entities.split(",");
    if (opts.assignees) body.assignee_ids = opts.assignees.split(",");
    const result = await client.post("/v1/tasks", body);
    printOutput(result, format);
  });

task
  .command("update")
  .description("Update a task")
  .argument("<id>", "Task ID")
  .option("--title <title>", "New title")
  .option("--description <description>", "New description")
  .option("--completed", "Mark as completed")
  .option("--no-completed", "Mark as not completed")
  .option("--priority <priority>", "Priority: low, medium, high, urgent")
  .option("--due <date>", "Due date")
  .option("--entities <ids>", "Comma-separated entity IDs")
  .option("--assignees <ids>", "Comma-separated assignee IDs")
  .action(async (id: string, opts: { title?: string; description?: string; completed?: boolean; priority?: string; due?: string; entities?: string; assignees?: string }) => {
    const { client, format } = getClient();
    const body: Record<string, unknown> = {};
    if (opts.title !== undefined) body.title = opts.title;
    if (opts.description !== undefined) body.description = opts.description;
    if (opts.completed !== undefined) body.is_completed = opts.completed;
    if (opts.priority !== undefined) body.priority = opts.priority;
    if (opts.due !== undefined) body.due_date = opts.due;
    if (opts.entities) body.entity_ids = opts.entities.split(",");
    if (opts.assignees) body.assignee_ids = opts.assignees.split(",");
    const result = await client.patch(`/v1/tasks/${encodeURIComponent(id)}`, body);
    printOutput(result, format);
  });

task
  .command("delete")
  .description("Delete a task")
  .argument("<id>", "Task ID")
  .action(async (id: string) => {
    const { client, format } = getClient();
    const result = await client.delete(`/v1/tasks/${encodeURIComponent(id)}`);
    printOutput(result, format);
  });
