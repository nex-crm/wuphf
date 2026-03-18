/**
 * Command dispatch layer — executes CLI commands and returns structured results.
 *
 * This is a bridge that lets both the Commander CLI and the Ink TUI execute
 * the same command logic. Instead of printing to stdout, each command returns
 * a CommandResult with formatted output and raw data.
 */

import { NexClient } from "../lib/client.js";
import { resolveApiKey, resolveFormat, resolveTimeout, loadConfig, saveConfig, CONFIG_PATH, BASE_URL } from "../lib/config.js";
import { formatOutput } from "../lib/output.js";
import type { Format } from "../lib/output.js";
import { AuthError, RateLimitError, ServerError } from "../lib/errors.js";
import { parseInput } from "./parse-input.js";
import { getAgentService } from "../tui/services/agent-service.js";
import { runInit, detectPlatforms, getDetected } from "./init.js";
import type { InitProgress } from "./init.js";

// ── Public types ──

export interface CommandResult {
  output: string;
  data?: unknown;
  exitCode: number;
  error?: string;
  sessionId?: string;
  nav?: {
    objectSlug?: string;
    recordId?: string;
  };
}

export interface CommandContext {
  apiKey?: string;
  format?: "json" | "text" | "quiet";
  timeout?: number;
  sessionId?: string;
  debug?: boolean;
}

interface CommandEntry {
  execute: (args: string[], ctx: CommandContext) => Promise<CommandResult>;
  description: string;
  category: "query" | "write" | "config" | "ai" | "graph" | "agent";
  usage?: string;
}

// ── Helpers ──

function makeClient(ctx: CommandContext): NexClient {
  return new NexClient(ctx.apiKey ?? resolveApiKey(), ctx.timeout ?? resolveTimeout());
}

function fmt(data: unknown, ctx: CommandContext): string {
  const format: Format = ctx.format ?? "text";
  return formatOutput(data, format) ?? "";
}

function ok(data: unknown, ctx: CommandContext, extra?: Partial<CommandResult>): CommandResult {
  return { output: fmt(data, ctx), data, exitCode: 0, ...extra };
}

function fail(error: string, exitCode = 1): CommandResult {
  return { output: "", exitCode, error };
}

function wrapError(err: unknown): CommandResult {
  if (err instanceof AuthError) return fail(err.message, 2);
  if (err instanceof RateLimitError) return fail(err.message, 1);
  if (err instanceof ServerError) return fail(err.message, 1);
  if (err instanceof Error) return fail(err.message, 1);
  return fail(String(err), 1);
}

/**
 * Extract named options from an args array.
 * Returns the remaining positional args and an options map.
 * Supports --key value and --flag (boolean).
 */
function extractOpts(args: string[]): { positional: string[]; opts: Record<string, string | true> } {
  const positional: string[] = [];
  const opts: Record<string, string | true> = {};

  for (let i = 0; i < args.length; i++) {
    const arg = args[i];
    if (arg.startsWith("--")) {
      const key = arg.slice(2);
      const next = args[i + 1];
      if (next !== undefined && !next.startsWith("--")) {
        opts[key] = next;
        i++;
      } else {
        opts[key] = true;
      }
    } else {
      positional.push(arg);
    }
  }

  return { positional, opts };
}

// ── Command executors ──

// -- AI commands --

async function executeAsk(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const query = args.join(" ");
  if (!query) return fail("No query provided. Pass a question as argument.");

  try {
    const client = makeClient(ctx);
    const body: Record<string, string> = { query };
    if (ctx.sessionId) body.session_id = ctx.sessionId;

    const result = await client.post<Record<string, unknown>>("/v1/context/ask", body);
    return ok(result, ctx, { sessionId: result.sessionId as string | undefined });
  } catch (err) {
    return wrapError(err);
  }
}

async function executeRemember(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const { positional, opts } = extractOpts(args);
  const content = positional.join(" ");
  if (!content) return fail("No content provided. Pass content as argument.");

  try {
    const client = makeClient(ctx);
    const body: Record<string, string> = { content };
    if (typeof opts.context === "string") body.context = opts.context;

    const result = await client.post("/v1/context/text", body);
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

async function executeRecall(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const query = args.join(" ");
  if (!query) return fail("No query provided. Pass a question as argument.");

  try {
    const client = makeClient(ctx);
    const body: Record<string, string> = { query };
    if (ctx.sessionId) body.session_id = ctx.sessionId;

    const result = await client.post<Record<string, unknown>>("/v1/context/ask", body);
    return ok(result, ctx, { sessionId: result.sessionId as string | undefined });
  } catch (err) {
    return wrapError(err);
  }
}

async function executeArtifact(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const id = args[0];
  if (!id) return fail("No artifact ID provided.");

  try {
    const client = makeClient(ctx);
    const result = await client.get(`/v1/context/artifacts/${encodeURIComponent(id)}`);
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

async function executeCapture(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const content = args.join(" ");
  if (!content) return fail("No content provided.");

  try {
    const client = makeClient(ctx);
    const body: Record<string, string> = { content };
    if (ctx.sessionId) body.session_id = ctx.sessionId;

    const result = await client.post("/v1/context/text", body, 60_000);
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

// -- Query commands --

async function executeSearch(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const query = args.join(" ");
  if (!query) return fail("No search query provided.");

  try {
    const client = makeClient(ctx);
    const result = await client.post("/v1/search", { query });
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

async function executeObjectList(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const { opts } = extractOpts(args);
  try {
    const client = makeClient(ctx);
    const params = opts["include-attributes"] ? "?include_attributes=true" : "";
    const result = await client.get(`/v1/objects${params}`);
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

async function executeObjectGet(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const slug = args[0];
  if (!slug) return fail("No object slug provided.");

  try {
    const client = makeClient(ctx);
    const result = await client.get(`/v1/objects/${encodeURIComponent(slug)}`);
    return ok(result, ctx, { nav: { objectSlug: slug } });
  } catch (err) {
    return wrapError(err);
  }
}

async function executeObjectCreate(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const { opts } = extractOpts(args);
  const name = opts.name;
  const slug = opts.slug;
  if (typeof name !== "string" || typeof slug !== "string") {
    return fail("Required: --name <name> --slug <slug>");
  }

  try {
    const client = makeClient(ctx);
    const body: Record<string, unknown> = { name, slug };
    if (typeof opts.type === "string") body.type = opts.type;
    if (typeof opts.description === "string") body.description = opts.description;
    const result = await client.post("/v1/objects", body);
    return ok(result, ctx, { nav: { objectSlug: slug } });
  } catch (err) {
    return wrapError(err);
  }
}

async function executeObjectUpdate(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const { positional, opts } = extractOpts(args);
  const slug = positional[0];
  if (!slug) return fail("No object slug provided.");

  try {
    const client = makeClient(ctx);
    const body: Record<string, unknown> = {};
    if (typeof opts.name === "string") body.name = opts.name;
    if (typeof opts.description === "string") body.description = opts.description;
    if (typeof opts["name-plural"] === "string") body.name_plural = opts["name-plural"];
    const result = await client.patch(`/v1/objects/${encodeURIComponent(slug)}`, body);
    return ok(result, ctx, { nav: { objectSlug: slug } });
  } catch (err) {
    return wrapError(err);
  }
}

async function executeObjectDelete(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const slug = args[0];
  if (!slug) return fail("No object slug provided.");

  try {
    const client = makeClient(ctx);
    const result = await client.delete(`/v1/objects/${encodeURIComponent(slug)}`);
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

async function executeRecordList(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const { positional, opts } = extractOpts(args);
  const objectSlug = positional[0];
  if (!objectSlug) return fail("No object slug provided.");

  try {
    const client = makeClient(ctx);
    const body: Record<string, unknown> = { attributes: opts.attributes ?? "primary" };
    if (typeof opts.limit === "string") body.limit = parseInt(opts.limit, 10);
    if (typeof opts.offset === "string") body.offset = parseInt(opts.offset, 10);
    if (typeof opts.sort === "string") {
      const [attribute, direction] = opts.sort.split(":");
      body.sort = { attribute, direction };
    }
    const result = await client.post(`/v1/objects/${encodeURIComponent(objectSlug)}/records`, body);
    return ok(result, ctx, { nav: { objectSlug } });
  } catch (err) {
    return wrapError(err);
  }
}

async function executeRecordGet(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const id = args[0];
  if (!id) return fail("No record ID provided.");

  try {
    const client = makeClient(ctx);
    const result = await client.get(`/v1/records/${encodeURIComponent(id)}`);
    return ok(result, ctx, { nav: { recordId: id } });
  } catch (err) {
    return wrapError(err);
  }
}

async function executeRecordCreate(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const { positional, opts } = extractOpts(args);
  const objectSlug = positional[0];
  if (!objectSlug) return fail("No object slug provided.");
  if (typeof opts.data !== "string") return fail("Required: --data <json>");

  let attributes: unknown;
  try {
    attributes = JSON.parse(opts.data);
  } catch {
    return fail(`Invalid JSON for --data: ${opts.data}`);
  }

  try {
    const client = makeClient(ctx);
    const result = await client.post(`/v1/objects/${encodeURIComponent(objectSlug)}`, { attributes });
    return ok(result, ctx, { nav: { objectSlug } });
  } catch (err) {
    return wrapError(err);
  }
}

async function executeRecordUpsert(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const { positional, opts } = extractOpts(args);
  const objectSlug = positional[0];
  if (!objectSlug) return fail("No object slug provided.");
  if (typeof opts.match !== "string") return fail("Required: --match <attr>");
  if (typeof opts.data !== "string") return fail("Required: --data <json>");

  let attributes: unknown;
  try {
    attributes = JSON.parse(opts.data);
  } catch {
    return fail(`Invalid JSON for --data: ${opts.data}`);
  }

  try {
    const client = makeClient(ctx);
    const result = await client.put(`/v1/objects/${encodeURIComponent(objectSlug)}`, {
      matching_attribute: opts.match,
      attributes,
    });
    return ok(result, ctx, { nav: { objectSlug } });
  } catch (err) {
    return wrapError(err);
  }
}

async function executeRecordUpdate(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const { positional, opts } = extractOpts(args);
  const id = positional[0];
  if (!id) return fail("No record ID provided.");
  if (typeof opts.data !== "string") return fail("Required: --data <json>");

  let attributes: unknown;
  try {
    attributes = JSON.parse(opts.data);
  } catch {
    return fail(`Invalid JSON for --data: ${opts.data}`);
  }

  try {
    const client = makeClient(ctx);
    const result = await client.patch(`/v1/records/${encodeURIComponent(id)}`, { attributes });
    return ok(result, ctx, { nav: { recordId: id } });
  } catch (err) {
    return wrapError(err);
  }
}

async function executeRecordDelete(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const id = args[0];
  if (!id) return fail("No record ID provided.");

  try {
    const client = makeClient(ctx);
    const result = await client.delete(`/v1/records/${encodeURIComponent(id)}`);
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

async function executeRecordTimeline(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const { positional, opts } = extractOpts(args);
  const id = positional[0];
  if (!id) return fail("No record ID provided.");

  try {
    const client = makeClient(ctx);
    const params = new URLSearchParams();
    if (typeof opts.limit === "string") params.set("limit", opts.limit);
    if (typeof opts.cursor === "string") params.set("cursor", opts.cursor);
    const qs = params.toString();
    const result = await client.get(`/v1/records/${encodeURIComponent(id)}/timeline${qs ? `?${qs}` : ""}`);
    return ok(result, ctx, { nav: { recordId: id } });
  } catch (err) {
    return wrapError(err);
  }
}

// -- Insight commands --

async function executeInsightList(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const { opts } = extractOpts(args);

  try {
    const client = makeClient(ctx);
    const params = new URLSearchParams();
    if (typeof opts.last === "string") params.set("last", opts.last);
    if (typeof opts.from === "string") params.set("from", opts.from);
    if (typeof opts.to === "string") params.set("to", opts.to);
    if (typeof opts.limit === "string") params.set("limit", opts.limit);
    const qs = params.toString();
    const result = await client.get(`/v1/insights${qs ? `?${qs}` : ""}`);
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

// -- Note commands --

async function executeNoteList(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const { opts } = extractOpts(args);

  try {
    const client = makeClient(ctx);
    const params = opts.entity ? `?entity_id=${encodeURIComponent(opts.entity as string)}` : "";
    const result = await client.get(`/v1/notes${params}`);
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

async function executeNoteGet(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const id = args[0];
  if (!id) return fail("No note ID provided.");

  try {
    const client = makeClient(ctx);
    const result = await client.get(`/v1/notes/${encodeURIComponent(id)}`);
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

async function executeNoteCreate(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const { opts } = extractOpts(args);
  if (typeof opts.title !== "string") return fail("Required: --title <title>");

  try {
    const client = makeClient(ctx);
    const body: Record<string, unknown> = { title: opts.title };
    if (typeof opts.content === "string") body.content = opts.content;
    if (typeof opts.entity === "string") body.entity_id = opts.entity;
    const result = await client.post("/v1/notes", body);
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

async function executeNoteUpdate(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const { positional, opts } = extractOpts(args);
  const id = positional[0];
  if (!id) return fail("No note ID provided.");

  try {
    const client = makeClient(ctx);
    const body: Record<string, unknown> = {};
    if (typeof opts.title === "string") body.title = opts.title;
    if (typeof opts.content === "string") body.content = opts.content;
    if (typeof opts.entity === "string") body.entity_id = opts.entity;
    const result = await client.patch(`/v1/notes/${encodeURIComponent(id)}`, body);
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

async function executeNoteDelete(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const id = args[0];
  if (!id) return fail("No note ID provided.");

  try {
    const client = makeClient(ctx);
    const result = await client.delete(`/v1/notes/${encodeURIComponent(id)}`);
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

// -- Task commands --

async function executeTaskList(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const { opts } = extractOpts(args);

  try {
    const client = makeClient(ctx);
    const params = new URLSearchParams();
    if (typeof opts.entity === "string") params.set("entity_id", opts.entity);
    if (typeof opts.assignee === "string") params.set("assignee_id", opts.assignee);
    if (typeof opts.search === "string") params.set("search", opts.search);
    if (opts.completed === true) params.set("is_completed", "true");
    if (typeof opts.limit === "string") params.set("limit", opts.limit);
    const qs = params.toString();
    const result = await client.get(`/v1/tasks${qs ? `?${qs}` : ""}`);
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

async function executeTaskGet(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const id = args[0];
  if (!id) return fail("No task ID provided.");

  try {
    const client = makeClient(ctx);
    const result = await client.get(`/v1/tasks/${encodeURIComponent(id)}`);
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

async function executeTaskCreate(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const { opts } = extractOpts(args);
  if (typeof opts.title !== "string") return fail("Required: --title <title>");

  try {
    const client = makeClient(ctx);
    const body: Record<string, unknown> = { title: opts.title };
    if (typeof opts.description === "string") body.description = opts.description;
    if (typeof opts.priority === "string") body.priority = opts.priority;
    if (typeof opts.due === "string") body.due_date = opts.due;
    if (typeof opts.entities === "string") body.entity_ids = opts.entities.split(",");
    if (typeof opts.assignees === "string") body.assignee_ids = opts.assignees.split(",");
    const result = await client.post("/v1/tasks", body);
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

async function executeTaskUpdate(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const { positional, opts } = extractOpts(args);
  const id = positional[0];
  if (!id) return fail("No task ID provided.");

  try {
    const client = makeClient(ctx);
    const body: Record<string, unknown> = {};
    if (typeof opts.title === "string") body.title = opts.title;
    if (typeof opts.description === "string") body.description = opts.description;
    if (opts.completed === true) body.is_completed = true;
    if (typeof opts.priority === "string") body.priority = opts.priority;
    if (typeof opts.due === "string") body.due_date = opts.due;
    if (typeof opts.entities === "string") body.entity_ids = opts.entities.split(",");
    if (typeof opts.assignees === "string") body.assignee_ids = opts.assignees.split(",");
    const result = await client.patch(`/v1/tasks/${encodeURIComponent(id)}`, body);
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

async function executeTaskDelete(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const id = args[0];
  if (!id) return fail("No task ID provided.");

  try {
    const client = makeClient(ctx);
    const result = await client.delete(`/v1/tasks/${encodeURIComponent(id)}`);
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

// -- Relationship commands --

async function executeRelListDefs(_args: string[], ctx: CommandContext): Promise<CommandResult> {
  try {
    const client = makeClient(ctx);
    const result = await client.get("/v1/relationships");
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

async function executeRelCreateDef(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const { opts } = extractOpts(args);
  if (typeof opts.type !== "string") return fail("Required: --type <type>");
  if (typeof opts.entity1 !== "string") return fail("Required: --entity1 <id>");
  if (typeof opts.entity2 !== "string") return fail("Required: --entity2 <id>");

  try {
    const client = makeClient(ctx);
    const body: Record<string, unknown> = {
      type: opts.type,
      entity_definition_1_id: opts.entity1,
      entity_definition_2_id: opts.entity2,
    };
    if (typeof opts.pred12 === "string") body.entity_1_to_2_predicate = opts.pred12;
    if (typeof opts.pred21 === "string") body.entity_2_to_1_predicate = opts.pred21;
    const result = await client.post("/v1/relationships", body);
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

async function executeRelDeleteDef(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const id = args[0];
  if (!id) return fail("No relationship definition ID provided.");

  try {
    const client = makeClient(ctx);
    const result = await client.delete(`/v1/relationships/${encodeURIComponent(id)}`);
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

async function executeRelCreate(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const { positional, opts } = extractOpts(args);
  const recordId = positional[0];
  if (!recordId) return fail("No record ID provided.");
  if (typeof opts.def !== "string") return fail("Required: --def <id>");
  if (typeof opts.entity1 !== "string") return fail("Required: --entity1 <id>");
  if (typeof opts.entity2 !== "string") return fail("Required: --entity2 <id>");

  try {
    const client = makeClient(ctx);
    const body = {
      definition_id: opts.def,
      entity_1_id: opts.entity1,
      entity_2_id: opts.entity2,
    };
    const result = await client.post(`/v1/records/${encodeURIComponent(recordId)}/relationships`, body);
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

async function executeRelDelete(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const recordId = args[0];
  const relId = args[1];
  if (!recordId) return fail("No record ID provided.");
  if (!relId) return fail("No relationship ID provided.");

  try {
    const client = makeClient(ctx);
    const result = await client.delete(
      `/v1/records/${encodeURIComponent(recordId)}/relationships/${encodeURIComponent(relId)}`,
    );
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

// -- Attribute commands --

async function executeAttrCreate(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const { positional, opts } = extractOpts(args);
  const objectSlug = positional[0];
  if (!objectSlug) return fail("No object slug provided.");
  if (typeof opts.name !== "string") return fail("Required: --name <name>");
  if (typeof opts.slug !== "string") return fail("Required: --slug <slug>");
  if (typeof opts.type !== "string") return fail("Required: --type <type>");

  const body: Record<string, unknown> = { name: opts.name, slug: opts.slug, type: opts.type };
  if (typeof opts.description === "string") body.description = opts.description;
  if (typeof opts.options === "string") {
    try {
      body.options = JSON.parse(opts.options);
    } catch {
      return fail(`Invalid JSON for --options: ${opts.options}`);
    }
  }

  try {
    const client = makeClient(ctx);
    const result = await client.post(`/v1/objects/${encodeURIComponent(objectSlug)}/attributes`, body);
    return ok(result, ctx, { nav: { objectSlug } });
  } catch (err) {
    return wrapError(err);
  }
}

async function executeAttrUpdate(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const { positional, opts } = extractOpts(args);
  const objectSlug = positional[0];
  const attrId = positional[1];
  if (!objectSlug) return fail("No object slug provided.");
  if (!attrId) return fail("No attribute ID provided.");

  const body: Record<string, unknown> = {};
  if (typeof opts.name === "string") body.name = opts.name;
  if (typeof opts.description === "string") body.description = opts.description;
  if (typeof opts.options === "string") {
    try {
      body.options = JSON.parse(opts.options);
    } catch {
      return fail(`Invalid JSON for --options: ${opts.options}`);
    }
  }

  try {
    const client = makeClient(ctx);
    const result = await client.patch(
      `/v1/objects/${encodeURIComponent(objectSlug)}/attributes/${encodeURIComponent(attrId)}`,
      body,
    );
    return ok(result, ctx, { nav: { objectSlug } });
  } catch (err) {
    return wrapError(err);
  }
}

async function executeAttrDelete(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const objectSlug = args[0];
  const attrId = args[1];
  if (!objectSlug) return fail("No object slug provided.");
  if (!attrId) return fail("No attribute ID provided.");

  try {
    const client = makeClient(ctx);
    const result = await client.delete(
      `/v1/objects/${encodeURIComponent(objectSlug)}/attributes/${encodeURIComponent(attrId)}`,
    );
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

// -- List commands --

async function executeListList(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const { positional, opts } = extractOpts(args);
  const objectSlug = positional[0];
  if (!objectSlug) return fail("No object slug provided.");

  try {
    const client = makeClient(ctx);
    const params = opts["include-attributes"] ? "?include_attributes=true" : "";
    const result = await client.get(`/v1/objects/${encodeURIComponent(objectSlug)}/lists${params}`);
    return ok(result, ctx, { nav: { objectSlug } });
  } catch (err) {
    return wrapError(err);
  }
}

async function executeListGet(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const id = args[0];
  if (!id) return fail("No list ID provided.");

  try {
    const client = makeClient(ctx);
    const result = await client.get(`/v1/lists/${encodeURIComponent(id)}`);
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

async function executeListCreate(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const { positional, opts } = extractOpts(args);
  const objectSlug = positional[0];
  if (!objectSlug) return fail("No object slug provided.");
  if (typeof opts.name !== "string") return fail("Required: --name <name>");
  if (typeof opts.slug !== "string") return fail("Required: --slug <slug>");

  try {
    const client = makeClient(ctx);
    const body: Record<string, unknown> = { name: opts.name, slug: opts.slug };
    if (typeof opts.description === "string") body.description = opts.description;
    const result = await client.post(`/v1/objects/${encodeURIComponent(objectSlug)}/lists`, body);
    return ok(result, ctx, { nav: { objectSlug } });
  } catch (err) {
    return wrapError(err);
  }
}

async function executeListDelete(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const id = args[0];
  if (!id) return fail("No list ID provided.");

  try {
    const client = makeClient(ctx);
    const result = await client.delete(`/v1/lists/${encodeURIComponent(id)}`);
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

async function executeListRecords(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const { positional, opts } = extractOpts(args);
  const listId = positional[0];
  if (!listId) return fail("No list ID provided.");

  try {
    const client = makeClient(ctx);
    const body: Record<string, unknown> = { attributes: opts.attributes ?? "primary" };
    if (typeof opts.limit === "string") body.limit = parseInt(opts.limit, 10);
    if (typeof opts.offset === "string") body.offset = parseInt(opts.offset, 10);
    if (typeof opts.sort === "string") {
      const [attribute, direction] = opts.sort.split(":");
      body.sort = { attribute, direction };
    }
    const result = await client.post(`/v1/lists/${encodeURIComponent(listId)}/records`, body);
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

// -- List job commands --

async function executeListJobCreate(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const { positional, opts } = extractOpts(args);
  const query = positional.join(" ");
  if (!query) return fail("No query provided.");

  try {
    const client = makeClient(ctx);
    const body: Record<string, unknown> = { query };
    if (typeof opts.type === "string") body.object_type = opts.type;
    if (typeof opts.limit === "string") body.limit = parseInt(opts.limit, 10);
    const result = await client.post("/v1/context/list/jobs", body);
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

async function executeListJobStatus(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const { positional, opts } = extractOpts(args);
  const id = positional[0];
  if (!id) return fail("No job ID provided.");

  try {
    const client = makeClient(ctx);
    const params = opts["include-attributes"] ? "?include_attributes=true" : "";
    const result = await client.get(`/v1/context/list/jobs/${encodeURIComponent(id)}${params}`);
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

// -- Config commands --

function executeConfigShow(_args: string[], ctx: CommandContext): Promise<CommandResult> {
  const config = loadConfig();
  const apiKey = ctx.apiKey ?? resolveApiKey();

  const display: Record<string, unknown> = {
    ...config,
    api_key: apiKey ? maskKey(apiKey) : undefined,
    base_url: BASE_URL,
    config_path: CONFIG_PATH,
  };

  return Promise.resolve(ok(display, ctx));
}

function maskKey(key: string): string {
  if (key.length <= 4) return key;
  return "****" + key.slice(-4);
}

function executeConfigSet(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const key = args[0];
  const value = args[1];
  if (!key || value === undefined) return Promise.resolve(fail("Usage: config set <key> <value>"));

  const config = loadConfig();
  (config as Record<string, unknown>)[key] = value;
  saveConfig(config);

  return Promise.resolve(ok({ [key]: value }, ctx));
}

function executeConfigPath(_args: string[], ctx: CommandContext): Promise<CommandResult> {
  return Promise.resolve(ok({ path: CONFIG_PATH }, ctx));
}

// -- Integrate commands --

async function executeIntegrateList(_args: string[], ctx: CommandContext): Promise<CommandResult> {
  try {
    const client = makeClient(ctx);
    const result = await client.get("/v1/integrations/");
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

async function executeIntegrateConnect(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const name = args[0];
  if (!name) return fail("No integration name provided.");

  const INTEGRATIONS: Record<string, { type: string; provider: string }> = {
    gmail: { type: "email", provider: "google" },
    "google-calendar": { type: "calendar", provider: "google" },
    outlook: { type: "email", provider: "microsoft" },
    "outlook-calendar": { type: "calendar", provider: "microsoft" },
    slack: { type: "messaging", provider: "slack" },
    salesforce: { type: "crm", provider: "salesforce" },
    hubspot: { type: "crm", provider: "hubspot" },
    attio: { type: "crm", provider: "attio" },
  };

  const integration = INTEGRATIONS[name.toLowerCase()];
  if (!integration) {
    return fail(`Unknown integration "${name}". Available: ${Object.keys(INTEGRATIONS).join(", ")}`);
  }

  try {
    const client = makeClient(ctx);
    const result = await client.post<{ auth_url: string; connect_id?: string }>(
      `/v1/integrations/${encodeURIComponent(integration.type)}/${encodeURIComponent(integration.provider)}/connect`,
    );
    // Return the auth URL in data for the TUI to handle browser opening
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

async function executeIntegrateDisconnect(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const connectionId = args[0];
  if (!connectionId) return fail("No connection ID provided.");

  try {
    const client = makeClient(ctx);
    const result = await client.delete(`/v1/integrations/connections/${encodeURIComponent(connectionId)}`);
    return ok(result, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

// -- Graph command --

async function executeGraph(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const { opts } = extractOpts(args);
  const limit = typeof opts.limit === "string" ? parseInt(opts.limit, 10) : 1000;
  const outPath = typeof opts.out === "string" ? opts.out : undefined;
  const noOpen = opts["no-open"] === true || opts["no-open"] === "true";

  try {
    const client = makeClient(ctx);
    const html = await client.getRaw(`/v1/graph?limit=${limit}&format=html`);
    const { writeFileSync } = await import("node:fs");
    const { join } = await import("node:path");
    const { tmpdir } = await import("node:os");
    const filePath = outPath ?? join(tmpdir(), `nex-graph-${Date.now()}.html`);
    writeFileSync(filePath, html, "utf-8");

    if (!noOpen) {
      const { spawn } = await import("node:child_process");
      const cmd = process.platform === "darwin" ? "open" : process.platform === "linux" ? "xdg-open" : "cmd";
      const cmdArgs = process.platform === "win32" ? ["/c", "start", "", filePath] : [filePath];
      try { spawn(cmd, cmdArgs, { stdio: "ignore", detached: true }).unref(); } catch { /* ignore */ }
    }

    return ok({ path: filePath, message: noOpen ? `Graph saved to ${filePath}` : "Graph opened in browser" }, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

// ── Agent command executors ──

async function executeAgentList(_args: string[], ctx: CommandContext): Promise<CommandResult> {
  try {
    const service = getAgentService();
    const agents = service.list().map(m => ({
      slug: m.config.slug,
      name: m.config.name,
      phase: m.state.phase,
      expertise: m.config.expertise,
      tokensUsed: m.state.tokensUsed,
      costUsd: m.state.costUsd,
    }));
    return ok(agents, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

async function executeAgentCreate(args: string[], _ctx: CommandContext): Promise<CommandResult> {
  try {
    const { positional, opts } = extractOpts(args);
    const slug = positional[0];
    if (!slug) return fail("Usage: agent create <slug> --template <name> | --name <name> --expertise <skills>");

    const service = getAgentService();

    if (opts.template && typeof opts.template === "string") {
      const managed = service.createFromTemplate(slug, opts.template);
      return { output: `Created agent "${managed.config.name}" (${slug}) from template "${opts.template}".`, data: managed.config, exitCode: 0 };
    }

    const name = typeof opts.name === "string" ? opts.name : slug;
    const expertiseRaw = typeof opts.expertise === "string" ? opts.expertise : "";
    const expertise = expertiseRaw ? expertiseRaw.split(",").map(s => s.trim()) : [];
    const personality = typeof opts.personality === "string" ? opts.personality : undefined;

    const managed = service.create({ slug, name, expertise, personality });
    return { output: `Created agent "${managed.config.name}" (${slug}).`, data: managed.config, exitCode: 0 };
  } catch (err) {
    return wrapError(err);
  }
}

async function executeAgentStart(args: string[], _ctx: CommandContext): Promise<CommandResult> {
  try {
    const slug = args[0];
    if (!slug) return fail("Usage: agent start <slug>");
    const service = getAgentService();
    await service.start(slug);
    const state = service.getState(slug);
    return { output: `Agent "${slug}" started (phase: ${state?.phase ?? "unknown"}).`, data: state, exitCode: 0 };
  } catch (err) {
    return wrapError(err);
  }
}

async function executeAgentStop(args: string[], _ctx: CommandContext): Promise<CommandResult> {
  try {
    const slug = args[0];
    if (!slug) return fail("Usage: agent stop <slug>");
    const service = getAgentService();
    service.stop(slug);
    return { output: `Agent "${slug}" stopped.`, exitCode: 0 };
  } catch (err) {
    return wrapError(err);
  }
}

async function executeAgentSteer(args: string[], _ctx: CommandContext): Promise<CommandResult> {
  try {
    const slug = args[0];
    const message = args.slice(1).join(" ");
    if (!slug || !message) return fail("Usage: agent steer <slug> <message>");
    const service = getAgentService();
    service.steer(slug, message);
    return { output: `Steer message sent to "${slug}".`, exitCode: 0 };
  } catch (err) {
    return wrapError(err);
  }
}

async function executeAgentInspect(args: string[], ctx: CommandContext): Promise<CommandResult> {
  try {
    const slug = args[0];
    if (!slug) return fail("Usage: agent inspect <slug>");
    const service = getAgentService();
    const managed = service.get(slug);
    if (!managed) return fail(`Agent "${slug}" not found.`);
    const data = {
      config: managed.config,
      state: managed.state,
    };
    return ok(data, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

async function executeAgentTemplates(_args: string[], ctx: CommandContext): Promise<CommandResult> {
  try {
    const service = getAgentService();
    const names = service.getTemplateNames();
    const data = names.map(name => {
      const tmpl = service.getTemplate(name);
      return { name, ...(tmpl ?? {}) };
    });
    return ok(data, ctx);
  } catch (err) {
    return wrapError(err);
  }
}

// -- Init / Setup --

async function executeInit(args: string[], ctx: CommandContext): Promise<CommandResult> {
  const { positional, opts } = extractOpts(args);
  const email = typeof opts.email === "string" ? opts.email : positional[0];
  const existingKey = ctx.apiKey ?? resolveApiKey();

  const logs: string[] = [];

  try {
    await runInit(
      (progress: InitProgress) => {
        const prefix = progress.error ? "[ERROR]" : "[OK]";
        const line = progress.detail ?? progress.step;
        logs.push(`${prefix} ${line}`);
      },
      {
        email: email || undefined,
        apiKey: existingKey || undefined,
      },
    );

    const output = logs.join("\n");
    return {
      output,
      data: { logs, success: true },
      exitCode: 0,
    };
  } catch (err) {
    const output = logs.join("\n");
    return {
      output,
      data: { logs, success: false },
      exitCode: 1,
      error: err instanceof Error ? err.message : String(err),
    };
  }
}

async function executeDetectPlatforms(_args: string[], ctx: CommandContext): Promise<CommandResult> {
  const platforms = detectPlatforms();
  const detected = platforms.filter((p) => p.detected);
  return ok(detected, ctx);
}

// ── Command registry ──

const commands = new Map<string, CommandEntry>();

function register(name: string, entry: CommandEntry): void {
  commands.set(name, entry);
}

// -- AI commands --
register("ask", { execute: executeAsk, description: "Query your context graph with AI", category: "ai", usage: "ask <query>" });
register("remember", { execute: executeRemember, description: "Store a note, fact, or observation", category: "ai", usage: "remember <content> [--context <ctx>]" });
register("recall", { execute: executeRecall, description: "Recall context for LLM prompt injection", category: "ai", usage: "recall <query>" });
register("artifact", { execute: executeArtifact, description: "Retrieve a context artifact by ID", category: "ai", usage: "artifact <id>" });
register("capture", { execute: executeCapture, description: "Auto-capture content with filtering", category: "ai", usage: "capture <content>" });

// -- Query: objects --
register("object list", { execute: executeObjectList, description: "List all objects", category: "query", usage: "object list [--include-attributes]" });
register("object get", { execute: executeObjectGet, description: "Get an object by slug", category: "query", usage: "object get <slug>" });
register("object create", { execute: executeObjectCreate, description: "Create a new object", category: "write", usage: "object create --name <n> --slug <s> [--type <t>]" });
register("object update", { execute: executeObjectUpdate, description: "Update an object", category: "write", usage: "object update <slug> [--name <n>] [--description <d>]" });
register("object delete", { execute: executeObjectDelete, description: "Delete an object", category: "write", usage: "object delete <slug>" });

// -- Query/Write: records --
register("record list", { execute: executeRecordList, description: "List records for an object", category: "query", usage: "record list <object-slug> [--limit <n>]" });
register("record get", { execute: executeRecordGet, description: "Get a record by ID", category: "query", usage: "record get <id>" });
register("record create", { execute: executeRecordCreate, description: "Create a record", category: "write", usage: "record create <object-slug> --data <json>" });
register("record upsert", { execute: executeRecordUpsert, description: "Upsert a record", category: "write", usage: "record upsert <object-slug> --match <attr> --data <json>" });
register("record update", { execute: executeRecordUpdate, description: "Update a record", category: "write", usage: "record update <id> --data <json>" });
register("record delete", { execute: executeRecordDelete, description: "Delete a record", category: "write", usage: "record delete <id>" });
register("record timeline", { execute: executeRecordTimeline, description: "Get record timeline", category: "query", usage: "record timeline <id> [--limit <n>]" });

// -- Query: search --
register("search", { execute: executeSearch, description: "Fuzzy keyword search across CRM records", category: "query", usage: "search <query>" });

// -- Query: insights --
register("insight list", { execute: executeInsightList, description: "List insights", category: "query", usage: "insight list [--last <dur>] [--from <date>] [--to <date>]" });

// -- Write: notes --
register("note list", { execute: executeNoteList, description: "List notes", category: "query", usage: "note list [--entity <id>]" });
register("note get", { execute: executeNoteGet, description: "Get a note by ID", category: "query", usage: "note get <id>" });
register("note create", { execute: executeNoteCreate, description: "Create a note", category: "write", usage: "note create --title <t> [--content <c>] [--entity <id>]" });
register("note update", { execute: executeNoteUpdate, description: "Update a note", category: "write", usage: "note update <id> [--title <t>] [--content <c>]" });
register("note delete", { execute: executeNoteDelete, description: "Delete a note", category: "write", usage: "note delete <id>" });

// -- Write: tasks --
register("task list", { execute: executeTaskList, description: "List tasks", category: "query", usage: "task list [--entity <id>] [--assignee <id>]" });
register("task get", { execute: executeTaskGet, description: "Get a task by ID", category: "query", usage: "task get <id>" });
register("task create", { execute: executeTaskCreate, description: "Create a task", category: "write", usage: "task create --title <t> [--priority <p>] [--due <d>]" });
register("task update", { execute: executeTaskUpdate, description: "Update a task", category: "write", usage: "task update <id> [--title <t>] [--completed]" });
register("task delete", { execute: executeTaskDelete, description: "Delete a task", category: "write", usage: "task delete <id>" });

// -- Relationships --
register("rel list-defs", { execute: executeRelListDefs, description: "List relationship definitions", category: "query", usage: "rel list-defs" });
register("rel create-def", { execute: executeRelCreateDef, description: "Create a relationship definition", category: "write", usage: "rel create-def --type <t> --entity1 <id> --entity2 <id>" });
register("rel delete-def", { execute: executeRelDeleteDef, description: "Delete a relationship definition", category: "write", usage: "rel delete-def <id>" });
register("rel create", { execute: executeRelCreate, description: "Create a relationship between records", category: "write", usage: "rel create <record-id> --def <id> --entity1 <id> --entity2 <id>" });
register("rel delete", { execute: executeRelDelete, description: "Delete a relationship", category: "write", usage: "rel delete <record-id> <rel-id>" });

// -- Attributes --
register("attribute create", { execute: executeAttrCreate, description: "Create an attribute on an object", category: "write", usage: "attribute create <slug> --name <n> --slug <s> --type <t>" });
register("attribute update", { execute: executeAttrUpdate, description: "Update an attribute", category: "write", usage: "attribute update <object-slug> <attr-id> [--name <n>]" });
register("attribute delete", { execute: executeAttrDelete, description: "Delete an attribute", category: "write", usage: "attribute delete <object-slug> <attr-id>" });

// -- Lists --
register("list list", { execute: executeListList, description: "List all lists for an object", category: "query", usage: "list list <object-slug>" });
register("list get", { execute: executeListGet, description: "Get a list by ID", category: "query", usage: "list get <id>" });
register("list create", { execute: executeListCreate, description: "Create a new list", category: "write", usage: "list create <object-slug> --name <n> --slug <s>" });
register("list delete", { execute: executeListDelete, description: "Delete a list", category: "write", usage: "list delete <id>" });
register("list records", { execute: executeListRecords, description: "List records in a list", category: "query", usage: "list records <list-id> [--limit <n>]" });

// -- List jobs --
register("list-job create", { execute: executeListJobCreate, description: "Create a list generation job", category: "ai", usage: "list-job create <query> [--type <t>]" });
register("list-job status", { execute: executeListJobStatus, description: "Get list generation job status", category: "query", usage: "list-job status <id>" });

// -- Config --
register("config show", { execute: executeConfigShow, description: "Show resolved configuration", category: "config" });
register("config set", { execute: executeConfigSet, description: "Set a configuration value", category: "config", usage: "config set <key> <value>" });
register("config path", { execute: executeConfigPath, description: "Print the config file path", category: "config" });

// -- Init / Setup --
register("init", { execute: executeInit, description: "Set up Nex for all detected AI platforms", category: "config", usage: "init [email] [--email <email>]" });
register("detect", { execute: executeDetectPlatforms, description: "Detect installed AI coding platforms", category: "config" });

// -- Integrations --
register("integrate list", { execute: executeIntegrateList, description: "List all integrations and connection status", category: "config" });
register("integrate connect", { execute: executeIntegrateConnect, description: "Connect an integration", category: "config", usage: "integrate connect <name>" });
register("integrate disconnect", { execute: executeIntegrateDisconnect, description: "Disconnect an integration", category: "config", usage: "integrate disconnect <connection-id>" });

// -- Graph --
register("graph", { execute: executeGraph, description: "Fetch workspace entity graph data", category: "graph", usage: "graph [--limit <n>]" });

// -- Agents --
register("agent list", { execute: executeAgentList, description: "List all managed agents", category: "agent", usage: "agent list" });
register("agent create", { execute: executeAgentCreate, description: "Create an agent from template or config", category: "agent", usage: "agent create <slug> --template <name> | --name <n> --expertise <skills>" });
register("agent start", { execute: executeAgentStart, description: "Start an agent loop", category: "agent", usage: "agent start <slug>" });
register("agent stop", { execute: executeAgentStop, description: "Stop an agent loop", category: "agent", usage: "agent stop <slug>" });
register("agent steer", { execute: executeAgentSteer, description: "Send a steer message to an agent", category: "agent", usage: "agent steer <slug> <message>" });
register("agent inspect", { execute: executeAgentInspect, description: "Inspect agent config and state", category: "agent", usage: "agent inspect <slug>" });
register("agent templates", { execute: executeAgentTemplates, description: "List available agent templates", category: "agent", usage: "agent templates" });

// -- TUI view hints (so aliases resolve and dispatch does not return "unknown command") --
register("chat", {
  execute: async () => ({ output: "Use the 'c' keybinding to open the chat view.", exitCode: 0 }),
  description: "Open the chat view (use 'c' keybinding in TUI)",
  category: "config",
});
register("calendar", {
  execute: async () => ({ output: "Use the 'C' keybinding to open the calendar view.", exitCode: 0 }),
  description: "Open the calendar view (use 'C' keybinding in TUI)",
  category: "config",
});
register("orchestration", {
  execute: async () => ({ output: "Use the 'o' keybinding to open the orchestration view.", exitCode: 0 }),
  description: "Open the orchestration view (use 'o' keybinding in TUI)",
  category: "config",
});

// ── Public API ──

// ── Command aliases ──
// Maps short-hand aliases to canonical command names so users can type less.

const COMMAND_ALIASES: Record<string, string> = {
  agents: "agent list",
  objects: "object list",
  orch: "orchestration",
  setup: "init",
};

/**
 * Resolve alias tokens into their canonical form.
 * Single-word aliases are checked first, then multi-word aliases via join.
 */
function resolveAlias(tokens: string[]): string[] {
  if (tokens.length === 0) return tokens;

  // Check single-word alias
  const oneWord = tokens[0].toLowerCase();
  if (COMMAND_ALIASES[oneWord]) {
    const canonical = COMMAND_ALIASES[oneWord].split(" ");
    return [...canonical, ...tokens.slice(1)];
  }

  return tokens;
}

/**
 * Resolve a token stream into a registered command name.
 * Tries alias resolution first, then two-word match (e.g. "record list"), then single-word.
 */
function resolveCommand(tokens: string[]): { name: string; args: string[] } | undefined {
  if (tokens.length === 0) return undefined;

  // Apply alias resolution
  const resolved = resolveAlias(tokens);

  // Try two-word match first
  if (resolved.length >= 2) {
    const twoWord = `${resolved[0]} ${resolved[1]}`;
    if (commands.has(twoWord)) {
      return { name: twoWord, args: resolved.slice(2) };
    }
  }

  // Single-word match
  if (commands.has(resolved[0])) {
    return { name: resolved[0], args: resolved.slice(1) };
  }

  return undefined;
}

/**
 * Dispatch a command string, returning a structured result.
 * The TUI and CLI can both call this to execute commands without stdout side effects.
 */
export async function dispatch(input: string, ctx?: CommandContext): Promise<CommandResult> {
  const tokens = parseInput(input);
  if (tokens.length === 0) {
    return fail("No command provided.");
  }

  const resolved = resolveCommand(tokens);
  if (!resolved) {
    return fail(`Unknown command: ${tokens[0]}${tokens.length > 1 ? " " + tokens[1] : ""}`);
  }

  const entry = commands.get(resolved.name)!;
  return entry.execute(resolved.args, ctx ?? {});
}

/** All registered command names, for autocomplete. */
export const commandNames: string[] = Array.from(commands.keys()).sort();

/** Help entries for each command, with category and description. */
export const commandHelp: { command: string; description: string; category: string; usage?: string }[] =
  Array.from(commands.entries())
    .map(([name, entry]) => ({
      command: name,
      description: entry.description,
      category: entry.category,
      usage: entry.usage,
    }))
    .sort((a, b) => a.category.localeCompare(b.category) || a.command.localeCompare(b.command));
