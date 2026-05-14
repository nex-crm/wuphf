import type { IncomingMessage, ServerResponse } from "node:http";

import {
  type AgentId,
  agentProviderRoutingToJsonValue,
  agentProviderRoutingWriteRequestFromJson,
  agentProviderRoutingWriteResponseFromJson,
} from "@wuphf/protocol";

import type { AgentProviderRoutingStore } from "./types.ts";

const MAX_AGENT_PROVIDER_ROUTING_BODY_BYTES = 256 * 1024;
const ALLOW_AGENT_PROVIDER_ROUTING = "GET, PUT";

export async function handleAgentProviderRoutingRoute(
  req: IncomingMessage,
  res: ServerResponse,
  agentId: AgentId,
  store: AgentProviderRoutingStore,
): Promise<void> {
  if (req.method === "GET") {
    const routing = await store.get(agentId);
    writeJson(res, 200, agentProviderRoutingToJsonValue(routing));
    return;
  }
  if (req.method === "PUT") {
    await handlePut(req, res, agentId, store);
    return;
  }
  methodNotAllowed(res);
}

async function handlePut(
  req: IncomingMessage,
  res: ServerResponse,
  agentId: AgentId,
  store: AgentProviderRoutingStore,
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
  await store.put({ agentId, routes: request.routes });
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
