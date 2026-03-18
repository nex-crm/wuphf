/**
 * Shared hook logic — platform-agnostic recall, capture, and session-start.
 *
 * Extracted from auto-recall.ts, auto-capture.ts, auto-session-start.ts
 * so adapters for Cursor, Windsurf, Cline, etc. can reuse the same logic
 * without duplicating filter/client/format code.
 */

import { loadConfig, loadScanConfig, ConfigError, isHookEnabled } from "./config.js";
import { NexClient } from "./nex-client.js";
import { formatNexContext } from "./context-format.js";
import { captureFilter } from "./capture-filter.js";
import { SessionStore } from "./session-store.js";
import { shouldRecall, recordRecall } from "./recall-filter.js";
import { RateLimiter } from "./rate-limiter.js";
import { scanAndIngest } from "./file-scanner.js";
import { ingestContextFiles } from "./context-files.js";
import { readManifest, writeManifest, isChanged, markIngested } from "./file-manifest.js";
import { readdirSync, statSync, readFileSync } from "node:fs";
import { join, extname } from "node:path";

export interface RecallResult {
  context: string;
  nexSessionId?: string;
}

export interface CaptureInput {
  message: string;
  planDir?: string;
}

export interface SessionStartResult {
  context: string;
  nexSessionId?: string;
  registrationPrompt?: string;
}

const sessions = new SessionStore();

// ── Recall ──────────────────────────────────────────────────────────────

/**
 * Run recall logic: filter prompt → query Nex /ask → return formatted context.
 * Returns null if recall is skipped or fails.
 */
export async function doRecall(
  prompt: string,
  sessionKey?: string,
): Promise<RecallResult | null> {
  if (!isHookEnabled("recall")) return null;

  const trimmed = prompt?.trim();
  if (!trimmed || trimmed.length < 5) return null;
  if (trimmed.startsWith("/")) return null;

  const isFirst = sessionKey ? !sessions.get(sessionKey) : true;
  const decision = shouldRecall(trimmed, isFirst);
  if (!decision.shouldRecall) return null;

  const cfg = loadConfig();
  const client = new NexClient(cfg.apiKey, cfg.baseUrl);

  const nexSessionId = sessionKey ? sessions.get(sessionKey) : undefined;
  const result = await client.ask(trimmed, nexSessionId, 30_000);

  if (!result.answer) return null;

  if (result.session_id && sessionKey) {
    sessions.set(sessionKey, result.session_id);
  }
  recordRecall(result.session_id);

  const entityCount = result.entity_references?.length ?? 0;
  const context = formatNexContext({
    answer: result.answer,
    entityCount,
    sessionId: result.session_id,
  });

  return { context, nexSessionId: result.session_id };
}

// ── Capture ─────────────────────────────────────────────────────────────

const INGEST_TIMEOUT_MS = 3_000;
const MAX_PLAN_FILES = 2;

/**
 * Run capture logic: filter message → ingest to Nex + scan plan files.
 */
export async function doCapture(input: CaptureInput): Promise<void> {
  if (!isHookEnabled("capture")) return;

  let cfg;
  try {
    cfg = loadConfig();
  } catch {
    return;
  }

  const client = new NexClient(cfg.apiKey, cfg.baseUrl);
  const rateLimiter = new RateLimiter();

  // Conversation capture
  const message = input.message?.trim();
  if (message) {
    const filterResult = captureFilter(message);
    if (!filterResult.skipped) {
      if (rateLimiter.canProceed()) {
        try {
          await client.ingest(filterResult.text, "claude-code-conversation", INGEST_TIMEOUT_MS);
        } catch (err) {
          process.stderr.write(
            `[nex-capture] Ingest failed: ${err instanceof Error ? err.message : String(err)}\n`
          );
        }
      }
    }
  }

  // Plan file ingestion
  const plansDir = input.planDir ?? join(process.cwd(), ".claude", "plans");
  try {
    await ingestPlanFiles(client, rateLimiter, plansDir);
  } catch (err) {
    process.stderr.write(
      `[nex-capture] Plan file scan error: ${err instanceof Error ? err.message : String(err)}\n`
    );
  }
}

async function ingestPlanFiles(
  client: NexClient,
  rateLimiter: RateLimiter,
  plansDir: string,
): Promise<void> {
  const scanConfig = loadScanConfig();
  if (!scanConfig.enabled) return;

  let entries;
  try {
    entries = readdirSync(plansDir, { withFileTypes: true });
  } catch {
    return;
  }

  const manifest = readManifest();
  let ingested = 0;

  for (const entry of entries) {
    if (ingested >= MAX_PLAN_FILES) break;
    if (!entry.isFile()) continue;
    if (extname(entry.name).toLowerCase() !== ".md") continue;

    const fullPath = join(plansDir, entry.name);
    try {
      const stat = statSync(fullPath);
      if (!isChanged(fullPath, stat, manifest)) continue;

      if (!rateLimiter.canProceed()) break;

      let content = readFileSync(fullPath, "utf-8");
      if (content.length > 100_000) {
        content = content.slice(0, 100_000) + "\n[...truncated]";
      }

      const context = `claude-code-plan:${entry.name}`;
      await client.ingest(content, context, INGEST_TIMEOUT_MS);
      markIngested(fullPath, stat, context, manifest);
      ingested++;
    } catch {
      // skip individual file errors
    }
  }

  if (ingested > 0) writeManifest(manifest);
}

// ── Session Start ───────────────────────────────────────────────────────

const SESSION_START_QUERY =
  "Summarize the key active context, recent interactions, and important updates for this user.";

/**
 * Run session-start logic: scan files → query Nex baseline → return context.
 * Returns null if disabled. Returns registrationPrompt if no API key.
 */
export async function doSessionStart(
  source: string,
  sessionKey?: string,
): Promise<SessionStartResult | null> {
  if (!isHookEnabled("session_start")) return null;

  let cfg;
  try {
    cfg = loadConfig();
  } catch (err) {
    if (err instanceof ConfigError) {
      // No API key — return registration instructions
      return {
        context: "",
        registrationPrompt: buildRegistrationPrompt(),
      };
    }
    return null;
  }

  const client = new NexClient(cfg.apiKey, cfg.baseUrl);
  const contextParts: string[] = [];

  // File scan on startup or clear
  const shouldScan = source === "startup" || source === "clear";
  if (shouldScan) {
    const rateLimiter = new RateLimiter();
    const cwd = process.cwd();

    try {
      const ctxResult = await ingestContextFiles(client, rateLimiter, cwd);
      if (ctxResult.ingested > 0) {
        contextParts.push(
          `[Context files: ${ctxResult.ingested} ingested (${ctxResult.files.join(", ")})]`
        );
      }
    } catch {
      // non-fatal
    }

    try {
      const scanConfig = loadScanConfig();
      if (scanConfig.enabled) {
        const scanResult = await scanAndIngest(client, rateLimiter, cwd, scanConfig);
        if (scanResult.ingested > 0) {
          contextParts.push(
            `[File scan: ${scanResult.ingested} file${scanResult.ingested === 1 ? "" : "s"} ingested, ${scanResult.scanned} scanned]`
          );
        }
      }
    } catch {
      // non-fatal
    }
  }

  const result = await client.ask(SESSION_START_QUERY, undefined, 30_000);

  if (!result.answer && contextParts.length === 0) return null;

  if (result.session_id && sessionKey) {
    sessions.set(sessionKey, result.session_id);
  }
  recordRecall(result.session_id);

  const entityCount = result.entity_references?.length ?? 0;
  const context = formatNexContext({
    answer: result.answer,
    entityCount,
    sessionId: result.session_id,
  });

  const fullContext =
    contextParts.length > 0
      ? `${context}\n${contextParts.join("\n")}`
      : context;

  return { context: fullContext, nexSessionId: result.session_id };
}

function buildRegistrationPrompt(): string {
  return [
    "[Nex Memory Plugin] No API key configured. Registration is required to enable persistent memory.",
    "",
    "IMPORTANT: Before proceeding with the user's request, you must set up Nex memory:",
    "1. Ask the user for their email address to register for a Nex account",
    "2. Run: nex register --email <email>",
    "3. Then run: nex setup",
    "",
    "Keep it brief — just ask for the email and register. No restart needed after registration.",
  ].join("\n");
}

// ── Helpers ─────────────────────────────────────────────────────────────

/** Read stdin as a string (shared across all hook scripts). */
export async function readStdin(): Promise<string> {
  const chunks: Buffer[] = [];
  for await (const chunk of process.stdin) {
    chunks.push(chunk as Buffer);
  }
  return Buffer.concat(chunks).toString("utf-8");
}

/** Wrap output in Claude Code hookSpecificOutput format. */
export function claudeCodeOutput(
  hookEventName: string,
  additionalContext: string,
): string {
  return JSON.stringify({
    hookSpecificOutput: {
      hookEventName,
      additionalContext,
    },
  });
}
