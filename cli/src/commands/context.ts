/**
 * Context commands: ask, remember, artifact, recall, capture.
 */

import { program } from "../cli.js";
import { NexClient } from "../lib/client.js";
import { resolveApiKey, resolveFormat, resolveTimeout } from "../lib/config.js";
import { printOutput } from "../lib/output.js";
import type { Format } from "../lib/output.js";
import { box, keyValue, style, sym, isTTY } from "../lib/tui.js";
import { shouldRecall, recordRecall } from "../lib/recall-filter.js";
import { formatNexContext } from "../lib/context-format.js";
import type { NexRecallResult } from "../lib/context-format.js";
import { captureFilter } from "../lib/capture-filter.js";
import { RateLimiter } from "../lib/rate-limiter.js";
import { SessionStore } from "../lib/session-store.js";

function getClient(): { client: NexClient; format: Format } {
  const opts = program.opts();
  const client = new NexClient(resolveApiKey(opts.apiKey), resolveTimeout(opts.timeout));
  return { client, format: resolveFormat(opts.format) as Format };
}

async function readStdin(): Promise<string | undefined> {
  if (process.stdin.isTTY) return undefined;
  const chunks: Buffer[] = [];
  for await (const chunk of process.stdin) {
    chunks.push(chunk as Buffer);
  }
  const text = Buffer.concat(chunks).toString("utf-8").trim();
  return text || undefined;
}

interface AskResult {
  answer?: string;
  entityCount?: number;
  sessionId?: string;
  [k: string]: unknown;
}

function formatAskText(data: unknown): string | undefined {
  const d = data as AskResult;
  if (!d || typeof d !== "object" || !d.answer) return undefined;

  if (!isTTY) {
    // Clean text output — just the answer
    return d.answer;
  }

  // Rich TTY output — boxed answer with metadata
  const lines: string[] = [];
  lines.push(box("Context Answer", d.answer));

  const meta: [string, string][] = [];
  if (d.entityCount !== undefined && d.entityCount > 0) {
    meta.push(["Entities", String(d.entityCount)]);
  }
  if (meta.length > 0) {
    lines.push(keyValue(meta));
  }

  return lines.join("\n");
}

interface RememberResult {
  artifact_id?: string;
  status?: string;
  [k: string]: unknown;
}

function formatRememberText(data: unknown): string | undefined {
  const d = data as RememberResult;
  if (!d || typeof d !== "object") return undefined;

  if (d.status === "completed" || d.artifact_id) {
    return `${sym.success} Stored${d.artifact_id ? style.dim(` (artifact: ${d.artifact_id})`) : ""}`;
  }
  if (d.status) {
    return `${sym.info} ${d.status}${d.artifact_id ? style.dim(` (artifact: ${d.artifact_id})`) : ""}`;
  }
  return undefined;
}

program
  .command("ask")
  .description("Query your context graph with AI (recommended for most lookups)")
  .argument("[query]", "The question to ask")
  .action(async (query?: string) => {
    const input = query ?? (await readStdin());
    if (!input) {
      throw new Error("No query provided. Pass as argument or pipe via stdin.");
    }

    const { client, format } = getClient();
    const body: Record<string, string> = { query: input };

    const sessionId = program.opts().session;
    if (sessionId) {
      body.session_id = sessionId;
    }

    const result = await client.post("/v1/context/ask", body);
    printOutput(result, format, formatAskText);
  });

program
  .command("remember")
  .description("Store a note, fact, or observation in your context graph")
  .argument("[content]", "The content to remember")
  .option("--context <context>", "Additional context")
  .action(async (content: string | undefined, opts: { context?: string }) => {
    const input = content ?? (await readStdin());
    if (!input) {
      throw new Error("No content provided. Pass as argument or pipe via stdin.");
    }

    const { client, format } = getClient();
    const body: Record<string, string> = { content: input };
    if (opts.context) {
      body.context = opts.context;
    }

    const result = await client.post("/v1/context/text", body);
    printOutput(result, format, formatRememberText);
  });

program
  .command("artifact")
  .description("Retrieve a context artifact by ID")
  .argument("<id>", "Artifact ID")
  .action(async (id: string) => {
    const { client, format } = getClient();
    const result = await client.get(`/v1/context/artifacts/${encodeURIComponent(id)}`);
    printOutput(result, format);
  });

program
  .command("recall")
  .description("Recall context for LLM prompt injection (used by hooks — prefer 'ask' for direct use)")
  .argument("[query]", "The query to recall context for")
  .option("--first-prompt", "Treat as first prompt in session")
  .action(async (query?: string, opts?: { firstPrompt?: boolean }) => {
    const input = query ?? (await readStdin());
    if (!input) {
      throw new Error("No query provided. Pass as argument or pipe via stdin.");
    }

    const isFirst = opts?.firstPrompt ?? false;
    const decision = shouldRecall(input, isFirst);
    if (!decision.shouldRecall) {
      process.exit(0);
    }

    const { client } = getClient();
    const body: Record<string, string> = { query: input };

    const sessionId = program.opts().session;
    if (sessionId) {
      body.session_id = sessionId;
    }

    const result = await client.post<NexRecallResult>("/v1/context/ask", body);
    recordRecall();

    const formatted = formatNexContext({
      answer: result.answer ?? "",
      entityCount: result.entityCount ?? 0,
      sessionId: result.sessionId,
    });
    process.stdout.write(formatted + "\n");
  });

program
  .command("capture")
  .description("Auto-capture content with filtering and rate limiting (used by hooks — prefer 'remember' for direct use)")
  .argument("[content]", "The content to capture")
  .action(async (content?: string) => {
    const input = content ?? (await readStdin());
    if (!input) {
      throw new Error("No content provided. Pass as argument or pipe via stdin.");
    }

    const filtered = captureFilter(input);
    if (filtered.skipped) {
      process.stderr.write(`capture skipped: ${filtered.reason}\n`);
      process.exit(0);
    }

    const limiter = new RateLimiter();
    if (!limiter.canProceed()) {
      process.stderr.write("capture skipped: rate limited\n");
      process.exit(0);
    }

    const { client, format } = getClient();
    const body: Record<string, string> = { content: filtered.text };

    const sessionId = program.opts().session;
    if (sessionId) {
      const store = new SessionStore();
      const existing = store.get(sessionId);
      if (existing) {
        body.session_id = existing;
      }
    }

    const result = await client.post("/v1/context/text", body, 60_000);
    printOutput(result, format);
  });
