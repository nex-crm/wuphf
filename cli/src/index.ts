#!/usr/bin/env node

/**
 * Entry point.
 *
 * Interactive terminal → TUI (default)
 * --cmd <input>       → single command, print result, exit
 * Piped stdin         → read input, dispatch, exit
 */

import { dispatch, dispatchTokens, commandNames } from "./commands/dispatch.js";
import { resolveFormat } from "./lib/config.js";
import type { Format } from "./lib/output.js";

/**
 * Extract global flags (--format, --api-key, --timeout) from args.
 * Returns the cleaned args and a context object for dispatch.
 */
function extractGlobalFlags(args: string[]): { cleanArgs: string[]; ctx: { format: Format; apiKey?: string; timeout?: number } } {
  const cleanArgs: string[] = [];
  let format: string | undefined;
  let apiKey: string | undefined;
  let timeout: number | undefined;

  for (let i = 0; i < args.length; i++) {
    if (args[i] === "--format" && args[i + 1]) {
      format = args[++i];
    } else if (args[i] === "--api-key" && args[i + 1]) {
      apiKey = args[++i];
    } else if (args[i] === "--timeout" && args[i + 1]) {
      timeout = parseInt(args[++i], 10);
    } else {
      cleanArgs.push(args[i]);
    }
  }

  return {
    cleanArgs,
    ctx: {
      format: (format ?? resolveFormat()) as Format,
      apiKey,
      timeout,
    },
  };
}

/** Print result to stdout/stderr and exit with the appropriate code. */
function emitAndExit(result: { output: string; error?: string; exitCode: number }): never {
  if (result.output) console.log(result.output);
  if (result.error) process.stderr.write(result.error + "\n");
  process.exit(result.exitCode);
}

async function main(): Promise<void> {
  const args = process.argv.slice(2);

  // --version flag
  if (args.includes("--version") || args.includes("-v")) {
    const pkg: any = await import("../package.json");
    const version = pkg.default?.version ?? pkg.version;
    console.log(version);
    process.exit(0);
  }

  // --help flag
  if (args.includes("--help") || args.includes("-h")) {
    console.log(`Usage: wuphf [command] [options]

Commands:
  ask <query>           Query your context graph with AI
  remember <content>    Store a note, fact, or observation
  object list           List all objects
  object get <slug>     Get an object by slug
  record list <slug>    List records for an object
  record get <id>       Get a record by ID
  task list             List tasks
  note list             List notes
  search <query>        Search across records
  graph                 View relationship graph
  detect                Detect installed AI coding platforms
  setup                 Configure API key and workspace
  agent templates       List available agent templates
  agent create          Create a new agent

Options:
  --cmd <input>         Run a single command and exit
  --format <fmt>        Output format: json, text, quiet
  --version             Show version number
  --help                Show this help message

When no command is given, launches the interactive TUI.`);
    process.exit(0);
  }

  // Extract global flags (--format, --api-key, --timeout) from args
  const { cleanArgs, ctx } = extractGlobalFlags(args);

  // --cmd "ask who is important" → run one command and exit
  const cmdIdx = cleanArgs.indexOf("--cmd");
  if (cmdIdx >= 0 && cleanArgs[cmdIdx + 1]) {
    const input = cleanArgs.slice(cmdIdx + 1).join(" ");
    const result = await dispatch(input, ctx);
    emitAndExit(result);
  }

  // Interactive commands → fall through to Commander (rich TUI with pickers, spinners, workflows)
  const INTERACTIVE_COMMANDS = new Set(["setup", "integrate", "scan", "register", "status"]);
  const firstArg = cleanArgs[0]?.toLowerCase();

  if (firstArg && !INTERACTIVE_COMMANDS.has(firstArg)) {
    // Non-interactive subcommand → dispatch and exit
    const result = await dispatchTokens(cleanArgs, ctx);
    if (!result.error?.startsWith("Unknown command")) {
      emitAndExit(result);
    }
    // Unknown command falls through to Commander / TUI
  }

  // Piped stdin → dispatch each line
  if (!process.stdin.isTTY) {
    const chunks: Buffer[] = [];
    for await (const chunk of process.stdin) {
      chunks.push(chunk as Buffer);
    }
    const input = Buffer.concat(chunks).toString("utf-8").trim();
    if (input) {
      const result = await dispatch(input, ctx);
      emitAndExit(result);
    }
    process.exit(0);
  }

  // Interactive commands or no subcommand → Commander (setup, integrate, scan, register)
  if (firstArg && INTERACTIVE_COMMANDS.has(firstArg)) {
    // Load Commander command modules to register their handlers
    await import("./commands/setup.js");
    await import("./commands/scan.js");
    await import("./commands/register.js");
    await import("./commands/integrate.js");
    const { program } = await import("./cli.js");
    program.parse(["node", "wuphf", ...args]);
    return;
  }

  // No subcommand → TUI
  const { startTui } = await import("./tui/index.js");
  startTui();
}

main();
