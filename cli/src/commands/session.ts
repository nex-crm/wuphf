/**
 * Session commands: list, clear.
 */

import { program } from "../cli.js";
import { SessionStore } from "../lib/session-store.js";
import { resolveFormat } from "../lib/config.js";
import { printOutput } from "../lib/output.js";
import type { Format } from "../lib/output.js";

const session = program
  .command("session")
  .description("Manage CLI session mappings");

session
  .command("list")
  .description("List all stored session mappings")
  .action(() => {
    const store = new SessionStore();
    const sessions = store.list();
    const opts = program.opts();
    const format = resolveFormat(opts.format) as Format;
    printOutput(sessions, format);
  });

session
  .command("clear")
  .description("Clear all stored session mappings")
  .action(() => {
    const store = new SessionStore();
    store.clear();
    const opts = program.opts();
    const format = resolveFormat(opts.format) as Format;
    printOutput({ message: "All sessions cleared." }, format);
  });
