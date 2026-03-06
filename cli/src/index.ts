#!/usr/bin/env node

/**
 * Entry point: stdin detection, error handling, exit codes.
 */

import { program } from "./cli.js";
import { AuthError, RateLimitError, ServerError } from "./lib/errors.js";
import { printError } from "./lib/output.js";

// Import command registrations
import "./commands/register.js";
import "./commands/context.js";
import "./commands/config-cmd.js";
import "./commands/search.js";
import "./commands/insight.js";
import "./commands/object.js";
import "./commands/attribute.js";
import "./commands/record.js";
import "./commands/relationship.js";
import "./commands/list.js";
import "./commands/list-job.js";
import "./commands/task.js";
import "./commands/note.js";
import "./commands/session.js";
import "./commands/scan.js";
import "./commands/integrate.js";

/**
 * Read all data from stdin (non-TTY only).
 */
export async function readStdin(): Promise<string | undefined> {
  if (process.stdin.isTTY) return undefined;

  const chunks: Buffer[] = [];
  for await (const chunk of process.stdin) {
    chunks.push(chunk as Buffer);
  }
  const text = Buffer.concat(chunks).toString("utf-8").trim();
  return text || undefined;
}

async function main(): Promise<void> {
  try {
    await program.parseAsync(process.argv);
  } catch (err) {
    if (err instanceof AuthError) {
      printError(err.message);
      process.exit(2);
    }
    if (err instanceof RateLimitError) {
      printError(err.message);
      process.exit(1);
    }
    if (err instanceof ServerError) {
      printError(err.message);
      process.exit(1);
    }
    if (err instanceof Error) {
      printError(err.message);
      if (program.opts().debug) {
        console.error(err.stack);
      }
      process.exit(1);
    }
    printError(String(err));
    process.exit(1);
  }
}

main();
