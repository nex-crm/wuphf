#!/usr/bin/env node
/**
 * Claude Bridge — standalone HTTP server that spawns `claude -p` and returns the response.
 *
 * Runs as a separate Node.js process so child_process operations work normally
 * (Bun+Ink's event loop stalls on child processes).
 *
 * Usage: node src/agent/providers/claude-bridge.ts
 *    or: tsx src/agent/providers/claude-bridge.ts
 *
 * Prints the port number to stdout on startup so the parent process can discover it.
 */

import { createServer } from "node:http";
import { spawn } from "node:child_process";

// ── NDJSON parser (same logic as parseClaudeOutput) ──────────────

function parseClaudeOutput(stdout: string): { text: string; sessionId?: string } {
  const textChunks: string[] = [];
  let sessionId: string | undefined;

  for (const line of stdout.split("\n")) {
    if (!line.trim()) continue;
    let event: Record<string, unknown>;
    try { event = JSON.parse(line); } catch { continue; }

    if (!sessionId && event.session_id) sessionId = event.session_id as string;

    if (event.type === "assistant" && event.message) {
      const msg = event.message as Record<string, unknown>;
      const content = msg.content as Array<Record<string, unknown>> | undefined;
      if (content) {
        for (const part of content) {
          if (part.type === "text" && part.text) textChunks.push(part.text as string);
        }
      }
    }

    if (event.type === "result" && textChunks.length === 0) {
      const resultText = event.result as string | undefined;
      if (resultText) textChunks.push(resultText);
    }
  }

  return { text: textChunks.join("").trim(), sid: sessionId } as unknown as { text: string; sessionId?: string };
}

// ── Build clean env (strip Claude nesting vars) ──────────────────

function buildCleanEnv(): NodeJS.ProcessEnv {
  const env = { ...process.env };
  delete env.CLAUDECODE;
  delete env.CLAUDE_CODE_ENTRYPOINT;
  delete env.CLAUDE_CODE_SESSION;
  delete env.CLAUDE_CODE_PARENT_SESSION;
  return env;
}

// ── Request handler ──────────────────────────────────────────────

interface InvokeRequest {
  prompt: string;
}

function handleInvoke(body: InvokeRequest): Promise<{ text: string; sessionId?: string }> {
  return new Promise((resolve) => {
    const args = [
      "-p",
      "--output-format", "stream-json",
      "--verbose",
      "--max-turns", "5",
      "--no-session-persistence",
      "--allowedTools", "Read,Glob,Grep,WebSearch,WebFetch",
    ];

    const child = spawn("claude", args, {
      stdio: ["pipe", "pipe", "ignore"],
      shell: false,
      env: buildCleanEnv(),
    });

    // Send prompt via stdin (Paperclip pattern)
    child.stdin.write(body.prompt);
    child.stdin.end();

    const chunks: Buffer[] = [];

    child.stdout.on("data", (chunk: Buffer) => {
      chunks.push(chunk);
    });

    child.on("close", (code) => {
      const stdout = Buffer.concat(chunks).toString("utf-8");
      if (code !== 0 && !stdout.trim()) {
        resolve({ text: `Claude exited with code ${code}` });
        return;
      }
      const parsed = parseClaudeOutput(stdout);
      resolve({ text: parsed.text || "(no response)", sessionId: parsed.sessionId });
    });

    child.on("error", (err) => {
      resolve({ text: `Failed to spawn claude: ${err.message}` });
    });

    // Safety timeout: 120s
    setTimeout(() => {
      try { child.kill("SIGTERM"); } catch {}
      resolve({ text: "Claude timed out after 120s" });
    }, 120_000);
  });
}

// ── HTTP server ──────────────────────────────────────────────────

const server = createServer((req, res) => {
  // Health check
  if (req.method === "GET" && req.url === "/health") {
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ ok: true }));
    return;
  }

  // Invoke endpoint
  if (req.method === "POST" && req.url === "/invoke") {
    const bodyChunks: Buffer[] = [];
    req.on("data", (chunk: Buffer) => bodyChunks.push(chunk));
    req.on("end", () => {
      let parsed: InvokeRequest;
      try {
        parsed = JSON.parse(Buffer.concat(bodyChunks).toString("utf-8")) as InvokeRequest;
      } catch {
        res.writeHead(400, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ error: "Invalid JSON" }));
        return;
      }

      if (!parsed.prompt || typeof parsed.prompt !== "string") {
        res.writeHead(400, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ error: "Missing prompt" }));
        return;
      }

      handleInvoke(parsed).then((result) => {
        res.writeHead(200, { "Content-Type": "application/json" });
        res.end(JSON.stringify(result));
      });
    });
    return;
  }

  res.writeHead(404, { "Content-Type": "application/json" });
  res.end(JSON.stringify({ error: "Not found" }));
});

// Listen on port 0 (random available port)
server.listen(0, "127.0.0.1", () => {
  const addr = server.address();
  if (addr && typeof addr === "object") {
    const port = addr.port;
    // Print port to stdout
    console.log(port);
    // Also write to port file if specified (for sync startup discovery)
    const portFile = process.env.NEX_BRIDGE_PORT_FILE;
    if (portFile) {
      const { writeFileSync } = require("node:fs") as typeof import("node:fs");
      writeFileSync(portFile, String(port));
    }
  }
});

// Graceful shutdown
process.on("SIGTERM", () => {
  server.close(() => process.exit(0));
});
process.on("SIGINT", () => {
  server.close(() => process.exit(0));
});
