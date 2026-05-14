import type { IncomingMessage, ServerResponse } from "node:http";

import {
  type AgentId,
  type ApiToken,
  agentProviderRoutingToJsonValue,
  agentProviderRoutingWriteRequestFromJson,
  agentProviderRoutingWriteResponseFromJson,
} from "@wuphf/protocol";

import { agentIdForBearer } from "../auth.ts";
import { isCompatibleRunnerProviderRoute } from "../runners/factory.ts";
import type { BrokerLogger } from "../types.ts";
import type { AgentProviderRoutingStore } from "./types.ts";

const MAX_AGENT_PROVIDER_ROUTING_BODY_BYTES = 256 * 1024;
const ALLOW_AGENT_PROVIDER_ROUTING = "GET, PUT";

export interface AgentProviderRoutingRouteDeps {
  readonly store: AgentProviderRoutingStore;
  readonly tokenAgentIds: ReadonlyMap<ApiToken, AgentId>;
  readonly logger: BrokerLogger;
}

export async function handleAgentProviderRoutingRoute(
  req: IncomingMessage,
  res: ServerResponse,
  agentId: AgentId,
  deps: AgentProviderRoutingRouteDeps,
): Promise<void> {
  // Bearer→agent binding mirrors POST /api/runners (runners/route.ts).
  // The global /api/* bearer gate proves "someone with the token", but a
  // bearer pinned to agent_alpha must not be able to PUT or even read
  // agent_beta's routing. Without this check, any authenticated caller
  // could swap a different agent's credential scope / provider.
  const callerAgentId = agentIdForBearer(req, deps.tokenAgentIds);
  if (callerAgentId === null || callerAgentId !== agentId) {
    deps.logger.warn("agent_provider_routing_rejected", {
      reason: callerAgentId === null ? "no_bearer_binding" : "agent_mismatch",
      method: req.method ?? null,
    });
    forbidden(res, "agent_provider_routing_not_authorized");
    return;
  }
  if (req.method === "GET") {
    const routing = await deps.store.get(agentId);
    writeJson(res, 200, agentProviderRoutingToJsonValue(routing));
    return;
  }
  if (req.method === "PUT") {
    await handlePut(req, res, agentId, deps);
    return;
  }
  methodNotAllowed(res);
}

async function handlePut(
  req: IncomingMessage,
  res: ServerResponse,
  agentId: AgentId,
  deps: AgentProviderRoutingRouteDeps,
): Promise<void> {
  let request: ReturnType<typeof agentProviderRoutingWriteRequestFromJson>;
  try {
    const parsed = JSON.parse(
      await readBody(req, MAX_AGENT_PROVIDER_ROUTING_BODY_BYTES),
    ) as unknown;
    request = agentProviderRoutingWriteRequestFromJson(parsed);
  } catch (err) {
    writeJson(res, 400, { error: err instanceof Error ? err.message : String(err) });
    return;
  }
  if (request.agentId !== agentId) {
    writeJson(res, 400, { error: "agent_id_mismatch" });
    return;
  }
  // Reject configs the spawn-time factory would reject anyway. Without this
  // an incompatible route persists, returns 200 {applied:true}, then breaks
  // every matching spawn with a `ProviderKindMismatch` — far from the write
  // that caused it.
  for (const entry of request.routes) {
    if (!isCompatibleRunnerProviderRoute(entry.kind, entry.credentialScope, entry.providerKind)) {
      writeJson(res, 400, {
        error: "incompatible_provider_route",
        detail: `runner kind ${entry.kind} does not support credentialScope=${entry.credentialScope} / providerKind=${entry.providerKind}`,
      });
      return;
    }
  }
  await deps.store.put({ agentId, routes: request.routes });
  deps.logger.info("agent_provider_routing_put_applied", {
    agentId,
    routeCount: request.routes.length,
    kinds: request.routes.map((entry) => entry.kind),
  });
  writeJson(res, 200, agentProviderRoutingWriteResponseFromJson({ applied: true }));
}

async function readBody(req: IncomingMessage, maxBytes: number): Promise<string> {
  let total = 0;
  const chunks: Buffer[] = [];
  for await (const chunk of req) {
    const buffer = Buffer.isBuffer(chunk) ? chunk : Buffer.from(String(chunk), "utf8");
    total += buffer.length;
    if (total > maxBytes) {
      throw new Error("agentProviderRoutingWriteRequest: body too large");
    }
    chunks.push(buffer);
  }
  return Buffer.concat(chunks).toString("utf8");
}

function writeJson(res: ServerResponse, status: number, bodyValue: unknown): void {
  const body = JSON.stringify(bodyValue);
  res.writeHead(status, {
    "Content-Type": "application/json; charset=utf-8",
    "Cache-Control": "no-store",
    "Content-Length": String(Buffer.byteLength(body, "utf8")),
  });
  res.end(body);
}

function methodNotAllowed(res: ServerResponse): void {
  const body = JSON.stringify({ error: "method_not_allowed" });
  res.writeHead(405, {
    Allow: ALLOW_AGENT_PROVIDER_ROUTING,
    "Content-Type": "application/json; charset=utf-8",
    "Cache-Control": "no-store",
    "Content-Length": String(Buffer.byteLength(body, "utf8")),
  });
  res.end(body);
}

function forbidden(res: ServerResponse, reason: string): void {
  const body = JSON.stringify({ error: reason });
  res.writeHead(403, {
    "Content-Type": "application/json; charset=utf-8",
    "Cache-Control": "no-store",
    "Content-Length": String(Buffer.byteLength(body, "utf8")),
  });
  res.end(body);
}
