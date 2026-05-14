import type { IncomingMessage, ServerResponse } from "node:http";

import type { AgentRunner } from "@wuphf/agent-runners";
import type { AgentId, ApiToken, BrokerIdentity, RunnerId } from "@wuphf/protocol";
import { asRunnerId, runnerEventToJsonValue, runnerSpawnRequestFromJson } from "@wuphf/protocol";

import { extractBearerFromHeader, tokenMatches } from "../auth.ts";
import type { BrokerLogger } from "../types.ts";
import { type AgentRunnerFactoryDeps, createAgentRunnerForBroker } from "./factory.ts";

export interface RunnerRouteConfig extends AgentRunnerFactoryDeps {
  readonly tokenAgentIds: ReadonlyMap<ApiToken, AgentId>;
  readonly brokerIdentityForAgent: (agentId: AgentId) => BrokerIdentity;
}

export interface RunnerRouteState {
  handle(req: IncomingMessage, res: ServerResponse, pathname: string): Promise<boolean>;
}

interface RunnerRouteDeps extends RunnerRouteConfig {
  readonly logger: BrokerLogger;
}

const MAX_RUNNER_REQUEST_BYTES = 256 * 1024;

export function createRunnerRouteState(deps: RunnerRouteDeps): RunnerRouteState {
  const runners = new Map<RunnerId, AgentRunner>();
  return {
    async handle(req, res, pathname) {
      if (pathname === "/api/runners") {
        await handleSpawn(req, res, deps, runners);
        return true;
      }
      if (pathname.startsWith("/api/runners/") && pathname.endsWith("/events")) {
        await handleEvents(req, res, pathname, deps, runners);
        return true;
      }
      return false;
    },
  };
}

async function handleSpawn(
  req: IncomingMessage,
  res: ServerResponse,
  deps: RunnerRouteDeps,
  runners: Map<RunnerId, AgentRunner>,
): Promise<void> {
  if (req.method !== "POST") {
    methodNotAllowed(res, "POST");
    return;
  }
  const callerAgentId = agentIdForBearer(req, deps.tokenAgentIds);
  if (callerAgentId === null) {
    forbidden(res, "runner_agent_not_authorized");
    return;
  }
  let request: ReturnType<typeof runnerSpawnRequestFromJson>;
  try {
    request = runnerSpawnRequestFromJson(JSON.parse(await readBody(req, MAX_RUNNER_REQUEST_BYTES)));
  } catch (error) {
    deps.logger.warn("runner_spawn_rejected", { reason: "invalid_request" });
    badRequest(res, error instanceof Error ? error.message : String(error));
    return;
  }
  if (request.agentId !== callerAgentId) {
    deps.logger.warn("runner_spawn_rejected", { reason: "agent_mismatch" });
    forbidden(res, "runner_agent_mismatch");
    return;
  }
  const runner = await createAgentRunnerForBroker(
    request,
    deps.brokerIdentityForAgent(callerAgentId),
    deps,
  );
  runners.set(runner.id, runner);
  writeJson(res, 201, { runnerId: runner.id });
}

async function handleEvents(
  req: IncomingMessage,
  res: ServerResponse,
  pathname: string,
  deps: RunnerRouteDeps,
  runners: ReadonlyMap<RunnerId, AgentRunner>,
): Promise<void> {
  if (req.method !== "GET" && req.method !== "HEAD") {
    methodNotAllowed(res, "GET, HEAD");
    return;
  }
  const id = runnerIdFromEventsPath(pathname);
  if (id === null) {
    notFound(res);
    return;
  }
  const runner = runners.get(id);
  if (runner === undefined) {
    notFound(res);
    return;
  }
  const callerAgentId = agentIdForBearer(req, deps.tokenAgentIds);
  if (callerAgentId === null || callerAgentId !== runner.agentId) {
    forbidden(res, "runner_agent_not_authorized");
    return;
  }
  res.writeHead(200, {
    "Content-Type": "text/event-stream; charset=utf-8",
    "Cache-Control": "no-store",
    Connection: "keep-alive",
    "X-Accel-Buffering": "no",
  });
  res.flushHeaders();
  if (req.method === "HEAD") {
    res.end();
    return;
  }
  await streamRunnerEvents(res, runner);
}

async function streamRunnerEvents(res: ServerResponse, runner: AgentRunner): Promise<void> {
  const reader = runner.events().getReader();
  let nextId = 0;
  const close = (): void => {
    reader.cancel().catch(() => undefined);
  };
  res.on("close", close);
  try {
    while (!res.writableEnded) {
      const next = await reader.read();
      if (next.done) break;
      res.write(
        `id: runner_${nextId}\nevent: ${next.value.kind}\ndata: ${JSON.stringify(
          runnerEventToJsonValue(next.value),
        )}\n\n`,
      );
      nextId += 1;
    }
  } finally {
    res.off("close", close);
    if (!res.writableEnded) res.end();
  }
}

function agentIdForBearer(
  req: IncomingMessage,
  tokenAgentIds: ReadonlyMap<ApiToken, AgentId>,
): AgentId | null {
  const presented = extractBearerFromHeader(headerString(req.headers.authorization));
  for (const [token, agentId] of tokenAgentIds) {
    if (tokenMatches(presented, token)) return agentId;
  }
  return null;
}

function runnerIdFromEventsPath(pathname: string): RunnerId | null {
  const prefix = "/api/runners/";
  const suffix = "/events";
  const encoded = pathname.slice(prefix.length, pathname.length - suffix.length);
  if (encoded.length === 0 || encoded.includes("/")) return null;
  try {
    return asRunnerId(decodeURIComponent(encoded));
  } catch {
    return null;
  }
}

async function readBody(req: IncomingMessage, maxBytes: number): Promise<string> {
  let total = 0;
  const chunks: Buffer[] = [];
  for await (const chunk of req) {
    const buffer = Buffer.isBuffer(chunk) ? chunk : Buffer.from(String(chunk), "utf8");
    total += buffer.length;
    if (total > maxBytes) {
      throw new Error("runner request body too large");
    }
    chunks.push(buffer);
  }
  return Buffer.concat(chunks).toString("utf8");
}

function headerString(value: string | string[] | undefined): string | undefined {
  if (typeof value === "string") return value;
  if (Array.isArray(value) && typeof value[0] === "string") return value[0];
  return undefined;
}

function writeJson(
  res: ServerResponse,
  status: number,
  bodyValue: Readonly<Record<string, unknown>>,
): void {
  const body = JSON.stringify(bodyValue);
  res.statusCode = status;
  res.setHeader("Content-Type", "application/json; charset=utf-8");
  res.setHeader("Cache-Control", "no-store");
  res.setHeader("Content-Length", String(Buffer.byteLength(body, "utf8")));
  res.end(body);
}

function badRequest(res: ServerResponse, reason: string): void {
  writeJson(res, 400, { error: "invalid_runner_request", reason });
}

function forbidden(res: ServerResponse, reason: string): void {
  res.statusCode = 403;
  res.setHeader("Content-Type", "text/plain; charset=utf-8");
  res.end(reason);
}

function notFound(res: ServerResponse): void {
  res.statusCode = 404;
  res.setHeader("Content-Type", "text/plain; charset=utf-8");
  res.end("not_found");
}

function methodNotAllowed(res: ServerResponse, allow: string): void {
  res.statusCode = 405;
  res.setHeader("Allow", allow);
  res.setHeader("Content-Type", "text/plain; charset=utf-8");
  res.end("method_not_allowed");
}
