#!/usr/bin/env node
/**
 * Registration entry point — creates a Nex account and persists the API key.
 *
 * Usage: node dist/auto-register.js <email> [name] [company]
 *
 * On success: prints API key and saves to ~/.nex-mcp.json (shared with MCP server).
 * If already registered (API key exists): prints status and exits.
 */

import { loadMcpConfig, persistRegistration, loadBaseUrl, MCP_CONFIG_PATH } from "./config.js";
import { NexClient } from "./nex-client.js";

async function main(): Promise<void> {
  const email = process.argv[2];
  const name = process.argv[3];
  const company = process.argv[4];

  if (!email) {
    console.error("Usage: auto-register.js <email> [name] [company]");
    process.exit(1);
  }

  // Check if already registered
  const existing = loadMcpConfig();
  if (existing.api_key) {
    console.log("Already registered.");
    console.log(`  API key: ${existing.api_key.slice(0, 12)}...`);
    console.log(`  Config: ${MCP_CONFIG_PATH}`);
    console.log("\nTo re-register, delete ~/.nex-mcp.json first.");
    return;
  }

  const baseUrl = loadBaseUrl();
  console.log(`Registering ${email} at ${baseUrl} ...`);

  try {
    const result = await NexClient.register(baseUrl, email, name, company);

    if (!result.api_key) {
      console.error("Registration succeeded but no API key returned.");
      console.error("Response:", JSON.stringify(result, null, 2));
      process.exit(1);
    }

    // Persist to shared config
    persistRegistration(result as Record<string, unknown>);

    console.log("Registration successful!");
    console.log(`  API key: ${result.api_key.slice(0, 12)}...`);
    if (result.workspace_slug) console.log(`  Workspace: ${result.workspace_slug}`);
    console.log(`  Saved to: ${MCP_CONFIG_PATH}`);
    console.log("\nAll Nex memory features are now active. No restart needed.");
  } catch (err) {
    console.error(`Registration failed: ${err instanceof Error ? err.message : String(err)}`);
    process.exit(1);
  }
}

main();
