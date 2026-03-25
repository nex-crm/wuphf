/**
 * wuphf task — CRUD operations for tasks.
 */

import { program } from "../cli.js";
import { NexClient } from "../lib/client.js";
import { resolveApiKey, resolveFormat, resolveTimeout } from "../lib/config.js";
import { printOutput } from "../lib/output.js";
import type { Format } from "../lib/output.js";
import { heading, style } from "../lib/tui.js";

function getClient(): { client: NexClient; format: Format } {
  const opts = program.opts();
  const client = new NexClient(resolveApiKey(opts.apiKey), resolveTimeout(opts.timeout));
  return { client, format: resolveFormat(opts.format) as Format };
}

interface TaskEntry {
  id?: string | number;
  title?: string;
  is_completed?: boolean;
  priority?: string;
  due_date?: string;
  [k: string]: unknown;
}

function formatTaskListTTY(data: unknown): string | undefined {
  if (!Array.isArray(data)) return undefined;
  const tasks = data as TaskEntry[];

  if (tasks.length === 0) {
    return `  ${style.dim("No tasks found.")}`;
  }

  const lines: string[] = [];
  lines.push(heading("Tasks"));
  lines.push("");

  for (const t of tasks) {
    const check = t.is_completed ? style.dim("\u2611") : "\u2610";
    const title = t.is_completed ? style.dim(t.title ?? "") : (t.title ?? "");
    const parts = [`  ${check} ${title}`];

    if (t.due_date) parts.push(`  ${style.dim("due: " + t.due_date)}`);
    if (t.priority) parts.push(`  ${style.dim("priority: " + t.priority)}`);

    lines.push(parts.join(""));
  }

  const completed = tasks.filter((t) => t.is_completed).length;
  lines.push("");
  lines.push(`  ${style.dim(`${tasks.length} task${tasks.length !== 1 ? "s" : ""} (${completed} completed)`)}`);

  return lines.join("\n");
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
    printOutput(result, format, formatTaskListTTY);
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
