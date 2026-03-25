/**
 * Shared hook logic — platform-agnostic recall, capture, and session-start.
 *
 * Extracted from auto-recall.ts, auto-capture.ts, auto-session-start.ts
 * so adapters for Cursor, Windsurf, Cline, etc. can reuse the same logic
 * without duplicating filter/client/format code.
 */

import { loadConfig, loadScanConfig, ConfigError, isHookEnabled, type NexConfig } from "./config.js";
import { NexClient, NexAuthError } from "./wuphf-client.js";
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
import { homedir } from "node:os";

export interface RecallResult {
  context: string;
  nexSessionId?: string;
}

export interface CaptureInput {
  message: string;
  sessionId?: string;
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
 * Run recall logic: filter prompt → query WUPHF /ask → return formatted context.
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

  let result;
  try {
    const nexSessionId = sessionKey ? sessions.get(sessionKey) : undefined;
    result = await client.ask(trimmed, nexSessionId, 30_000);
  } catch (err) {
    if (err instanceof NexAuthError) {
      return {
        context: "[WUPHF] API key expired or invalid. Run: wuphf register --email <email>",
      };
    }
    throw err;
  }

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
 * Run capture logic: filter message → ingest to WUPHF + scan plan files.
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
          if (err instanceof NexAuthError) {
            process.stderr.write(
              `[wuphf-capture] API key expired or invalid. Run 'wuphf register --email <email>' to get a new key.\n`
            );
            return;
          }
          process.stderr.write(
            `[wuphf-capture] Ingest failed: ${err instanceof Error ? err.message : String(err)}\n`
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
      `[wuphf-capture] Plan file scan error: ${err instanceof Error ? err.message : String(err)}\n`
    );
  }

  // Transcript ingestion — ingest the full session conversation
  if (input.sessionId) {
    try {
      await ingestTranscript(client, input.sessionId);
    } catch {
      // non-fatal
    }
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

/**
 * Read a Claude Code session transcript and ingest the full conversation.
 *
 * Extracts user and assistant messages from the JSONL transcript file,
 * builds a condensed conversation text, and sends it to WUPHF in one shot.
 * This captures the full context instead of just the last assistant message.
 */
const MAX_TRANSCRIPT_LENGTH = 100_000;

async function ingestTranscript(client: NexClient, sessionId: string): Promise<void> {
  const cwd = process.cwd();
  // Claude Code stores transcripts at ~/.claude/projects/<project-hash>/<session-id>.jsonl
  // Project hash is the CWD path with / replaced by -
  const projectHash = "-" + cwd.replace(/\//g, "-");
  const transcriptPath = join(homedir(), ".claude", "projects", projectHash, `${sessionId}.jsonl`);

  let raw: string;
  try {
    raw = readFileSync(transcriptPath, "utf-8");
  } catch {
    return; // transcript doesn't exist yet or path is wrong
  }

  // Extract user/assistant messages
  const lines: string[] = [];
  for (const line of raw.split("\n")) {
    if (!line.trim()) continue;
    try {
      const entry = JSON.parse(line);
      const role = entry.message?.role;
      if (role !== "user" && role !== "assistant") continue;

      let text = "";
      const content = entry.message?.content;
      if (typeof content === "string") {
        text = content;
      } else if (Array.isArray(content)) {
        text = content
          .filter((c: any) => c.type === "text")
          .map((c: any) => c.text)
          .join("\n");
      }

      if (!text.trim()) continue;
      // Strip wuphf-context blocks to avoid feedback loops
      text = text.replace(/<wuphf-context>[\s\S]*?<\/wuphf-context>/g, "").trim();
      if (!text) continue;

      lines.push(`[${role}]: ${text}`);
    } catch {
      continue;
    }
  }

  if (lines.length < 2) return; // need at least one exchange

  let transcript = lines.join("\n\n");
  if (transcript.length > MAX_TRANSCRIPT_LENGTH) {
    // Keep the most recent messages (end of conversation is most valuable)
    transcript = transcript.slice(-MAX_TRANSCRIPT_LENGTH);
    transcript = "[...earlier messages truncated]\n\n" + transcript;
  }

  await client.ingest(transcript, `claude-code-transcript:${sessionId}`, 30_000);
}

// ── Session Start ───────────────────────────────────────────────────────

const SESSION_START_QUERY =
  "Summarize the key active context, recent interactions, and important updates for this user.";

/**
 * Run session-start logic: scan files → query WUPHF baseline → return context.
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
          // Trigger compounding intelligence after ingestion (non-blocking, best-effort)
          triggerCompounding(cfg).catch(() => {});
        }
      }
    } catch {
      // non-fatal
    }
  }

  let result;
  try {
    result = await client.ask(SESSION_START_QUERY, undefined, 30_000);
  } catch (err) {
    if (err instanceof NexAuthError) {
      return {
        context: "",
        registrationPrompt: buildAuthExpiredPrompt(),
      };
    }
    throw err;
  }

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
    "[WUPHF Memory Plugin] No API key configured. Registration is required to enable persistent memory.",
    "",
    "IMPORTANT: Before proceeding with the user's request, you must set up WUPHF memory:",
    "1. Ask the user for their email address to register for a WUPHF account",
    "2. Run: wuphf register --email <email>",
    "3. Then run: wuphf setup",
    "",
    "Keep it brief — just ask for the email and register. No restart needed after registration.",
  ].join("\n");
}

function buildAuthExpiredPrompt(): string {
  return [
    "[WUPHF Memory Plugin] API key expired or invalid. Re-registration is required to restore persistent memory.",
    "",
    "IMPORTANT: Before proceeding with the user's request, you must renew the WUPHF API key:",
    "1. Ask the user for their email address",
    "2. Run: wuphf register --email <email>",
    "3. Then run: wuphf setup",
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

/**
 * Trigger compounding intelligence jobs after content ingestion.
 * Runs consolidation, pattern detection, and playbook synthesis.
 * Best-effort — errors are silently ignored.
 */
async function triggerCompounding(cfg: NexConfig): Promise<void> {
  const jobs = ["consolidation", "pattern_detection", "playbook_synthesis"];
  const url = `${cfg.baseUrl}/api/developers/v1/compounding/trigger`;
  await Promise.allSettled(
    jobs.map((job) =>
      fetch(url, {
        method: "POST",
        headers: { Authorization: `Bearer ${cfg.apiKey}`, "Content-Type": "application/json" },
        body: JSON.stringify({ job_type: job, dry_run: false }),
        signal: AbortSignal.timeout(30_000),
      }),
    ),
  );
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
