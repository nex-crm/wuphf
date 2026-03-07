/**
 * Commander program definition with global flags.
 */

import { Command } from "commander";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { version } = require("../package.json") as { version: string };

export const program = new Command();

program
  .name("nex")
  .description("Nex CLI — command-line interface for the Nex Developer API")
  .version(version)
  .option("--api-key <key>", "Override API key (env: NEX_API_KEY)")
  .option("--format <fmt>", "Output format: json, text, quiet")
  .option("--timeout <ms>", "Request timeout in milliseconds")
  .option("--session <id>", "Session ID for multi-turn context")
  .option("--debug", "Debug output on stderr");
