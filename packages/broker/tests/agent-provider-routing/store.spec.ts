import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import {
  type AgentProviderRouting,
  type AgentProviderRoutingEntry,
  asAgentId,
  asCredentialScope,
  asProviderKind,
} from "@wuphf/protocol";
import { afterEach, describe, expect, it } from "vitest";

import {
  createAgentProviderRoutingStore,
  SqliteAgentProviderRoutingStore,
} from "../../src/agent-provider-routing/index.ts";
import { CURRENT_SCHEMA_VERSION, openDatabase, runMigrations } from "../../src/event-log/index.ts";

const stores: SqliteAgentProviderRoutingStore[] = [];
const tempDirs: string[] = [];

afterEach(() => {
  for (const store of stores.splice(0)) {
    store.close();
  }
  for (const dir of tempDirs.splice(0)) {
    rmSync(dir, { recursive: true, force: true });
  }
});

function openStore(): SqliteAgentProviderRoutingStore {
  const store = SqliteAgentProviderRoutingStore.open({ path: ":memory:" });
  stores.push(store);
  return store;
}

function tempDbPath(): string {
  const dir = mkdtempSync(join(tmpdir(), "wuphf-agent-provider-routing-"));
  tempDirs.push(dir);
  return join(dir, "routing.sqlite");
}

const agentA = asAgentId("agent_a");
const agentB = asAgentId("agent_b");

function route(
  kind: AgentProviderRoutingEntry["kind"],
  credentialScope: string,
  providerKind: string,
): AgentProviderRoutingEntry {
  return {
    kind,
    credentialScope: asCredentialScope(credentialScope),
    providerKind: asProviderKind(providerKind),
  };
}

describe("SqliteAgentProviderRoutingStore", () => {
  it("get returns an empty config for an agent with no rows", async () => {
    const store = openStore();

    await expect(store.get(agentA)).resolves.toEqual({ agentId: agentA, routes: [] });
  });

  it("getEntry returns null for an agent and runner kind with no row", async () => {
    const store = openStore();

    await expect(store.getEntry(agentA, "claude-cli")).resolves.toBeNull();
  });

  it("put then get round-trips all route fields", async () => {
    const store = openStore();
    const config: AgentProviderRouting = {
      agentId: agentA,
      routes: [
        route("claude-cli", "anthropic", "anthropic"),
        route("codex-cli", "openai", "openai"),
      ],
    };

    await store.put(config);

    await expect(store.get(agentA)).resolves.toEqual(config);
    await expect(store.getEntry(agentA, "codex-cli")).resolves.toEqual({
      credentialScope: asCredentialScope("openai"),
      providerKind: asProviderKind("openai"),
    });
  });

  it("get returns routes sorted by RunnerKind enum order regardless of input order", async () => {
    const store = openStore();

    await store.put({
      agentId: agentA,
      routes: [
        route("openai-compat", "openai-compat", "openai-compat"),
        route("codex-cli", "openai", "openai"),
        route("claude-cli", "anthropic", "anthropic"),
      ],
    });

    const config = await store.get(agentA);
    expect(config.routes.map((entry) => entry.kind)).toEqual([
      "claude-cli",
      "codex-cli",
      "openai-compat",
    ]);
  });

  it("put replaces all routes for an agent", async () => {
    const store = openStore();

    await store.put({
      agentId: agentA,
      routes: [
        route("claude-cli", "anthropic", "anthropic"),
        route("codex-cli", "openai", "openai"),
      ],
    });
    await store.put({
      agentId: agentA,
      routes: [route("openai-compat", "openai-compat", "openai-compat")],
    });

    await expect(store.get(agentA)).resolves.toEqual({
      agentId: agentA,
      routes: [route("openai-compat", "openai-compat", "openai-compat")],
    });
  });

  it("put with no routes clears an agent's entries", async () => {
    const store = openStore();

    await store.put({
      agentId: agentA,
      routes: [route("claude-cli", "anthropic", "anthropic")],
    });
    await store.put({ agentId: agentA, routes: [] });

    await expect(store.get(agentA)).resolves.toEqual({ agentId: agentA, routes: [] });
    await expect(store.getEntry(agentA, "claude-cli")).resolves.toBeNull();
  });

  it("keeps routes for different agents isolated", async () => {
    const store = openStore();
    const agentARoute = route("claude-cli", "anthropic", "anthropic");
    const agentBRoute = route("codex-cli", "openai", "openai");

    await store.put({ agentId: agentA, routes: [agentARoute] });
    await store.put({ agentId: agentB, routes: [agentBRoute] });

    await expect(store.get(agentA)).resolves.toEqual({ agentId: agentA, routes: [agentARoute] });
    await expect(store.get(agentB)).resolves.toEqual({ agentId: agentB, routes: [agentBRoute] });
  });

  it("rejects invalid route values without clearing existing rows", async () => {
    const store = openStore();
    const original: AgentProviderRouting = {
      agentId: agentA,
      routes: [route("claude-cli", "anthropic", "anthropic")],
    };
    const invalid = {
      agentId: agentA,
      routes: [
        {
          kind: "codex-cli",
          credentialScope: "not-a-scope",
          providerKind: "openai",
        },
      ],
    } as unknown as AgentProviderRouting;

    await store.put(original);
    await expect(store.put(invalid)).rejects.toThrow(
      "agentProviderRouting.routes/0.credentialScope",
    );
    await expect(store.get(agentA)).resolves.toEqual(original);
  });

  it("can be constructed over an already-migrated database", async () => {
    const db = openDatabase({ path: ":memory:" });
    try {
      runMigrations(db);
      const store = createAgentProviderRoutingStore(db);
      await store.put({
        agentId: agentA,
        routes: [route("claude-cli", "anthropic", "anthropic")],
      });

      await expect(store.get(agentA)).resolves.toEqual({
        agentId: agentA,
        routes: [route("claude-cli", "anthropic", "anthropic")],
      });
      store.close();
    } finally {
      db.close();
    }
  });

  it("migrates a fresh database through schema version 3", () => {
    const db = openDatabase({ path: ":memory:" });
    try {
      runMigrations(db);

      expect(db.pragma("user_version", { simple: true })).toBe(CURRENT_SCHEMA_VERSION);
      expect(
        db
          .prepare<[], { readonly name: string }>(
            "SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'agent_provider_routing'",
          )
          .get()?.name,
      ).toBe("agent_provider_routing");
    } finally {
      db.close();
    }
  });

  it("does not re-run the agent provider routing migration for an already-v3 database", () => {
    const path = tempDbPath();
    const first = openDatabase({ path });
    try {
      runMigrations(first);
      first
        .prepare<[string, string, string, string]>(
          `INSERT INTO agent_provider_routing
             (agent_id, runner_kind, credential_scope, provider_kind)
           VALUES (?, ?, ?, ?)`,
        )
        .run("agent_a", "claude-cli", "anthropic", "anthropic");
    } finally {
      first.close();
    }

    const second = openDatabase({ path });
    try {
      runMigrations(second);
      expect(
        second
          .prepare<[], { readonly count: number }>(
            "SELECT COUNT(*) AS count FROM agent_provider_routing",
          )
          .get()?.count,
      ).toBe(1);
    } finally {
      second.close();
    }
  });
});
