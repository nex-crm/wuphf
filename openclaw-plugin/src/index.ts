/**
 * Nex Memory Plugin for OpenClaw
 *
 * Gives OpenClaw agents persistent long-term memory powered by the Nex
 * context intelligence layer. Auto-recalls relevant context before each
 * agent turn and auto-captures conversation facts after each turn.
 */

import { Type, type Static } from "@sinclair/typebox";
import { parseConfig, type NexPluginConfig } from "./config.js";
import { NexClient, NexAuthError } from "./nex-client.js";
import { RateLimiter } from "./rate-limiter.js";
import { SessionStore } from "./session-store.js";
import { formatNexContext, stripNexContext } from "./context-format.js";
import { captureFilter, type AgentMessage } from "./capture-filter.js";
import { scanFiles as scanFilesUtil, type ScanResult } from "./file-scanner.js";

// --- TypeBox schemas for tool parameters ---

const SearchParams = Type.Object({
  query: Type.String({ description: "What to search for in the knowledge base" }),
});

const RememberParams = Type.Object({
  content: Type.String({ description: "The information to remember" }),
  label: Type.Optional(Type.String({ description: "Optional context label (e.g. 'meeting notes', 'preference')" })),
});

const EntitiesParams = Type.Object({
  query: Type.String({ description: "Search query to find related entities" }),
});

const ScanFilesParams = Type.Object({
  dir: Type.String({ description: "Directory path to scan for text files" }),
  extensions: Type.Optional(Type.String({ description: "Comma-separated file extensions (default: .md,.txt,.csv,.json,.yaml,.yml)" })),
  max_files: Type.Optional(Type.Number({ description: "Maximum files to scan per run (default: 5)" })),
  depth: Type.Optional(Type.Number({ description: "Maximum directory depth (default: 2)" })),
  force: Type.Optional(Type.Boolean({ description: "Re-scan all files ignoring manifest (default: false)" })),
});

// --- Plugin Logger interface (matches OpenClaw's PluginLogger) ---

interface Logger {
  debug?(...args: unknown[]): void;
  info(...args: unknown[]): void;
  warn(...args: unknown[]): void;
  error(...args: unknown[]): void;
}

// --- Minimal OpenClaw Plugin API types ---
// These match the actual OpenClaw SDK types discovered during research.

interface PluginHookAgentContext {
  agentId?: string;
  sessionKey?: string;
  sessionId?: string;
  workspaceDir?: string;
  messageProvider?: string;
}

interface BeforeAgentStartEvent {
  prompt: string;
  messages?: unknown[];
}

interface AgentEndEvent {
  messages: unknown[];
  success: boolean;
  error?: unknown;
  durationMs?: number;
}

interface PluginCommandContext {
  args?: string;
  commandBody: string;
}

interface ToolCallResult {
  content: Array<{ type: string; text: string }>;
  details: unknown;
}

interface OpenClawPluginApi {
  id: string;
  name: string;
  pluginConfig?: Record<string, unknown>;
  logger: Logger;

  on(
    hookName: "before_agent_start",
    handler: (event: BeforeAgentStartEvent, ctx: PluginHookAgentContext) =>
      Promise<{ prependContext?: string } | void> | { prependContext?: string } | void,
    opts?: { priority?: number }
  ): void;

  on(
    hookName: "agent_end",
    handler: (event: AgentEndEvent, ctx: PluginHookAgentContext) => Promise<void> | void,
    opts?: { priority?: number }
  ): void;

  registerTool(tool: {
    name: string;
    label: string;
    description: string;
    parameters: unknown;
    execute: (
      toolCallId: string,
      params: Record<string, unknown>,
      signal?: AbortSignal,
    ) => Promise<ToolCallResult>;
    ownerOnly?: boolean;
  }): void;

  registerCommand(command: {
    name: string;
    description: string;
    acceptsArgs?: boolean;
    handler: (ctx: PluginCommandContext) => Promise<{ text: string }> | { text: string };
  }): void;

  registerService(service: {
    id: string;
    start: (ctx: { logger: Logger }) => Promise<void> | void;
    stop?: (ctx: { logger: Logger }) => Promise<void> | void;
  }): void;
}

// --- Plugin definition ---

const plugin = {
  id: "nex",
  name: "Nex Memory",
  description: "Persistent context intelligence for OpenClaw agents, powered by Nex",
  version: "0.1.0",
  kind: "memory" as const,

  register(api: OpenClawPluginApi) {
    const log = api.logger;

    // --- Config ---
    let cfg: NexPluginConfig;
    try {
      cfg = parseConfig(api.pluginConfig);
    } catch (err) {
      log.error("Failed to parse Nex plugin config:", err);
      throw err;
    }

    const client = new NexClient(cfg.apiKey, cfg.baseUrl);
    const rateLimiter = new RateLimiter();
    const sessions = new SessionStore();

    const debug = (...args: unknown[]) => {
      if (cfg.debug && log.debug) log.debug("[nex]", ...args);
    };

    debug("Plugin config loaded", { baseUrl: cfg.baseUrl, autoRecall: cfg.autoRecall, autoCapture: cfg.autoCapture });

    // --- Service (health check on start, cleanup on stop) ---

    api.registerService({
      id: "nex",
      async start({ logger }) {
        logger.info("Nex memory plugin starting...");
        try {
          const healthy = await client.healthCheck();
          if (healthy) {
            logger.info("Nex API connection verified");
          } else {
            logger.warn("Nex API health check failed — recall/capture may not work");
          }
        } catch (err) {
          if (err instanceof NexAuthError) {
            logger.error("Nex API key is invalid. Check your apiKey config or NEX_API_KEY env var.");
          } else {
            logger.warn("Could not reach Nex API:", err);
          }
        }
      },
      async stop({ logger }) {
        logger.info("Nex memory plugin stopping — flushing capture queue...");
        try {
          await Promise.race([
            rateLimiter.flush(),
            new Promise((_, reject) => setTimeout(() => reject(new Error("flush timeout")), 5000)),
          ]);
        } catch {
          logger.warn("Capture queue flush timed out");
        }
        rateLimiter.destroy();
        sessions.clear();
      },
    });

    // --- Hook: before_agent_start (auto-recall) ---

    if (cfg.autoRecall) {
      api.on(
        "before_agent_start",
        async (event, ctx) => {
          if (!event.prompt) return;

          debug("Auto-recall triggered", { sessionKey: ctx.sessionKey, promptLength: event.prompt.length });

          try {
            // Resolve session ID for multi-turn continuity
            const nexSessionId = ctx.sessionKey && cfg.sessionTracking
              ? sessions.get(ctx.sessionKey)
              : undefined;

            const result = await client.ask(event.prompt, nexSessionId, cfg.recallTimeoutMs);

            if (!result.answer) {
              debug("Recall returned empty answer");
              return;
            }

            // Store session ID for future turns
            if (result.session_id && ctx.sessionKey && cfg.sessionTracking) {
              sessions.set(ctx.sessionKey, result.session_id);
            }

            const entityCount = result.entity_references?.length ?? 0;
            const context = formatNexContext({
              answer: result.answer,
              entityCount,
              sessionId: result.session_id,
            });

            debug("Recall injecting context", { entityCount, answerLength: result.answer.length });

            return { prependContext: context };
          } catch (err) {
            // Graceful degradation — never block agent on recall failure
            if (err instanceof Error && err.name === "AbortError") {
              debug("Recall timed out");
            } else {
              log.warn("Nex recall failed (agent will proceed without context):", err);
            }
            return;
          }
        },
        { priority: 10 },
      );
    }

    // --- Hook: agent_end (auto-capture) ---

    if (cfg.autoCapture) {
      api.on("agent_end", async (event, ctx) => {
        const messages = event.messages as AgentMessage[];
        const result = captureFilter(messages, cfg, {
          messageProvider: ctx.messageProvider,
          success: event.success,
        });

        if (result.skipped) {
          debug("Capture skipped:", result.reason);
          return;
        }

        debug("Capture enqueued", { textLength: result.text.length });

        // Fire-and-forget via rate limiter
        rateLimiter.enqueue(async () => {
          try {
            const res = await client.ingest(result.text, "openclaw-conversation");
            debug("Capture complete", { artifactId: res.artifact_id });
          } catch (err) {
            log.warn("Nex capture failed:", err);
          }
        }).catch(() => {
          // Queue full / dropped — already logged by rate limiter
        });
      });
    }

    // --- Tools ---

    api.registerTool({
      name: "nex_search",
      label: "Search Nex Knowledge",
      description:
        "Search the user's Nex knowledge base for relevant context. Returns an AI-synthesized answer with entity references.",
      parameters: SearchParams,
      async execute(_toolCallId, params) {
        const { query } = params as Static<typeof SearchParams>;
        const result = await client.ask(query);

        const parts: string[] = [result.answer];
        if (result.entity_references && result.entity_references.length > 0) {
          parts.push("\n\nRelated entities:");
          for (const ref of result.entity_references) {
            parts.push(`- ${ref.name} (${ref.type})`);
          }
        }

        return {
          content: [{ type: "text", text: parts.join("\n") }],
          details: result,
        };
      },
    });

    api.registerTool({
      name: "nex_remember",
      label: "Remember in Nex",
      description:
        "Store information in the user's Nex knowledge base for long-term recall. Use this when the user explicitly asks you to remember something.",
      parameters: RememberParams,
      async execute(_toolCallId, params) {
        const { content, label } = params as Static<typeof RememberParams>;

        // Enqueue via rate limiter but wait for result
        const res = await new Promise<{ artifact_id: string }>((resolve, reject) => {
          rateLimiter
            .enqueue(async () => {
              const r = await client.ingest(content, label);
              resolve(r);
            })
            .catch(reject);
        });

        return {
          content: [{ type: "text", text: `Remembered. (artifact: ${res.artifact_id})` }],
          details: res,
        };
      },
    });

    api.registerTool({
      name: "nex_entities",
      label: "Find Nex Entities",
      description:
        "Search for entities (people, companies, topics) in the user's Nex knowledge base. Returns a structured list with types and mention counts.",
      parameters: EntitiesParams,
      async execute(_toolCallId, params) {
        const { query } = params as Static<typeof EntitiesParams>;
        const result = await client.ask(query);

        if (!result.entity_references || result.entity_references.length === 0) {
          return {
            content: [{ type: "text", text: "No matching entities found." }],
            details: { entities: [] },
          };
        }

        const lines = result.entity_references.map(
          (ref) => `- ${ref.name} (${ref.type})${ref.count ? ` — ${ref.count} mentions` : ""}`
        );

        return {
          content: [{ type: "text", text: `Found ${result.entity_references.length} entities:\n${lines.join("\n")}` }],
          details: { entities: result.entity_references },
        };
      },
    });

    api.registerTool({
      name: "nex_scan_files",
      label: "Scan Files into Nex",
      description:
        "Scan a directory for text files (.md, .txt, .csv, .json, .yaml, .yml) and ingest new or changed files into the Nex knowledge base. Uses SHA-256 content hashing to skip already-ingested files.",
      parameters: ScanFilesParams,
      async execute(_toolCallId, params) {
        const { dir, extensions, max_files, depth, force } = params as Static<typeof ScanFilesParams>;

        const extList = extensions
          ? extensions.split(",").map((e: string) => e.trim())
          : undefined;

        const result = await scanFilesUtil(dir, client, {
          extensions: extList,
          maxFiles: max_files,
          depth,
          force,
        });

        const summary = `Scanned ${result.scanned} file(s), skipped ${result.skipped}, errors ${result.errors}.`;
        const details = result.files
          .map((f) => `- ${f.path}: ${f.status}${f.reason ? ` (${f.reason})` : ""}`)
          .join("\n");

        return {
          content: [{ type: "text", text: `${summary}\n\n${details}` }],
          details: result,
        };
      },
    });

    // --- Integration tools ---

    const ListIntegrationsParams = Type.Object({});

    api.registerTool({
      name: "nex_list_integrations",
      label: "List Integrations",
      description: "List available third-party integrations and their connection status. Use nex_connect_integration to connect new ones, or nex_disconnect_integration with a connection ID to remove.",
      parameters: ListIntegrationsParams,
      async execute(_toolCallId, _params) {
        const result = await client.get("/v1/integrations/");
        return {
          content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
          details: result,
        };
      },
    });

    const ConnectIntegrationParams = Type.Object({
      type: Type.String({ description: "Integration type: email, calendar, crm, messaging" }),
      provider: Type.String({ description: "Provider: google, microsoft, attio, slack, salesforce, hubspot" }),
    });

    api.registerTool({
      name: "nex_connect_integration",
      label: "Connect Integration",
      description: "Start connecting a third-party integration via OAuth. Returns an auth_url for the user to open in their browser. Types: email, calendar, crm, messaging. Providers: google, microsoft, attio, slack, salesforce, hubspot.",
      parameters: ConnectIntegrationParams,
      async execute(_toolCallId, params) {
        const { type, provider } = params as Static<typeof ConnectIntegrationParams>;
        const result = await client.post(
          `/v1/integrations/${encodeURIComponent(type)}/${encodeURIComponent(provider)}/connect`
        );
        return {
          content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
          details: result,
        };
      },
    });

    const DisconnectIntegrationParams = Type.Object({
      connection_id: Type.String({ description: "Connection ID to disconnect" }),
    });

    api.registerTool({
      name: "nex_disconnect_integration",
      label: "Disconnect Integration",
      description: "Disconnect a third-party integration by connection ID. Get connection IDs from nex_list_integrations.",
      parameters: DisconnectIntegrationParams,
      async execute(_toolCallId, params) {
        const { connection_id } = params as Static<typeof DisconnectIntegrationParams>;
        const result = await client.delete(
          `/v1/integrations/connections/${encodeURIComponent(connection_id)}`
        );
        return {
          content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
          details: result,
        };
      },
    });

    // --- Schema tools ---

    const ListObjectsParams = Type.Object({
      include_attributes: Type.Optional(Type.Boolean({ description: "Include attribute definitions in the response" })),
    });

    api.registerTool({
      name: "nex_list_objects",
      label: "List Object Types",
      description: "List all object type definitions in the workspace. Call this first to discover available object types and their schemas.",
      parameters: ListObjectsParams,
      async execute(_toolCallId, params) {
        const { include_attributes } = params as Static<typeof ListObjectsParams>;
        const qs = new URLSearchParams();
        if (include_attributes) qs.set("include_attributes", "true");
        const q = qs.toString();
        const result = await client.get(`/v1/objects${q ? `?${q}` : ""}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const GetObjectParams = Type.Object({
      slug: Type.String({ description: "Object type slug (e.g. 'person', 'company', 'deal')" }),
    });

    api.registerTool({
      name: "nex_get_object",
      label: "Get Object Type",
      description: "Get a single object type definition with its attributes.",
      parameters: GetObjectParams,
      async execute(_toolCallId, params) {
        const { slug } = params as Static<typeof GetObjectParams>;
        const result = await client.get(`/v1/objects/${encodeURIComponent(slug)}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const CreateObjectParams = Type.Object({
      name: Type.String({ description: "Display name for the object type" }),
      slug: Type.String({ description: "URL-safe identifier (lowercase, hyphens)" }),
      name_plural: Type.Optional(Type.String({ description: "Plural display name" })),
      description: Type.Optional(Type.String({ description: "Description of the object type" })),
      type: Type.Optional(Type.String({ description: "Object category: person, company, custom, deal (default: custom)" })),
    });

    api.registerTool({
      name: "nex_create_object",
      label: "Create Object Type",
      description: "Create a new custom object type definition (e.g. 'Project', 'Deal').",
      parameters: CreateObjectParams,
      async execute(_toolCallId, params) {
        const { name, slug, name_plural, description, type } = params as Static<typeof CreateObjectParams>;
        const body: Record<string, unknown> = { name, slug };
        if (name_plural !== undefined) body.name_plural = name_plural;
        if (description !== undefined) body.description = description;
        if (type !== undefined) body.type = type;
        const result = await client.post("/v1/objects", body);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const UpdateObjectParams = Type.Object({
      slug: Type.String({ description: "Object type slug to update" }),
      name: Type.Optional(Type.String({ description: "New display name" })),
      name_plural: Type.Optional(Type.String({ description: "New plural display name" })),
      description: Type.Optional(Type.String({ description: "New description" })),
    });

    api.registerTool({
      name: "nex_update_object",
      label: "Update Object Type",
      description: "Update an existing object type definition (name, description, plural name).",
      parameters: UpdateObjectParams,
      async execute(_toolCallId, params) {
        const { slug, name, name_plural, description } = params as Static<typeof UpdateObjectParams>;
        const body: Record<string, unknown> = {};
        if (name !== undefined) body.name = name;
        if (name_plural !== undefined) body.name_plural = name_plural;
        if (description !== undefined) body.description = description;
        const result = await client.patch(`/v1/objects/${encodeURIComponent(slug)}`, body);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const DeleteObjectParams = Type.Object({
      slug: Type.String({ description: "Object type slug to delete" }),
    });

    api.registerTool({
      name: "nex_delete_object",
      label: "Delete Object Type",
      description: "Delete an object type definition and ALL its records. This is destructive and cannot be undone.",
      parameters: DeleteObjectParams,
      async execute(_toolCallId, params) {
        const { slug } = params as Static<typeof DeleteObjectParams>;
        const result = await client.delete(`/v1/objects/${encodeURIComponent(slug)}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const CreateAttributeParams = Type.Object({
      object_slug: Type.String({ description: "Object type slug to add the attribute to" }),
      name: Type.String({ description: "Display name for the attribute" }),
      slug: Type.String({ description: "URL-safe identifier for the attribute" }),
      type: Type.String({ description: "Attribute data type: text, number, email, phone, url, date, boolean, currency, location, select, social_profile, domain, full_name" }),
      description: Type.Optional(Type.String({ description: "Description of the attribute" })),
      options: Type.Optional(Type.Object({
        is_required: Type.Optional(Type.Boolean()),
        is_unique: Type.Optional(Type.Boolean()),
        is_multi_value: Type.Optional(Type.Boolean()),
        use_raw_format: Type.Optional(Type.Boolean()),
        is_whole_number: Type.Optional(Type.Boolean()),
        select_options: Type.Optional(Type.Array(Type.Object({ name: Type.String() }))),
      })),
    });

    api.registerTool({
      name: "nex_create_attribute",
      label: "Create Attribute",
      description: "Add a new attribute (field) to an object type. Supports types: text, number, email, phone, url, date, boolean, currency, location, select, social_profile, domain, full_name.",
      parameters: CreateAttributeParams,
      async execute(_toolCallId, params) {
        const { object_slug, name, slug, type, description, options } = params as Static<typeof CreateAttributeParams>;
        const body: Record<string, unknown> = { name, slug, type };
        if (description !== undefined) body.description = description;
        if (options) body.options = options;
        const result = await client.post(`/v1/objects/${encodeURIComponent(object_slug)}/attributes`, body);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const UpdateAttributeParams = Type.Object({
      object_slug: Type.String({ description: "Object type slug" }),
      attribute_id: Type.String({ description: "Attribute ID to update" }),
      name: Type.Optional(Type.String({ description: "New display name" })),
      description: Type.Optional(Type.String({ description: "New description" })),
      options: Type.Optional(Type.Object({
        is_required: Type.Optional(Type.Boolean()),
        select_options: Type.Optional(Type.Array(Type.Object({ name: Type.String() }))),
        use_raw_format: Type.Optional(Type.Boolean()),
        is_whole_number: Type.Optional(Type.Boolean()),
      })),
    });

    api.registerTool({
      name: "nex_update_attribute",
      label: "Update Attribute",
      description: "Update an existing attribute definition on an object type.",
      parameters: UpdateAttributeParams,
      async execute(_toolCallId, params) {
        const { object_slug, attribute_id, name, description, options } = params as Static<typeof UpdateAttributeParams>;
        const body: Record<string, unknown> = {};
        if (name !== undefined) body.name = name;
        if (description !== undefined) body.description = description;
        if (options) body.options = options;
        const result = await client.patch(`/v1/objects/${encodeURIComponent(object_slug)}/attributes/${encodeURIComponent(attribute_id)}`, body);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const DeleteAttributeParams = Type.Object({
      object_slug: Type.String({ description: "Object type slug" }),
      attribute_id: Type.String({ description: "Attribute ID to delete" }),
    });

    api.registerTool({
      name: "nex_delete_attribute",
      label: "Delete Attribute",
      description: "Delete an attribute from an object type. Removes the field and its data from all records. Cannot be undone.",
      parameters: DeleteAttributeParams,
      async execute(_toolCallId, params) {
        const { object_slug, attribute_id } = params as Static<typeof DeleteAttributeParams>;
        const result = await client.delete(`/v1/objects/${encodeURIComponent(object_slug)}/attributes/${encodeURIComponent(attribute_id)}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    // --- Record tools ---

    const CreateRecordParams = Type.Object({
      object_slug: Type.String({ description: "Object type slug (e.g. 'person', 'company')" }),
      attributes: Type.Record(Type.String(), Type.Unknown(), { description: "Record attributes — must include 'name'" }),
    });

    api.registerTool({
      name: "nex_create_record",
      label: "Create Record",
      description: "Create a new record for an object type. Use only when you have clean, structured data with known attribute slugs.",
      parameters: CreateRecordParams,
      async execute(_toolCallId, params) {
        const { object_slug, attributes } = params as Static<typeof CreateRecordParams>;
        const result = await client.post(`/v1/objects/${encodeURIComponent(object_slug)}`, { attributes });
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const UpsertRecordParams = Type.Object({
      object_slug: Type.String({ description: "Object type slug (e.g. 'person', 'company')" }),
      matching_attribute: Type.String({ description: "Attribute slug or ID to match on for dedup (e.g. 'email')" }),
      attributes: Type.Record(Type.String(), Type.Unknown(), { description: "Record attributes — must include 'name' when creating" }),
    });

    api.registerTool({
      name: "nex_upsert_record",
      label: "Upsert Record",
      description: "Create a record if it doesn't exist, or update it if a match is found on the specified attribute. Useful for deduplication.",
      parameters: UpsertRecordParams,
      async execute(_toolCallId, params) {
        const { object_slug, matching_attribute, attributes } = params as Static<typeof UpsertRecordParams>;
        const result = await client.put(`/v1/objects/${encodeURIComponent(object_slug)}`, { matching_attribute, attributes });
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const ListRecordsParams = Type.Object({
      object_slug: Type.String({ description: "Object type slug (e.g. 'person', 'company')" }),
      attributes: Type.Optional(Type.Union([
        Type.String({ description: "'all', 'primary', or 'none'" }),
        Type.Record(Type.String(), Type.Unknown()),
      ])),
      limit: Type.Optional(Type.Number({ description: "Number of records to return" })),
      offset: Type.Optional(Type.Number({ description: "Pagination offset" })),
      sort: Type.Optional(Type.Object({
        attribute: Type.String({ description: "Attribute slug to sort by" }),
        direction: Type.String({ description: "Sort direction: asc or desc" }),
      })),
    });

    api.registerTool({
      name: "nex_list_records",
      label: "List Records",
      description: "List records for an object type with optional filtering, sorting, and pagination.",
      parameters: ListRecordsParams,
      async execute(_toolCallId, params) {
        const { object_slug, attributes, limit, offset, sort } = params as Static<typeof ListRecordsParams>;
        const body: Record<string, unknown> = {};
        if (attributes !== undefined) body.attributes = attributes;
        if (limit !== undefined) body.limit = limit;
        if (offset !== undefined) body.offset = offset;
        if (sort) body.sort = sort;
        const result = await client.post(`/v1/objects/${encodeURIComponent(object_slug)}/records`, body);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const GetRecordParams = Type.Object({
      record_id: Type.String({ description: "Record ID" }),
    });

    api.registerTool({
      name: "nex_get_record",
      label: "Get Record",
      description: "Retrieve a specific record by its ID, including all its attributes.",
      parameters: GetRecordParams,
      async execute(_toolCallId, params) {
        const { record_id } = params as Static<typeof GetRecordParams>;
        const result = await client.get(`/v1/records/${encodeURIComponent(record_id)}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const UpdateRecordParams = Type.Object({
      record_id: Type.String({ description: "Record ID to update" }),
      attributes: Type.Record(Type.String(), Type.Unknown(), { description: "Attributes to update (only provided fields are changed)" }),
    });

    api.registerTool({
      name: "nex_update_record",
      label: "Update Record",
      description: "Update specific attributes on an existing record. Only the provided attributes are changed.",
      parameters: UpdateRecordParams,
      async execute(_toolCallId, params) {
        const { record_id, attributes } = params as Static<typeof UpdateRecordParams>;
        const result = await client.patch(`/v1/records/${encodeURIComponent(record_id)}`, { attributes });
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const DeleteRecordParams = Type.Object({
      record_id: Type.String({ description: "Record ID to delete" }),
    });

    api.registerTool({
      name: "nex_delete_record",
      label: "Delete Record",
      description: "Permanently delete a record. This cannot be undone.",
      parameters: DeleteRecordParams,
      async execute(_toolCallId, params) {
        const { record_id } = params as Static<typeof DeleteRecordParams>;
        const result = await client.delete(`/v1/records/${encodeURIComponent(record_id)}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const GetRecordTimelineParams = Type.Object({
      record_id: Type.String({ description: "Record ID" }),
      limit: Type.Optional(Type.Number({ description: "Max events (1-100, default: 50)" })),
      cursor: Type.Optional(Type.String({ description: "Pagination cursor from previous response" })),
    });

    api.registerTool({
      name: "nex_get_record_timeline",
      label: "Get Record Timeline",
      description: "Get paginated timeline events for a record (tasks, notes, attribute changes, etc.).",
      parameters: GetRecordTimelineParams,
      async execute(_toolCallId, params) {
        const { record_id, limit, cursor } = params as Static<typeof GetRecordTimelineParams>;
        const qs = new URLSearchParams();
        if (limit !== undefined) qs.set("limit", String(limit));
        if (cursor) qs.set("cursor", cursor);
        const q = qs.toString();
        const result = await client.get(`/v1/records/${encodeURIComponent(record_id)}/timeline${q ? `?${q}` : ""}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    // --- Search tools ---

    const SearchRecordsParams = Type.Object({
      query: Type.String({ description: "Search query (1-500 characters)" }),
    });

    api.registerTool({
      name: "nex_search_records",
      label: "Search Records",
      description: "Search records by name across all object types. Returns matches grouped by object type with relevance scores.",
      parameters: SearchRecordsParams,
      async execute(_toolCallId, params) {
        const { query } = params as Static<typeof SearchRecordsParams>;
        const result = await client.post("/v1/search", { query });
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    // --- Relationship tools ---

    const ListRelationshipDefsParams = Type.Object({});

    api.registerTool({
      name: "nex_list_relationship_defs",
      label: "List Relationship Definitions",
      description: "List all relationship type definitions in the workspace.",
      parameters: ListRelationshipDefsParams,
      async execute(_toolCallId, _params) {
        const result = await client.get("/v1/relationships");
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const CreateRelationshipDefParams = Type.Object({
      type: Type.String({ description: "Relationship cardinality: one_to_one, one_to_many, many_to_many" }),
      entity_definition_1_id: Type.String({ description: "First object definition ID" }),
      entity_definition_2_id: Type.String({ description: "Second object definition ID" }),
      entity_1_to_2_predicate: Type.Optional(Type.String({ description: "Label for 1→2 direction (e.g. 'works at')" })),
      entity_2_to_1_predicate: Type.Optional(Type.String({ description: "Label for 2→1 direction (e.g. 'employs')" })),
    });

    api.registerTool({
      name: "nex_create_relationship_def",
      label: "Create Relationship Definition",
      description: "Define a new relationship type between two object types (e.g. person 'works at' company).",
      parameters: CreateRelationshipDefParams,
      async execute(_toolCallId, params) {
        const { type, entity_definition_1_id, entity_definition_2_id, entity_1_to_2_predicate, entity_2_to_1_predicate } = params as Static<typeof CreateRelationshipDefParams>;
        const body: Record<string, unknown> = { type, entity_definition_1_id, entity_definition_2_id };
        if (entity_1_to_2_predicate !== undefined) body.entity_1_to_2_predicate = entity_1_to_2_predicate;
        if (entity_2_to_1_predicate !== undefined) body.entity_2_to_1_predicate = entity_2_to_1_predicate;
        const result = await client.post("/v1/relationships", body);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const DeleteRelationshipDefParams = Type.Object({
      id: Type.String({ description: "Relationship definition ID to delete" }),
    });

    api.registerTool({
      name: "nex_delete_relationship_def",
      label: "Delete Relationship Definition",
      description: "Delete a relationship type definition. Removes all instances of this relationship. Cannot be undone.",
      parameters: DeleteRelationshipDefParams,
      async execute(_toolCallId, params) {
        const { id } = params as Static<typeof DeleteRelationshipDefParams>;
        const result = await client.delete(`/v1/relationships/${encodeURIComponent(id)}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const CreateRelationshipParams = Type.Object({
      record_id: Type.String({ description: "Record ID to create the relationship from" }),
      definition_id: Type.String({ description: "Relationship definition ID" }),
      entity_1_id: Type.String({ description: "First record ID" }),
      entity_2_id: Type.String({ description: "Second record ID" }),
    });

    api.registerTool({
      name: "nex_create_relationship",
      label: "Create Relationship",
      description: "Link two records using an existing relationship definition.",
      parameters: CreateRelationshipParams,
      async execute(_toolCallId, params) {
        const { record_id, definition_id, entity_1_id, entity_2_id } = params as Static<typeof CreateRelationshipParams>;
        const result = await client.post(`/v1/records/${encodeURIComponent(record_id)}/relationships`, {
          definition_id,
          entity_1_id,
          entity_2_id,
        });
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const DeleteRelationshipParams = Type.Object({
      record_id: Type.String({ description: "Record ID" }),
      relationship_id: Type.String({ description: "Relationship instance ID to delete" }),
    });

    api.registerTool({
      name: "nex_delete_relationship",
      label: "Delete Relationship",
      description: "Remove a relationship between two records. Cannot be undone.",
      parameters: DeleteRelationshipParams,
      async execute(_toolCallId, params) {
        const { record_id, relationship_id } = params as Static<typeof DeleteRelationshipParams>;
        const result = await client.delete(`/v1/records/${encodeURIComponent(record_id)}/relationships/${encodeURIComponent(relationship_id)}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    // --- List tools ---

    const ListListsParams = Type.Object({
      object_slug: Type.String({ description: "Object type slug (e.g. 'person', 'company')" }),
      include_attributes: Type.Optional(Type.Boolean({ description: "Include attribute definitions" })),
    });

    api.registerTool({
      name: "nex_list_lists",
      label: "List Object Lists",
      description: "Get all lists associated with an object type.",
      parameters: ListListsParams,
      async execute(_toolCallId, params) {
        const { object_slug, include_attributes } = params as Static<typeof ListListsParams>;
        const qs = new URLSearchParams();
        if (include_attributes) qs.set("include_attributes", "true");
        const q = qs.toString();
        const result = await client.get(`/v1/objects/${encodeURIComponent(object_slug)}/lists${q ? `?${q}` : ""}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const CreateListParams = Type.Object({
      object_slug: Type.String({ description: "Object type slug" }),
      name: Type.String({ description: "List display name" }),
      slug: Type.String({ description: "URL-safe identifier" }),
      name_plural: Type.Optional(Type.String({ description: "Plural name" })),
      description: Type.Optional(Type.String({ description: "List description" })),
    });

    api.registerTool({
      name: "nex_create_list",
      label: "Create List",
      description: "Create a new list under an object type.",
      parameters: CreateListParams,
      async execute(_toolCallId, params) {
        const { object_slug, name, slug, name_plural, description } = params as Static<typeof CreateListParams>;
        const body: Record<string, unknown> = { name, slug };
        if (name_plural !== undefined) body.name_plural = name_plural;
        if (description !== undefined) body.description = description;
        const result = await client.post(`/v1/objects/${encodeURIComponent(object_slug)}/lists`, body);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const GetListParams = Type.Object({
      list_id: Type.String({ description: "List ID" }),
    });

    api.registerTool({
      name: "nex_get_list",
      label: "Get List",
      description: "Get a list definition by ID.",
      parameters: GetListParams,
      async execute(_toolCallId, params) {
        const { list_id } = params as Static<typeof GetListParams>;
        const result = await client.get(`/v1/lists/${encodeURIComponent(list_id)}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const DeleteListParams = Type.Object({
      list_id: Type.String({ description: "List ID to delete" }),
    });

    api.registerTool({
      name: "nex_delete_list",
      label: "Delete List",
      description: "Delete a list definition. Cannot be undone.",
      parameters: DeleteListParams,
      async execute(_toolCallId, params) {
        const { list_id } = params as Static<typeof DeleteListParams>;
        const result = await client.delete(`/v1/lists/${encodeURIComponent(list_id)}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const AddListMemberParams = Type.Object({
      list_id: Type.String({ description: "List ID" }),
      parent_id: Type.String({ description: "ID of the existing record to add" }),
      attributes: Type.Optional(Type.Record(Type.String(), Type.Unknown(), { description: "List-specific attribute values" })),
    });

    api.registerTool({
      name: "nex_add_list_member",
      label: "Add List Member",
      description: "Add an existing record to a list with optional list-specific attributes.",
      parameters: AddListMemberParams,
      async execute(_toolCallId, params) {
        const { list_id, parent_id, attributes } = params as Static<typeof AddListMemberParams>;
        const body: Record<string, unknown> = { parent_id };
        if (attributes) body.attributes = attributes;
        const result = await client.post(`/v1/lists/${encodeURIComponent(list_id)}`, body);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const UpsertListMemberParams = Type.Object({
      list_id: Type.String({ description: "List ID" }),
      parent_id: Type.String({ description: "ID of the record" }),
      attributes: Type.Optional(Type.Record(Type.String(), Type.Unknown(), { description: "List-specific attribute values" })),
    });

    api.registerTool({
      name: "nex_upsert_list_member",
      label: "Upsert List Member",
      description: "Add a record to a list, or update its list-specific attributes if already a member.",
      parameters: UpsertListMemberParams,
      async execute(_toolCallId, params) {
        const { list_id, parent_id, attributes } = params as Static<typeof UpsertListMemberParams>;
        const body: Record<string, unknown> = { parent_id };
        if (attributes) body.attributes = attributes;
        const result = await client.put(`/v1/lists/${encodeURIComponent(list_id)}`, body);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const ListListRecordsParams = Type.Object({
      list_id: Type.String({ description: "List ID" }),
      attributes: Type.Optional(Type.Union([
        Type.String({ description: "'all', 'primary', or 'none'" }),
        Type.Record(Type.String(), Type.Unknown()),
      ])),
      limit: Type.Optional(Type.Number({ description: "Number of records to return" })),
      offset: Type.Optional(Type.Number({ description: "Pagination offset" })),
      sort: Type.Optional(Type.Object({
        attribute: Type.String({ description: "Attribute slug to sort by" }),
        direction: Type.String({ description: "Sort direction: asc or desc" }),
      })),
    });

    api.registerTool({
      name: "nex_list_list_records",
      label: "List List Records",
      description: "Get paginated records from a specific list.",
      parameters: ListListRecordsParams,
      async execute(_toolCallId, params) {
        const { list_id, attributes, limit, offset, sort } = params as Static<typeof ListListRecordsParams>;
        const body: Record<string, unknown> = {};
        if (attributes !== undefined) body.attributes = attributes;
        if (limit !== undefined) body.limit = limit;
        if (offset !== undefined) body.offset = offset;
        if (sort) body.sort = sort;
        const result = await client.post(`/v1/lists/${encodeURIComponent(list_id)}/records`, body);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const UpdateListRecordParams = Type.Object({
      list_id: Type.String({ description: "List ID" }),
      record_id: Type.String({ description: "Record ID within the list" }),
      attributes: Type.Record(Type.String(), Type.Unknown(), { description: "Attributes to update" }),
    });

    api.registerTool({
      name: "nex_update_list_record",
      label: "Update List Record",
      description: "Update list-specific attributes for a record within a list.",
      parameters: UpdateListRecordParams,
      async execute(_toolCallId, params) {
        const { list_id, record_id, attributes } = params as Static<typeof UpdateListRecordParams>;
        const result = await client.patch(`/v1/lists/${encodeURIComponent(list_id)}/records/${encodeURIComponent(record_id)}`, { attributes });
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const RemoveListRecordParams = Type.Object({
      list_id: Type.String({ description: "List ID" }),
      record_id: Type.String({ description: "Record ID to remove from the list" }),
    });

    api.registerTool({
      name: "nex_remove_list_record",
      label: "Remove List Record",
      description: "Remove a record from a list. The record itself is not deleted.",
      parameters: RemoveListRecordParams,
      async execute(_toolCallId, params) {
        const { list_id, record_id } = params as Static<typeof RemoveListRecordParams>;
        const result = await client.delete(`/v1/lists/${encodeURIComponent(list_id)}/records/${encodeURIComponent(record_id)}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    // --- Task tools ---

    const CreateTaskParams = Type.Object({
      title: Type.String({ description: "Task title" }),
      description: Type.Optional(Type.String({ description: "Task description" })),
      priority: Type.Optional(Type.String({ description: "Task priority: low, medium, high, urgent" })),
      due_date: Type.Optional(Type.String({ description: "Due date in RFC3339 format" })),
      entity_ids: Type.Optional(Type.Array(Type.String({ description: "Record ID" }))),
      assignee_ids: Type.Optional(Type.Array(Type.String({ description: "User ID" }))),
    });

    api.registerTool({
      name: "nex_create_task",
      label: "Create Task",
      description: "Create a new task, optionally linked to records and assigned to users.",
      parameters: CreateTaskParams,
      async execute(_toolCallId, params) {
        const { title, description, priority, due_date, entity_ids, assignee_ids } = params as Static<typeof CreateTaskParams>;
        const body: Record<string, unknown> = { title };
        if (description !== undefined) body.description = description;
        if (priority !== undefined) body.priority = priority;
        if (due_date !== undefined) body.due_date = due_date;
        if (entity_ids) body.entity_ids = entity_ids;
        if (assignee_ids) body.assignee_ids = assignee_ids;
        const result = await client.post("/v1/tasks", body);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const ListTasksParams = Type.Object({
      entity_id: Type.Optional(Type.String({ description: "Filter by associated record ID" })),
      assignee_id: Type.Optional(Type.String({ description: "Filter by assignee user ID" })),
      search: Type.Optional(Type.String({ description: "Search task titles" })),
      is_completed: Type.Optional(Type.Boolean({ description: "Filter by completion status" })),
      limit: Type.Optional(Type.Number({ description: "Max results (1-500, default: 100)" })),
      offset: Type.Optional(Type.Number({ description: "Pagination offset" })),
    });

    api.registerTool({
      name: "nex_list_tasks",
      label: "List Tasks",
      description: "List tasks with optional filtering by record, assignee, completion status, or search query.",
      parameters: ListTasksParams,
      async execute(_toolCallId, params) {
        const { entity_id, assignee_id, search, is_completed, limit, offset } = params as Static<typeof ListTasksParams>;
        const qs = new URLSearchParams();
        if (entity_id) qs.set("entity_id", entity_id);
        if (assignee_id) qs.set("assignee_id", assignee_id);
        if (search) qs.set("search", search);
        if (is_completed !== undefined) qs.set("is_completed", String(is_completed));
        if (limit !== undefined) qs.set("limit", String(limit));
        if (offset !== undefined) qs.set("offset", String(offset));
        const q = qs.toString();
        const result = await client.get(`/v1/tasks${q ? `?${q}` : ""}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const GetTaskParams = Type.Object({
      task_id: Type.String({ description: "Task ID" }),
    });

    api.registerTool({
      name: "nex_get_task",
      label: "Get Task",
      description: "Get a single task by ID.",
      parameters: GetTaskParams,
      async execute(_toolCallId, params) {
        const { task_id } = params as Static<typeof GetTaskParams>;
        const result = await client.get(`/v1/tasks/${encodeURIComponent(task_id)}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const UpdateTaskParams = Type.Object({
      task_id: Type.String({ description: "Task ID to update" }),
      title: Type.Optional(Type.String({ description: "New title" })),
      description: Type.Optional(Type.String({ description: "New description" })),
      priority: Type.Optional(Type.String({ description: "New priority" })),
      due_date: Type.Optional(Type.String({ description: "New due date in RFC3339 format" })),
      is_completed: Type.Optional(Type.Boolean({ description: "Mark complete/incomplete" })),
      entity_ids: Type.Optional(Type.Array(Type.String({ description: "Record ID" }))),
      assignee_ids: Type.Optional(Type.Array(Type.String({ description: "User ID" }))),
    });

    api.registerTool({
      name: "nex_update_task",
      label: "Update Task",
      description: "Update a task's fields. All fields are optional — only provided fields are changed.",
      parameters: UpdateTaskParams,
      async execute(_toolCallId, params) {
        const { task_id, title, description, priority, due_date, is_completed, entity_ids, assignee_ids } = params as Static<typeof UpdateTaskParams>;
        const body: Record<string, unknown> = {};
        if (title !== undefined) body.title = title;
        if (description !== undefined) body.description = description;
        if (priority !== undefined) body.priority = priority;
        if (due_date !== undefined) body.due_date = due_date;
        if (is_completed !== undefined) body.is_completed = is_completed;
        if (entity_ids !== undefined) body.entity_ids = entity_ids;
        if (assignee_ids !== undefined) body.assignee_ids = assignee_ids;
        const result = await client.patch(`/v1/tasks/${encodeURIComponent(task_id)}`, body);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const DeleteTaskParams = Type.Object({
      task_id: Type.String({ description: "Task ID to delete" }),
    });

    api.registerTool({
      name: "nex_delete_task",
      label: "Delete Task",
      description: "Archive a task (soft delete). Cannot be undone via API.",
      parameters: DeleteTaskParams,
      async execute(_toolCallId, params) {
        const { task_id } = params as Static<typeof DeleteTaskParams>;
        const result = await client.delete(`/v1/tasks/${encodeURIComponent(task_id)}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    // --- Note tools ---

    const CreateNoteParams = Type.Object({
      title: Type.String({ description: "Note title" }),
      content: Type.Optional(Type.String({ description: "Note body text" })),
      entity_id: Type.Optional(Type.String({ description: "Associated record ID" })),
    });

    api.registerTool({
      name: "nex_create_note",
      label: "Create Note",
      description: "Create a new note, optionally linked to a record.",
      parameters: CreateNoteParams,
      async execute(_toolCallId, params) {
        const { title, content, entity_id } = params as Static<typeof CreateNoteParams>;
        const body: Record<string, unknown> = { title };
        if (content !== undefined) body.content = content;
        if (entity_id !== undefined) body.entity_id = entity_id;
        const result = await client.post("/v1/notes", body);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const ListNotesParams = Type.Object({
      entity_id: Type.Optional(Type.String({ description: "Filter notes by associated record ID" })),
    });

    api.registerTool({
      name: "nex_list_notes",
      label: "List Notes",
      description: "List notes, optionally filtered by associated record.",
      parameters: ListNotesParams,
      async execute(_toolCallId, params) {
        const { entity_id } = params as Static<typeof ListNotesParams>;
        const qs = new URLSearchParams();
        if (entity_id) qs.set("entity_id", entity_id);
        const q = qs.toString();
        const result = await client.get(`/v1/notes${q ? `?${q}` : ""}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const GetNoteParams = Type.Object({
      note_id: Type.String({ description: "Note ID" }),
    });

    api.registerTool({
      name: "nex_get_note",
      label: "Get Note",
      description: "Get a single note by ID.",
      parameters: GetNoteParams,
      async execute(_toolCallId, params) {
        const { note_id } = params as Static<typeof GetNoteParams>;
        const result = await client.get(`/v1/notes/${encodeURIComponent(note_id)}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const UpdateNoteParams = Type.Object({
      note_id: Type.String({ description: "Note ID to update" }),
      title: Type.Optional(Type.String({ description: "New title" })),
      content: Type.Optional(Type.String({ description: "New content" })),
      entity_id: Type.Optional(Type.String({ description: "Change associated record" })),
    });

    api.registerTool({
      name: "nex_update_note",
      label: "Update Note",
      description: "Update a note's fields. All fields are optional — only provided fields are changed.",
      parameters: UpdateNoteParams,
      async execute(_toolCallId, params) {
        const { note_id, title, content, entity_id } = params as Static<typeof UpdateNoteParams>;
        const body: Record<string, unknown> = {};
        if (title !== undefined) body.title = title;
        if (content !== undefined) body.content = content;
        if (entity_id !== undefined) body.entity_id = entity_id;
        const result = await client.patch(`/v1/notes/${encodeURIComponent(note_id)}`, body);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const DeleteNoteParams = Type.Object({
      note_id: Type.String({ description: "Note ID to delete" }),
    });

    api.registerTool({
      name: "nex_delete_note",
      label: "Delete Note",
      description: "Archive a note (soft delete). Cannot be undone via API.",
      parameters: DeleteNoteParams,
      async execute(_toolCallId, params) {
        const { note_id } = params as Static<typeof DeleteNoteParams>;
        const result = await client.delete(`/v1/notes/${encodeURIComponent(note_id)}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    // --- Context tools ---

    const GetArtifactStatusParams = Type.Object({
      artifact_id: Type.String({ description: "The artifact ID returned by nex_remember or add_context" }),
    });

    api.registerTool({
      name: "nex_get_artifact_status",
      label: "Get Artifact Status",
      description: "Check the processing status and results of a previously submitted text artifact. Poll until status is 'completed' or 'failed'.",
      parameters: GetArtifactStatusParams,
      async execute(_toolCallId, params) {
        const { artifact_id } = params as Static<typeof GetArtifactStatusParams>;
        const result = await client.get(`/v1/context/artifacts/${encodeURIComponent(artifact_id)}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    const GetInsightsParams = Type.Object({
      last: Type.Optional(Type.String({ description: "Duration window, e.g. '30m', '2h', '1h30m'" })),
      from: Type.Optional(Type.String({ description: "Start of time range in RFC3339 format" })),
      to: Type.Optional(Type.String({ description: "End of time range in RFC3339 format" })),
      limit: Type.Optional(Type.Number({ description: "Max results (default: 20, max: 100)" })),
    });

    api.registerTool({
      name: "nex_get_insights",
      label: "Get Insights",
      description: "Query insights by time window. Returns discovered opportunities, risks, relationship changes, milestones, and other insights.",
      parameters: GetInsightsParams,
      async execute(_toolCallId, params) {
        const { last, from, to, limit } = params as Static<typeof GetInsightsParams>;
        const qs = new URLSearchParams();
        if (last) qs.set("last", last);
        if (from) qs.set("from", from);
        if (to) qs.set("to", to);
        if (limit !== undefined) qs.set("limit", String(limit));
        const q = qs.toString();
        const result = await client.get(`/v1/insights${q ? `?${q}` : ""}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
      },
    });

    // --- Commands ---

    api.registerCommand({
      name: "recall",
      description: "Search your Nex knowledge base. Usage: /recall <query>",
      acceptsArgs: true,
      async handler(ctx) {
        const query = ctx.args?.trim();
        if (!query) {
          return { text: "Usage: /recall <query>" };
        }

        try {
          const result = await client.ask(query);
          const parts: string[] = [result.answer];

          if (result.entity_references && result.entity_references.length > 0) {
            const typeLabel = (t: string) => {
              switch (t) {
                case "14": return "Person";
                case "15": return "Company";
                default: return "Entity";
              }
            };
            // Deduplicate by name+type
            const seen = new Set<string>();
            const unique = result.entity_references.filter((ref) => {
              const key = `${ref.name}:${ref.type}`;
              if (seen.has(key)) return false;
              seen.add(key);
              return true;
            });
            parts.push("\n\nSources:");
            for (const ref of unique) {
              parts.push(`\n• ${ref.name} · ${typeLabel(ref.type)}`);
            }
          }

          return { text: parts.join("") };
        } catch (err) {
          return { text: `Recall failed: ${err instanceof Error ? err.message : String(err)}` };
        }
      },
    });

    api.registerCommand({
      name: "remember",
      description: "Store information in your Nex knowledge base. Usage: /remember <text>",
      acceptsArgs: true,
      async handler(ctx) {
        const text = ctx.args?.trim();
        if (!text) {
          return { text: "Usage: /remember <text>" };
        }

        try {
          await rateLimiter.enqueue(async () => {
            await client.ingest(text, "manual-command");
          });
          return { text: "Remembered." };
        } catch (err) {
          return { text: `Remember failed: ${err instanceof Error ? err.message : String(err)}` };
        }
      },
    });

    api.registerCommand({
      name: "scan",
      description: "Scan a directory for files and ingest into Nex. Usage: /scan [dir]",
      acceptsArgs: true,
      async handler(ctx) {
        const dir = ctx.args?.trim() || ".";
        try {
          const result = await scanFilesUtil(dir, client);
          return {
            text: `Scanned ${result.scanned} file(s), skipped ${result.skipped}, errors ${result.errors}.`,
          };
        } catch (err) {
          return { text: `Scan failed: ${err instanceof Error ? err.message : String(err)}` };
        }
      },
    });

    log.info(`Nex memory plugin registered (recall: ${cfg.autoRecall}, capture: ${cfg.autoCapture})`);
  },
};

export default plugin;
