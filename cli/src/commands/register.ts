/**
 * nex register — register a new developer account.
 */

import { program } from "../cli.js";
import { NexClient } from "../lib/client.js";
import { persistRegistration, resolveTimeout } from "../lib/config.js";
import { printOutput } from "../lib/output.js";
import type { Format } from "../lib/output.js";
import { resolveFormat } from "../lib/config.js";

program
  .command("register")
  .description("Register a new Nex workspace and get an API key")
  .requiredOption("--email <email>", "Email address")
  .option("--name <name>", "Your name")
  .option("--company <company>", "Company name")
  .action(async (opts: { email: string; name?: string; company?: string }) => {
    const globalOpts = program.opts();
    const client = new NexClient(undefined, resolveTimeout(globalOpts.timeout));
    const format = resolveFormat(globalOpts.format) as Format;

    const data = await client.register(opts.email, opts.name, opts.company);
    persistRegistration(data);
    printOutput(data, format);
  });
