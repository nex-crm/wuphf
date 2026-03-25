/**
 * wuphf config — view and update CLI configuration.
 */

import { program } from "../cli.js";
import {
  loadConfig,
  saveConfig,
  CONFIG_PATH,
  resolveApiKey,
  BASE_URL,
} from "../lib/config.js";
import { printOutput } from "../lib/output.js";
import { resolveFormat } from "../lib/config.js";
import type { Format } from "../lib/output.js";

function maskApiKey(key: string): string {
  if (key.length <= 4) return key;
  return "****" + key.slice(-4);
}

const configCmd = program
  .command("config")
  .description("Manage CLI configuration");

configCmd
  .command("show")
  .description("Show resolved configuration")
  .action(() => {
    const globalOpts = program.opts();
    const format = resolveFormat(globalOpts.format) as Format;
    const config = loadConfig();
    const apiKey = resolveApiKey(globalOpts.apiKey);

    const display: Record<string, unknown> = {
      ...config,
      api_key: apiKey ? maskApiKey(apiKey) : undefined,
      base_url: BASE_URL,
      config_path: CONFIG_PATH,
    };

    printOutput(display, format);
  });

configCmd
  .command("set")
  .description("Set a configuration value")
  .argument("<key>", "Config key to set")
  .argument("<value>", "Value to set")
  .action((key: string, value: string) => {
    const config = loadConfig();
    (config as Record<string, unknown>)[key] = value;
    saveConfig(config);

    const globalOpts = program.opts();
    const format = resolveFormat(globalOpts.format) as Format;
    printOutput({ [key]: value }, format);
  });

configCmd
  .command("path")
  .description("Print the config file path")
  .action(() => {
    const globalOpts = program.opts();
    const format = resolveFormat(globalOpts.format) as Format;
    printOutput({ path: CONFIG_PATH }, format);
  });
