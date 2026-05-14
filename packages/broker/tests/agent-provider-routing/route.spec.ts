import { type IncomingHttpHeaders, type OutgoingHttpHeaders, request } from "node:http";

import { forBrokerTests } from "@wuphf/credentials/testing";
import {
  type AgentId,
  type AgentProviderRouting,
  type AgentProviderRoutingEntry,
  agentProviderRoutingToJsonValue,
  agentProviderRoutingWriteResponseFromJson,
  asAgentId,
  asApiToken,
  asCredentialScope,
  asProviderKind,
  type RunnerKind,
} from "@wuphf/protocol";
import { afterEach, describe, expect, it } from "vitest";

import type { AgentProviderRoutingStore } from "../../src/agent-provider-routing/types.ts";
import { type BrokerHandle, createBroker } from "../../src/index.ts";

const token = asApiToken("test-token-with-enough-entropy-AAAAAAAAA");
const agentId = asAgentId("agent_alpha");
const otherAgentId = asAgentId("agent_beta");
const routePath = `/api/agents/${encodeURIComponent(agentId)}/provider-routing`;
let broker: BrokerHandle | null = null;

describe("agent provider routing routes", () => {
  afterEach(async () => {
    if (broker !== null) {
      await broker.stop();
      broker = null;
    }
  });

  it("GET returns the store config through the protocol JSON codec", async () => {
    const initial = [routeEntry("codex-cli", "openai", "openai")];
    const store = fakeStore(initial);
    const handle = await startBroker(store);

    const res = await fetch(`${handle.url}${routePath}`, {
      headers: { Authorization: `Bearer ${token}` },
    });

    expect(res.status).toBe(200);
    expect(res.headers.get("cache-control")).toBe("no-store");
    const body: unknown = await res.json();
    expect(body).toEqual(agentProviderRoutingToJsonValue({ agentId, routes: initial }));
  });

  it("PUT stores a protocol-parsed routing config and returns the write response shape", async () => {
    const store = fakeStore();
    const handle = await startBroker(store);
    const routes = [routeEntry("claude-cli", "anthropic", "anthropic")];
    const put = await fetch(`${handle.url}${routePath}`, {
      method: "PUT",
      headers: {
        Authorization: `Bearer ${token}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ agentId, routes }),
    });

    expect(put.status).toBe(200);
    expect(put.headers.get("cache-control")).toBe("no-store");
    expect(agentProviderRoutingWriteResponseFromJson(await put.json())).toEqual({
      applied: true,
    });

    await expect(store.get(agentId)).resolves.toEqual({ agentId, routes });
  });

  it("rejects a PUT whose body agentId disagrees with the URL agentId", async () => {
    const store = fakeStore();
    const handle = await startBroker(store);
    const res = await fetch(`${handle.url}${routePath}`, {
      method: "PUT",
      headers: {
        Authorization: `Bearer ${token}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        agentId: otherAgentId,
        routes: [routeEntry("claude-cli", "anthropic", "anthropic")],
      }),
    });

    expect(res.status).toBe(400);
    await expect(res.json()).resolves.toEqual({ error: "agent_id_mismatch" });
    await expect(store.get(agentId)).resolves.toEqual({ agentId, routes: [] });
  });

  it("requires bearer auth for GET and PUT", async () => {
    const handle = await startBroker(fakeStore());

    const get = await fetch(`${handle.url}${routePath}`);
    const put = await fetch(`${handle.url}${routePath}`, {
      method: "PUT",
      body: JSON.stringify({ agentId, routes: [] }),
    });

    expect(get.status).toBe(401);
    expect(put.status).toBe(401);
  });

  it("inherits the loopback DNS-rebinding guard", async () => {
    const handle = await startBroker(fakeStore());
    const res = await rawRequest({
      port: handle.port,
      path: routePath,
      headers: {
        Authorization: `Bearer ${token}`,
        Host: "evil.example.com",
      },
    });

    expect(res.status).toBe(403);
    expect(res.body).toMatch(/^loopback_/);
  });

  it("returns 405 with the route Allow header for unsupported methods", async () => {
    const handle = await startBroker(fakeStore());
    const res = await fetch(`${handle.url}${routePath}`, {
      method: "POST",
      headers: { Authorization: `Bearer ${token}` },
    });

    expect(res.status).toBe(405);
    expect(res.headers.get("allow")).toBe("GET, PUT");
    expect(res.headers.get("cache-control")).toBe("no-store");
  });

  it("returns codec validation errors for malformed PUT bodies", async () => {
    const handle = await startBroker(fakeStore());
    const res = await fetch(`${handle.url}${routePath}`, {
      method: "PUT",
      headers: {
        Authorization: `Bearer ${token}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ agentId, routes: "claude-cli" }),
    });

    expect(res.status).toBe(400);
    await expect(res.json()).resolves.toEqual({
      error: "agentProviderRoutingWriteRequest.routes: must be an array",
    });
  });
});

async function startBroker(store: AgentProviderRoutingStore): Promise<BrokerHandle> {
  const handle = await createBroker({
    token,
    runners: {
      tokenAgentIds: new Map([[token, agentId]]),
      brokerIdentityForAgent: (id) => forBrokerTests({ agentId: id }),
      credentialStore: {
        write: async () => {
          throw new Error("not used by agent provider routing route tests");
        },
        read: async () => {
          throw new Error("route tests must not read credentials");
        },
        readWithOwnership: async (input) => ({
          secret: "secret",
          agentId: input.expectedAgentId,
          scope: input.expectedScope,
        }),
        delete: async () => undefined,
      },
      costLedger: { record: async () => undefined },
      eventLog: { append: async () => 1 },
      spawnRunner: async () => {
        throw new Error("route tests must not spawn runners");
      },
      agentProviderRoutingStore: store,
    },
  });
  broker = handle;
  return handle;
}

function fakeStore(initial: readonly AgentProviderRoutingEntry[] = []): AgentProviderRoutingStore {
  const byAgent = new Map<AgentId, AgentProviderRouting>();
  if (initial.length > 0) {
    byAgent.set(agentId, { agentId, routes: initial });
  }
  return {
    get: async (id) => byAgent.get(id) ?? { agentId: id, routes: [] },
    getEntry: async (id, kind) => {
      const config = byAgent.get(id);
      const entry = config?.routes.find((route) => route.kind === kind);
      if (entry === undefined) return null;
      return {
        credentialScope: entry.credentialScope,
        providerKind: entry.providerKind,
      };
    },
    put: async (config) => {
      byAgent.set(config.agentId, { agentId: config.agentId, routes: [...config.routes] });
    },
    close: () => undefined,
  };
}

function routeEntry(
  kind: RunnerKind,
  credentialScope: "anthropic" | "openai" | "openai-compat",
  providerKind: "anthropic" | "openai" | "openai-compat",
): AgentProviderRoutingEntry {
  return {
    kind,
    credentialScope: asCredentialScope(credentialScope),
    providerKind: asProviderKind(providerKind),
  };
}

interface RawResponse {
  readonly status: number;
  readonly body: string;
  readonly headers: IncomingHttpHeaders;
}

function rawRequest(args: {
  readonly port: number;
  readonly path: string;
  readonly method?: string | undefined;
  readonly headers?: OutgoingHttpHeaders | undefined;
  readonly body?: string | undefined;
}): Promise<RawResponse> {
  return new Promise((resolveFn, rejectFn) => {
    const req = request(
      {
        host: "127.0.0.1",
        port: args.port,
        path: args.path,
        method: args.method ?? "GET",
        headers: args.headers,
      },
      (res) => {
        const chunks: Buffer[] = [];
        res.on("data", (chunk: Buffer) => chunks.push(chunk));
        res.on("end", () =>
          resolveFn({
            status: res.statusCode ?? 0,
            body: Buffer.concat(chunks).toString("utf8"),
            headers: res.headers,
          }),
        );
      },
    );
    req.on("error", rejectFn);
    if (args.body !== undefined) req.write(args.body);
    req.end();
  });
}
