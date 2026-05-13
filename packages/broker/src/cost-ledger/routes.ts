// HTTP routes for the cost ledger.
//
// All routes are mounted under `/api/v1/cost/*` so they inherit the
// existing `/api/*` bearer gate from the listener. The v1 namespace makes
// future versioning a route-table change rather than a wire-shape break.
//
// Routes (see also docs/modules/cost-ledger.md):
//   POST   /api/v1/cost/events                  — append a cost_event
//   POST   /api/v1/cost/budgets                 — set/update a budget
//   DELETE /api/v1/cost/budgets/:id             — tombstone a budget
//   GET    /api/v1/cost/budgets                 — list all budgets
//   GET    /api/v1/cost/budgets/:id             — fetch one budget
//   GET    /api/v1/cost/summary                 — current aggregate state
//   GET    /api/v1/cost/replay-check            — drift detector
//   POST   /api/v1/cost/idempotency/prune       — prune expired replay rows
//
// State-changing cost-command routes require an `Idempotency-Key` header in
// the shape `cmd_<command>_<26-char-ULID>`. On duplicate the broker replays
// the originally-stored response byte for byte. Mutations also require
// `X-Operator-Capability` when the host configures `cost.operatorToken`,
// plus `X-Operator-Identity` so server-minted budget audit fields identify
// the acting operator.

import type { IncomingMessage, ServerResponse } from "node:http";

import {
  type ApiToken,
  asBudgetId,
  asMicroUsd,
  asSignerIdentity,
  type BudgetId,
  type BudgetScope,
  type BudgetSetAuditPayload,
  type CostEventAuditPayload,
  costAuditPayloadFromJsonValue,
  isBudgetScope,
  type SignerIdentity,
} from "@wuphf/protocol";

import { tokenMatches } from "../auth.ts";
import type { BrokerLogger } from "../types.ts";
import {
  type CostCommand,
  DEFAULT_COMMAND_IDEMPOTENCY_TTL_MS,
  type IdempotencyParseError,
  parseIdempotencyKey,
} from "./idempotency.ts";
import type { CostLedger } from "./projections.ts";
import { runReplayCheck } from "./replay-check.ts";

// 256 KiB body cap. Cost payloads are tiny (<1 KiB typical); 256 KiB
// gives 1000x headroom while still pre-aborting any caller that streams
// a runaway body. Distinct from MAX_RECEIPT_BODY_BYTES because cost
// commands carry strictly smaller payloads.
const MAX_COST_BODY_BYTES = 256 * 1_024;
const DECIMAL_INTEGER_RE = /^(0|[1-9]\d*)$/;

export interface CostRouteDeps {
  readonly ledger: CostLedger;
  readonly logger: BrokerLogger;
  readonly db: import("better-sqlite3").Database;
  readonly operatorToken: ApiToken | null;
  readonly nowMs: () => number;
}

/**
 * Route dispatcher. Returns `true` if it handled the request, `false`
 * otherwise (so the listener can fall through to its 404 path).
 */
export async function handleCostRoute(
  req: IncomingMessage,
  res: ServerResponse,
  pathname: string,
  deps: CostRouteDeps,
): Promise<boolean> {
  if (pathname === "/api/v1/cost/events") {
    if (req.method !== "POST") {
      methodNotAllowed(res, "POST");
      return true;
    }
    await handleCostEventPost(req, res, deps);
    return true;
  }
  if (pathname === "/api/v1/cost/budgets") {
    if (req.method === "POST") {
      await handleBudgetSetPost(req, res, deps);
      return true;
    }
    if (req.method === "GET" || req.method === "HEAD") {
      handleBudgetList(res, deps);
      return true;
    }
    methodNotAllowed(res, "GET, POST");
    return true;
  }
  if (pathname.startsWith("/api/v1/cost/budgets/")) {
    const id = pathname.slice("/api/v1/cost/budgets/".length);
    if (id.length === 0 || id.includes("/")) {
      notFound(res);
      return true;
    }
    if (req.method === "GET" || req.method === "HEAD") {
      handleBudgetGet(res, deps, id);
      return true;
    }
    if (req.method === "DELETE") {
      await handleBudgetDelete(req, res, deps, id);
      return true;
    }
    methodNotAllowed(res, "GET, DELETE");
    return true;
  }
  if (pathname === "/api/v1/cost/summary") {
    if (req.method !== "GET" && req.method !== "HEAD") {
      methodNotAllowed(res, "GET");
      return true;
    }
    handleSummary(res, deps);
    return true;
  }
  if (pathname === "/api/v1/cost/replay-check") {
    if (req.method !== "GET" && req.method !== "HEAD") {
      methodNotAllowed(res, "GET");
      return true;
    }
    handleReplayCheck(res, deps);
    return true;
  }
  if (pathname === "/api/v1/cost/idempotency/prune") {
    if (req.method !== "POST") {
      methodNotAllowed(res, "POST");
      return true;
    }
    handleIdempotencyPrune(req, res, deps);
    return true;
  }
  return false;
}

async function handleCostEventPost(
  req: IncomingMessage,
  res: ServerResponse,
  deps: CostRouteDeps,
): Promise<void> {
  const operator = requireOperator(req, res, deps);
  if (!operator.ok) return;

  const idemKey = parseIdempotencyKey(req.headers["idempotency-key"]?.toString(), "cost.event");
  if (!idemKey.ok) {
    writeIdempotencyError(res, idemKey.error, deps.logger, "cost_event");
    return;
  }
  if (!ensureJsonContentType(req, res)) return;

  let body: string;
  try {
    body = await readBody(req, res, MAX_COST_BODY_BYTES);
  } catch {
    return;
  }

  let payload: CostEventAuditPayload;
  try {
    const parsed = JSON.parse(body) as unknown;
    payload = costAuditPayloadFromJsonValue("cost_event", parsed) as CostEventAuditPayload;
  } catch (err) {
    const reason = err instanceof Error ? err.message : "validation_failed";
    deps.logger.warn("cost_event_rejected", { reason: "invalid_payload" });
    writeJson(res, 400, { error: "invalid_payload", reason });
    return;
  }

  const nowMs = deps.nowMs();
  const result = deps.ledger.appendCostEventIdempotent({
    payload,
    idempotency: idemKey.key,
    nowMs,
    render: (applied) => {
      const responseBody = JSON.stringify({
        lsn: applied.lsn,
        agentDayTotal: applied.agentDayTotal as number,
        taskTotal: applied.taskTotal === null ? null : (applied.taskTotal as number),
        newCrossings: applied.newCrossings.map((c) => ({
          budgetId: c.budgetId,
          budgetSetLsn: c.budgetSetLsn,
          thresholdBps: c.thresholdBps,
          observedMicroUsd: c.observedMicroUsd as number,
          limitMicroUsd: c.limitMicroUsd as number,
          crossingEventLsn: c.crossingEventLsn,
        })),
      });
      return { statusCode: 201, payload: Buffer.from(responseBody, "utf8") };
    },
  });
  writeJsonRaw(res, result.statusCode, result.payload, result.replayed);
}

async function handleBudgetSetPost(
  req: IncomingMessage,
  res: ServerResponse,
  deps: CostRouteDeps,
): Promise<void> {
  const operator = requireOperator(req, res, deps);
  if (!operator.ok) return;

  const idemKey = parseIdempotencyKey(
    req.headers["idempotency-key"]?.toString(),
    "cost.budget.set",
  );
  if (!idemKey.ok) {
    writeIdempotencyError(res, idemKey.error, deps.logger, "budget_set");
    return;
  }
  if (!ensureJsonContentType(req, res)) return;

  let body: string;
  try {
    body = await readBody(req, res, MAX_COST_BODY_BYTES);
  } catch {
    return;
  }

  const nowMs = deps.nowMs();
  let payload: BudgetSetAuditPayload;
  try {
    payload = budgetSetPayloadFromRequest(body, operator.identity, nowMs);
  } catch (err) {
    const reason = err instanceof Error ? err.message : "validation_failed";
    deps.logger.warn("budget_set_rejected", { reason: "invalid_payload" });
    writeJson(res, 400, { error: "invalid_payload", reason });
    return;
  }
  // Setting limit=0 via POST is a category error — tombstoning belongs on
  // DELETE so the operator's intent is unambiguous in audit logs.
  if ((payload.limitMicroUsd as number) === 0) {
    writeJson(res, 400, {
      error: "use_delete_to_tombstone",
      reason: "POST /budgets requires limitMicroUsd > 0; DELETE /budgets/:id tombstones",
    });
    return;
  }

  const result = deps.ledger.appendBudgetSetIdempotent({
    payload,
    idempotency: idemKey.key,
    nowMs,
    render: (applied) => ({
      statusCode: 201,
      payload: Buffer.from(
        JSON.stringify({ lsn: applied.lsn, tombstoned: applied.tombstoned }),
        "utf8",
      ),
    }),
  });
  writeJsonRaw(res, result.statusCode, result.payload, result.replayed);
}

async function handleBudgetDelete(
  req: IncomingMessage,
  res: ServerResponse,
  deps: CostRouteDeps,
  rawId: string,
): Promise<void> {
  const operator = requireOperator(req, res, deps);
  if (!operator.ok) return;

  const idemKey = parseIdempotencyKey(
    req.headers["idempotency-key"]?.toString(),
    "cost.budget.tombstone",
  );
  if (!idemKey.ok) {
    writeIdempotencyError(res, idemKey.error, deps.logger, "budget_tombstone");
    return;
  }
  let budgetId: BudgetId;
  try {
    budgetId = asBudgetId(decodeURIComponent(rawId));
  } catch {
    notFound(res);
    return;
  }
  const existing = deps.ledger.getBudget(budgetId);
  if (existing === null) {
    notFound(res);
    return;
  }

  const nowMs = deps.nowMs();
  // Tombstone = budget_set with limitMicroUsd === 0. The projection's
  // `tombstoned` flag flips to 1; the reactor treats thresholds against
  // tombstoned budgets as unreachable; the event row stays in event_log
  // for replay.
  const tombstonePayload: BudgetSetAuditPayload = {
    budgetId,
    scope: existing.scope,
    ...(existing.subjectId === null ? {} : { subjectId: existing.subjectId }),
    limitMicroUsd: asMicroUsd(0),
    thresholdsBps: existing.thresholdsBps,
    setBy: operator.identity,
    setAt: new Date(nowMs),
  };

  const result = deps.ledger.appendBudgetSetIdempotent({
    payload: tombstonePayload,
    idempotency: idemKey.key,
    nowMs,
    render: (applied) => ({
      statusCode: 200,
      payload: Buffer.from(JSON.stringify({ lsn: applied.lsn, tombstoned: true }), "utf8"),
    }),
  });
  writeJsonRaw(res, result.statusCode, result.payload, result.replayed);
}

function handleBudgetList(res: ServerResponse, deps: CostRouteDeps): void {
  const budgets = deps.ledger.listBudgets();
  writeJson(res, 200, {
    budgets: budgets.map((b) => ({
      budgetId: b.budgetId,
      scope: b.scope,
      subjectId: b.subjectId,
      limitMicroUsd: b.limitMicroUsd as number,
      thresholdsBps: b.thresholdsBps,
      setAtLsn: b.setAtLsn,
      tombstoned: b.tombstoned,
    })),
  });
}

function handleBudgetGet(res: ServerResponse, deps: CostRouteDeps, rawId: string): void {
  let budgetId: BudgetId;
  try {
    budgetId = asBudgetId(decodeURIComponent(rawId));
  } catch {
    notFound(res);
    return;
  }
  const row = deps.ledger.getBudget(budgetId);
  if (row === null) {
    notFound(res);
    return;
  }
  writeJson(res, 200, {
    budgetId: row.budgetId,
    scope: row.scope,
    subjectId: row.subjectId,
    limitMicroUsd: row.limitMicroUsd as number,
    thresholdsBps: row.thresholdsBps,
    setAtLsn: row.setAtLsn,
    tombstoned: row.tombstoned,
  });
}

function handleSummary(res: ServerResponse, deps: CostRouteDeps): void {
  writeJson(res, 200, {
    agentSpend: deps.ledger.listAgentSpend().map((r) => ({
      agentSlug: r.agentSlug,
      dayUtc: r.dayUtc,
      totalMicroUsd: r.totalMicroUsd as number,
      lastLsn: r.lastLsn,
    })),
    budgets: deps.ledger.listBudgets().map((b) => ({
      budgetId: b.budgetId,
      scope: b.scope,
      subjectId: b.subjectId,
      limitMicroUsd: b.limitMicroUsd as number,
      thresholdsBps: b.thresholdsBps,
      tombstoned: b.tombstoned,
    })),
    thresholdCrossings: deps.ledger.listThresholdCrossings().map((c) => ({
      budgetId: c.budgetId,
      budgetSetLsn: c.budgetSetLsn,
      thresholdBps: c.thresholdBps,
      crossedAtLsn: c.crossedAtLsn,
      observedMicroUsd: c.observedMicroUsd as number,
      limitMicroUsd: c.limitMicroUsd as number,
    })),
  });
}

function handleReplayCheck(res: ServerResponse, deps: CostRouteDeps): void {
  const report = runReplayCheck(deps.db);
  writeJson(res, report.ok ? 200 : 500, {
    ok: report.ok,
    highestLsn: report.highestLsn,
    eventsScanned: report.eventsScanned,
    discrepancies: report.discrepancies,
  });
}

function handleIdempotencyPrune(
  req: IncomingMessage,
  res: ServerResponse,
  deps: CostRouteDeps,
): void {
  if (!requireOperator(req, res, deps).ok) return;

  const ttl = parseOlderThanMs(req);
  if (!ttl.ok) {
    writeJson(res, 400, { error: "invalid_older_than_ms", reason: ttl.reason });
    return;
  }
  const cutoffMs = deps.nowMs() - ttl.olderThanMs;
  const pruned = deps.ledger.pruneIdempotencyOlderThan(cutoffMs);
  writeJson(res, 200, { pruned, olderThanMs: ttl.olderThanMs, cutoffMs });
}

// ─────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────

type OperatorAuthResult =
  | { readonly ok: true; readonly identity: SignerIdentity }
  | { readonly ok: false };

type OlderThanMsParseResult =
  | { readonly ok: true; readonly olderThanMs: number }
  | { readonly ok: false; readonly reason: string };

function requireOperator(
  req: IncomingMessage,
  res: ServerResponse,
  deps: CostRouteDeps,
): OperatorAuthResult {
  if (deps.operatorToken !== null) {
    const presented = headerString(req.headers["x-operator-capability"]) ?? null;
    if (!tokenMatches(presented, deps.operatorToken)) {
      deps.logger.warn("cost_operator_auth_rejected", {
        reason: presented === null ? "missing_operator_capability" : "invalid_operator_capability",
      });
      writeJson(res, 403, {
        error: presented === null ? "operator_capability_required" : "operator_capability_invalid",
      });
      return { ok: false };
    }
  }

  const rawIdentity = headerString(req.headers["x-operator-identity"]);
  if (rawIdentity === undefined || rawIdentity.length === 0) {
    writeJson(res, 400, { error: "operator_identity_required" });
    return { ok: false };
  }
  try {
    return { ok: true, identity: asSignerIdentity(rawIdentity) };
  } catch (err) {
    const reason = err instanceof Error ? err.message : "invalid";
    writeJson(res, 400, { error: "operator_identity_invalid", reason });
    return { ok: false };
  }
}

function budgetSetPayloadFromRequest(
  body: string,
  operatorIdentity: SignerIdentity,
  nowMs: number,
): BudgetSetAuditPayload {
  const parsed = JSON.parse(body) as unknown;
  if (!isJsonRecord(parsed)) {
    throw new Error("budget_set request body must be a JSON object");
  }
  const serverStamped: Record<string, unknown> = {
    ...parsed,
    setBy: operatorIdentity,
    setAt: new Date(nowMs).toISOString(),
  };
  return costAuditPayloadFromJsonValue("budget_set", serverStamped) as BudgetSetAuditPayload;
}

function isJsonRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function parseOlderThanMs(req: IncomingMessage): OlderThanMsParseResult {
  const url = new URL(req.url ?? "", "http://127.0.0.1");
  const values = url.searchParams.getAll("olderThanMs");
  if (values.length === 0) {
    return { ok: true, olderThanMs: DEFAULT_COMMAND_IDEMPOTENCY_TTL_MS };
  }
  if (values.length > 1) {
    return { ok: false, reason: "olderThanMs may appear only once" };
  }
  const raw = values[0] ?? "";
  if (!DECIMAL_INTEGER_RE.test(raw)) {
    return { ok: false, reason: "olderThanMs must be a positive integer millisecond value" };
  }
  const olderThanMs = Number(raw);
  if (!Number.isSafeInteger(olderThanMs) || olderThanMs <= 0) {
    return { ok: false, reason: "olderThanMs must be a positive safe integer" };
  }
  return { ok: true, olderThanMs };
}

function headerString(value: string | string[] | undefined): string | undefined {
  if (typeof value === "string") return value;
  if (Array.isArray(value) && typeof value[0] === "string") return value[0];
  return undefined;
}

function writeIdempotencyError(
  res: ServerResponse,
  err: IdempotencyParseError,
  logger: BrokerLogger,
  routeName: string,
): void {
  logger.warn(`${routeName}_rejected`, { reason: `idempotency_${err.code}` });
  if (err.code === "missing") {
    writeJson(res, 400, { error: "idempotency_key_required" });
    return;
  }
  writeJson(res, 400, { error: "idempotency_key_invalid", reason: err });
}

function ensureJsonContentType(req: IncomingMessage, res: ServerResponse): boolean {
  const contentType = req.headers["content-type"];
  if (typeof contentType !== "string" || !isJsonMediaType(contentType)) {
    writeJson(res, 415, { error: "unsupported_media_type" });
    return false;
  }
  return true;
}

function isJsonMediaType(value: string): boolean {
  const semi = value.indexOf(";");
  const head = (semi === -1 ? value : value.slice(0, semi)).trim().toLowerCase();
  return head === "application/json";
}

function readBody(req: IncomingMessage, res: ServerResponse, maxBytes: number): Promise<string> {
  return new Promise((resolveFn, rejectFn) => {
    let receivedBytes = 0;
    const chunks: Buffer[] = [];
    let settled = false;
    const finish = (run: () => void): void => {
      if (settled) return;
      settled = true;
      run();
    };
    req.on("data", (chunk: Buffer) => {
      receivedBytes += chunk.length;
      if (receivedBytes > maxBytes) {
        req.pause();
        if (!res.writableEnded) {
          res.writeHead(413, { "Content-Type": "text/plain", Connection: "close" });
          res.end("body_too_large");
        }
        finish(() => rejectFn(new Error("body_too_large")));
        return;
      }
      chunks.push(chunk);
    });
    req.on("end", () => finish(() => resolveFn(Buffer.concat(chunks).toString("utf8"))));
    req.on("error", (err) => finish(() => rejectFn(err)));
    req.on("close", () => finish(() => rejectFn(new Error("body_read_aborted"))));
  });
}

function writeJson(res: ServerResponse, status: number, body: unknown): void {
  const text = JSON.stringify(body);
  res.writeHead(status, {
    "Content-Type": "application/json; charset=utf-8",
    "Cache-Control": "no-store",
    "Content-Length": String(Buffer.byteLength(text, "utf8")),
  });
  res.end(text);
}

function writeJsonRaw(
  res: ServerResponse,
  status: number,
  payload: Buffer,
  replayed: boolean,
): void {
  res.writeHead(status, {
    "Content-Type": "application/json; charset=utf-8",
    "Cache-Control": "no-store",
    "Content-Length": String(payload.byteLength),
    ...(replayed ? { "Idempotent-Replay": "true" } : {}),
  });
  res.end(payload);
}

function notFound(res: ServerResponse): void {
  writeJson(res, 404, { error: "not_found" });
}

function methodNotAllowed(res: ServerResponse, allow: string): void {
  res.writeHead(405, { Allow: allow, "Content-Type": "application/json; charset=utf-8" });
  res.end(JSON.stringify({ error: "method_not_allowed" }));
}

// Surface re-exports so test code can compose the route module without
// reaching into private files.
export type { BudgetScope, CostCommand };
export { isBudgetScope };
