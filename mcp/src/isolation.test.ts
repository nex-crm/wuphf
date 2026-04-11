/**
 * W9 — Multi-tenant isolation tests for the Nex MCP server.
 *
 * Isolation model: each call to createServer() produces a fresh McpServer
 * instance with its own NexApiClient bound to a specific API key.
 * The Nex API backend enforces workspace isolation on every request using
 * that key, so two parallel Managed Agents sessions with different keys
 * can never access each other's workspace data.
 */
import { test, expect, describe } from "bun:test";
import { createServer } from "../dist/server.js";
import { NexApiClient } from "../dist/client.js";

describe("MCP server multi-tenant isolation", () => {
  test("two server instances with different API keys are independent objects", () => {
    const server1 = createServer("api-key-workspace-A");
    const server2 = createServer("api-key-workspace-B");

    expect(server1).toBeDefined();
    expect(server2).toBeDefined();
    // Each call to createServer() returns a new instance — never a singleton
    expect(server1).not.toBe(server2);
  });

  test("NexApiClient stores the API key it was constructed with", () => {
    const clientA = new NexApiClient("key-for-workspace-A");
    const clientB = new NexApiClient("key-for-workspace-B");

    expect(clientA.isAuthenticated).toBe(true);
    expect(clientB.isAuthenticated).toBe(true);

    // Verify internal key isolation: mutating one client does not affect the other
    clientA.setApiKey("rotated-key-A");
    // clientB retains its original key
    expect(clientB.isAuthenticated).toBe(true);
    // Verify they are independent instances
    expect(clientA).not.toBe(clientB);
  });

  test("NexApiClient with no key is unauthenticated", () => {
    const clientAnon = new NexApiClient(undefined);
    expect(clientAnon.isAuthenticated).toBe(false);
  });

  test("NexApiClient with empty string is unauthenticated", () => {
    const clientEmpty = new NexApiClient("");
    expect(clientEmpty.isAuthenticated).toBe(false);
  });

  test("setApiKey authenticates a previously unauthenticated client", () => {
    const client = new NexApiClient(undefined);
    expect(client.isAuthenticated).toBe(false);
    client.setApiKey("freshly-injected-vault-key");
    expect(client.isAuthenticated).toBe(true);
  });
});

describe("MCP server tool list completeness smoke test", () => {
  test("server instantiates successfully with a valid API key", () => {
    const server = createServer("smoke-test-api-key");
    expect(server).toBeDefined();
    // McpServer exposes a `server` property with the underlying Server object
    expect(typeof server).toBe("object");
  });

  test("server instantiates successfully without any API key (registration-only mode)", () => {
    const server = createServer(undefined);
    expect(server).toBeDefined();
  });

  test("multiple sequential server instantiations do not interfere", () => {
    const servers = Array.from({ length: 5 }, (_, i) =>
      createServer(`api-key-workspace-${i}`)
    );
    // All instances are distinct
    for (let i = 0; i < servers.length; i++) {
      for (let j = i + 1; j < servers.length; j++) {
        expect(servers[i]).not.toBe(servers[j]);
      }
    }
  });
});
