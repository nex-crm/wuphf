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
//
// Every state-changing route requires an `Idempotency-Key` header in the
// shape `cmd_<command>_<26-char-ULID>`. On duplicate the broker replays
// the originally-stored response byte for byte.

import type { IncomingMessage, ServerResponse } from "node:http";

import {
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

import type { BrokerLogger } from "../types.ts";
import {
  type CostCommand,
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

export interface CostRouteDeps {
  readonly ledger: CostLedger;
  readonly logger: BrokerLogger;
  readonly db: import("better-sqlite3").Database;
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
  return false;
}

async function handleCostEventPost(
  req: IncomingMessage,
  res: ServerResponse,
  deps: CostRouteDeps,
): Promise<void> {
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

  // Atomic with the ledger transaction: lookup + append + idempotency
  // insert all in one SQLite commit. See triangulation finding B1.
  const result = deps.ledger.appendCostEventIdempotent({
    payload,
    idempotency: idemKey.key,
    nowMs: deps.nowMs(),
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

  let payload: BudgetSetAuditPayload;
  try {
    const parsed = JSON.parse(body) as unknown;
    payload = costAuditPayloadFromJsonValue("budget_set", parsed) as BudgetSetAuditPayload;
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
    nowMs: deps.nowMs(),
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
  const idemKey = parseIdempotencyKey(
    req.headers["idempotency-key"]?.toString(),
    "cost.budget.tombstone",
  );
  if (!idemKey.ok) {
    writeIdempotencyError(res, idemKey.error, deps.logger, "budget_tombstone");
    return;
  }
  // The operator's identity is required so the audit row records *who*
  // tombstoned the budget. PR B's supervisor will mint this header from
  // the authenticated operator; PR A's clients pass it directly.
  let operatorIdentity: SignerIdentity;
  try {
    const raw = req.headers["x-operator-identity"];
    if (typeof raw !== "string" || raw.length === 0) {
      writeJson(res, 400, { error: "operator_identity_required" });
      return;
    }
    operatorIdentity = asSignerIdentity(raw);
  } catch (err) {
    const reason = err instanceof Error ? err.message : "invalid";
    writeJson(res, 400, { error: "operator_identity_invalid", reason });
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
    setBy: operatorIdentity,
    setAt: new Date(deps.nowMs()),
  };

  const result = deps.ledger.appendBudgetSetIdempotent({
    payload: tombstonePayload,
    idempotency: idemKey.key,
    nowMs: deps.nowMs(),
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

// ─────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────

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
