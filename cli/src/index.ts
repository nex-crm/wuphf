#!/usr/bin/env node

/**
 * Entry point.
 *
 * Interactive terminal → TUI (default)
 * --cmd <input>       → single command, print result, exit
 * Piped stdin         → read input, dispatch, exit
 */

import { dispatch, commandNames } from "./commands/dispatch.js";

async function main(): Promise<void> {
  const args = process.argv.slice(2);

  // --cmd "ask who is important" → run one command and exit
  const cmdIdx = args.indexOf("--cmd");
  if (cmdIdx >= 0 && args[cmdIdx + 1]) {
    const input = args.slice(cmdIdx + 1).join(" ");
    const result = await dispatch(input);
    if (result.output) console.log(result.output);
    process.exit(result.exitCode);
  }

  // Direct subcommand: `nex graph`, `nex scan`, etc. → dispatch and exit
  if (args.length > 0 && !args[0].startsWith("-")) {
    const candidate = args[0];
    const twoWord = args.length > 1 ? `${args[0]} ${args[1]}` : "";
    if (commandNames.includes(twoWord) || commandNames.includes(candidate)) {
      const input = args.join(" ");
      const result = await dispatch(input);
      if (result.output) console.log(result.output);
      process.exit(result.exitCode);
    }
  }

  // Piped stdin → dispatch each line
  if (!process.stdin.isTTY) {
    const chunks: Buffer[] = [];
    for await (const chunk of process.stdin) {
      chunks.push(chunk as Buffer);
    }
    const input = Buffer.concat(chunks).toString("utf-8").trim();
    if (input) {
      const result = await dispatch(input);
      if (result.output) console.log(result.output);
      process.exit(result.exitCode);
    }
    process.exit(0);
  }

  // Interactive terminal → TUI (no subcommand given)
  const { startTui } = await import("./tui/index.js");
  startTui();
}

main();
