import { mkdirSync, realpathSync } from "node:fs";
import type { IncomingMessage, ServerResponse } from "node:http";
import os from "node:os";
import path from "node:path";
import { ReadableStream } from "node:stream/web";

import type { AgentRunner, RunnerEventRecord } from "@wuphf/agent-runners";
import type { AgentId, ApiToken, BrokerIdentity, RunnerEvent, RunnerId } from "@wuphf/protocol";
import { asRunnerId, runnerEventToJsonValue, runnerSpawnRequestFromJson } from "@wuphf/protocol";

import { extractBearerFromHeader, tokenMatches } from "../auth.ts";
import type { BrokerLogger } from "../types.ts";
import { type AgentRunnerFactoryDeps, createAgentRunnerForBroker } from "./factory.ts";

export interface RunnerRouteConfig extends AgentRunnerFactoryDeps {
  readonly tokenAgentIds: ReadonlyMap<ApiToken, AgentId>;
  readonly brokerIdentityForAgent: (agentId: AgentId) => BrokerIdentity;
  readonly workspaceRoot?: string | undefined;
  readonly retentionTtlMs?: number | undefined;
  readonly maxRunners?: number | undefined;
  readonly stopGraceMs?: number | undefined;
  readonly sseDrainTimeoutMs?: number | undefined;
}

export interface RunnerRouteState {
  handle(req: IncomingMessage, res: ServerResponse, pathname: string): Promise<boolean>;
  stop(): Promise<void>;
}

interface RunnerRouteDeps extends RunnerRouteConfig {
  readonly logger: BrokerLogger;
}

const MAX_RUNNER_REQUEST_BYTES = 256 * 1024;
const DEFAULT_RUNNER_RETENTION_TTL_MS = 60_000;
const DEFAULT_MAX_RUNNERS = 100;
const DEFAULT_STOP_GRACE_MS = 5_000;
const DEFAULT_SSE_DRAIN_TIMEOUT_MS = 30_000;

interface RunnerEntry {
  readonly runner: AgentRunner;
  retentionTimer: NodeJS.Timeout | null;
}

export function createRunnerRouteState(deps: RunnerRouteDeps): RunnerRouteState {
  const runners = new Map<RunnerId, RunnerEntry>();
  const { WUPHF_WORKSPACE_ROOT: envWorkspaceRoot } = process.env;
  const workspaceRoot = deps.workspaceRoot ?? envWorkspaceRoot ?? defaultWorkspaceRoot();
  const retentionTtlMs = deps.retentionTtlMs ?? DEFAULT_RUNNER_RETENTION_TTL_MS;
  const maxRunners = deps.maxRunners ?? DEFAULT_MAX_RUNNERS;
  const stopGraceMs = deps.stopGraceMs ?? DEFAULT_STOP_GRACE_MS;
  const sseDrainTimeoutMs = deps.sseDrainTimeoutMs ?? DEFAULT_SSE_DRAIN_TIMEOUT_MS;
  return {
    async handle(req, res, pathname) {
      if (pathname === "/api/runners") {
        await handleSpawn(req, res, deps, runners, {
          maxRunners,
          retentionTtlMs,
          workspaceRoot,
        });
        return true;
      }
      if (pathname.startsWith("/api/runners/") && pathname.endsWith("/events")) {
        await handleEvents(req, res, pathname, deps, runners, sseDrainTimeoutMs);
        return true;
      }
      return false;
    },
    async stop() {
      const entries = [...runners.values()];
      await Promise.race([
        Promise.all(entries.map((entry) => entry.runner.terminate({ gracePeriodMs: stopGraceMs }))),
        delay(stopGraceMs),
      ]);
      for (const entry of entries) {
        if (entry.retentionTimer !== null) clearTimeout(entry.retentionTimer);
      }
      runners.clear();
    },
  };
}

async function handleSpawn(
  req: IncomingMessage,
  res: ServerResponse,
  deps: RunnerRouteDeps,
  runners: Map<RunnerId, RunnerEntry>,
  options: {
    readonly maxRunners: number;
    readonly retentionTtlMs: number;
    readonly workspaceRoot: string;
  },
): Promise<void> {
  if (req.method !== "POST") {
    methodNotAllowed(res, "POST");
    return;
  }
  if (runners.size >= options.maxRunners) {
    runnerCapacityExhausted(res, options.maxRunners);
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
  let brokerResolvedCwd: string;
  try {
    brokerResolvedCwd = resolveRunnerCwd(options.workspaceRoot, callerAgentId, request.cwd);
  } catch (error) {
    deps.logger.warn("runner_spawn_rejected", { reason: "cwd_out_of_workspace" });
    cwdOutOfWorkspace(res, error instanceof Error ? error.message : String(error));
    return;
  }
  const runner = await createAgentRunnerForBroker(
    { ...request, cwd: brokerResolvedCwd },
    deps.brokerIdentityForAgent(callerAgentId),
    deps,
  );
  runners.set(runner.id, { runner, retentionTimer: null });
  monitorRunner(runners, runner, options.retentionTtlMs, deps.logger);
  writeJson(res, 201, { runnerId: runner.id });
}

async function handleEvents(
  req: IncomingMessage,
  res: ServerResponse,
  pathname: string,
  deps: RunnerRouteDeps,
  runners: ReadonlyMap<RunnerId, RunnerEntry>,
  sseDrainTimeoutMs: number,
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
  const entry = runners.get(id);
  if (entry === undefined) {
    notFound(res);
    return;
  }
  const runner = entry.runner;
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
  await streamRunnerEvents(req, res, runner, deps.logger, sseDrainTimeoutMs);
}

async function streamRunnerEvents(
  req: IncomingMessage,
  res: ServerResponse,
  runner: AgentRunner,
  logger: BrokerLogger,
  sseDrainTimeoutMs: number,
): Promise<void> {
  const afterLsn = parseLastEventId(headerString(req.headers["last-event-id"]));
  const stream =
    runner.eventRecords?.({ afterLsn }) ?? eventsToRecords(runner.events({ afterLsn }));
  const reader = stream.getReader();
  const close = (): void => {
    reader.cancel().catch(() => undefined);
  };
  res.on("close", close);
  try {
    while (!res.writableEnded) {
      const next = await reader.read();
      if (next.done) break;
      const ok = res.write(formatRunnerSseEvent(next.value));
      if (!ok) {
        const drained = await waitForDrain(res, sseDrainTimeoutMs);
        if (!drained) {
          logger.warn("runner_sse_disconnected", {
            runnerId: runner.id,
            reason: "drain_timeout",
            timeoutMs: sseDrainTimeoutMs,
          });
          break;
        }
      }
    }
  } finally {
    res.off("close", close);
    if (!res.writableEnded) res.end();
  }
}

function monitorRunner(
  runners: Map<RunnerId, RunnerEntry>,
  runner: AgentRunner,
  retentionTtlMs: number,
  logger: BrokerLogger,
): void {
  const reader = runner.events().getReader();
  void (async () => {
    try {
      while (true) {
        const next = await reader.read();
        if (next.done) return;
        if (next.value.kind === "finished" || next.value.kind === "failed") {
          const entry = runners.get(runner.id);
          if (entry === undefined) return;
          entry.retentionTimer = setTimeout(() => {
            runners.delete(runner.id);
          }, retentionTtlMs);
          entry.retentionTimer.unref();
          return;
        }
      }
    } catch (error) {
      logger.warn("runner_retention_monitor_failed", {
        runnerId: runner.id,
        error: error instanceof Error ? error.message : String(error),
      });
    } finally {
      reader.releaseLock();
    }
  })();
}

function formatRunnerSseEvent(record: RunnerEventRecord): string {
  const id = record.lsn === undefined ? "unlogged" : String(record.lsn);
  return `id: ${id}\nevent: ${record.event.kind}\ndata: ${JSON.stringify(
    runnerEventToJsonValue(record.event),
  )}\n\n`;
}

function eventsToRecords(stream: ReadableStream<RunnerEvent>): ReadableStream<RunnerEventRecord> {
  let localLsn = 0;
  return new ReadableStream<RunnerEventRecord>({
    async start(controller) {
      const reader = stream.getReader();
      try {
        while (true) {
          const next = await reader.read();
          if (next.done) break;
          localLsn += 1;
          controller.enqueue({ event: next.value, lsn: localLsn });
        }
        controller.close();
      } catch (error) {
        controller.error(error);
      }
    },
    cancel() {
      return stream.cancel();
    },
  });
}

function parseLastEventId(value: string | undefined): number | undefined {
  if (value === undefined || value.length === 0) return undefined;
  const parsed = Number.parseInt(value, 10);
  return Number.isSafeInteger(parsed) && parsed >= 0 ? parsed : undefined;
}

function waitForDrain(res: ServerResponse, timeoutMs: number): Promise<boolean> {
  if (res.writableEnded) return Promise.resolve(false);
  return new Promise((resolve) => {
    const cleanup = (): void => {
      clearTimeout(timeout);
      res.off("drain", onDrain);
      res.off("close", onClose);
      res.off("error", onClose);
    };
    const onDrain = (): void => {
      cleanup();
      resolve(true);
    };
    const onClose = (): void => {
      cleanup();
      resolve(false);
    };
    const timeout = setTimeout(() => {
      cleanup();
      resolve(false);
    }, timeoutMs);
    timeout.unref();
    res.once("drain", onDrain);
    res.once("close", onClose);
    res.once("error", onClose);
  });
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => {
    const timeout = setTimeout(resolve, ms);
    timeout.unref();
  });
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

function runnerCapacityExhausted(res: ServerResponse, maxRunners: number): void {
  writeJson(res, 503, { error: "runner_capacity_exhausted", maxRunners });
}

function cwdOutOfWorkspace(res: ServerResponse, reason: string): void {
  writeJson(res, 400, { error: "cwd_out_of_workspace", reason });
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

function defaultWorkspaceRoot(): string {
  return path.join(os.homedir(), ".wuphf", "workspaces");
}

// Threat model: cwd is broker-resolved against the caller agent's workspace
// root (`<workspaceRoot>/<agentId>/`); raw client cwd values are never passed
// to spawn. Branch 10 will populate providerRoute before this route receives
// the request, but this pass only adds the wire slot and cwd containment.
function resolveRunnerCwd(
  workspaceRoot: string,
  agentId: AgentId,
  requestedCwd: string | undefined,
): string {
  const agentWorkspacePath = path.join(workspaceRoot, agentId);
  mkdirSync(agentWorkspacePath, { recursive: true });
  const workspaceRootReal = realpathSync(workspaceRoot);
  const agentWorkspaceReal = realpathSync(agentWorkspacePath);
  if (!pathIsInside(workspaceRootReal, agentWorkspaceReal)) {
    throw new Error("agent workspace root escapes workspaceRoot");
  }
  const candidate =
    requestedCwd === undefined
      ? agentWorkspaceReal
      : path.resolve(agentWorkspaceReal, requestedCwd);
  const candidateReal = realpathSync(candidate);
  if (!pathIsInside(agentWorkspaceReal, candidateReal)) {
    throw new Error("cwd resolves outside the agent workspace");
  }
  return candidateReal;
}

function pathIsInside(parent: string, child: string): boolean {
  const relative = path.relative(parent, child);
  return relative === "" || (!relative.startsWith("..") && !path.isAbsolute(relative));
}
